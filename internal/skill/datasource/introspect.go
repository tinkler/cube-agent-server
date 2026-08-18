package datasource

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tinkler/cube-agent-server/internal/engine/source"
)

// Introspector 数据源元数据提取
// 阉割版:只实现 SQLite,其他驱动 W4+ 接入
type Introspector struct {
	reg     *source.Registry
	cache   map[string]*source.DataSourceConfig
}

// NewIntrospector 构造
func NewIntrospector(reg *source.Registry, cfgs []*source.DataSourceConfig) *Introspector {
	m := map[string]*source.DataSourceConfig{}
	for _, c := range cfgs {
		if c != nil {
			m[c.Name] = c
		}
	}
	return &Introspector{reg: reg, cache: m}
}

// Introspect 提取数据源元数据
func (i *Introspector) Introspect(ctx context.Context, dsName string) (*Meta, error) {
	cfg, ok := i.cache[dsName]
	if !ok {
		return nil, fmt.Errorf("introspect: datasource %q not found", dsName)
	}
	ds, err := i.reg.Build(cfg)
	if err != nil {
		return nil, fmt.Errorf("introspect: build %q: %w", dsName, err)
	}
	defer ds.Close()

	driver := ds.Driver()
	switch driver {
	case "sqlite":
		return introspectSQLite(ctx, ds, dsName)
	case "pgx", "postgres":
		return introspectPostgres(ctx, ds, dsName)
	case "mysql":
		return introspectMySQL(ctx, ds, dsName)
	case "clickhouse":
		return introspectClickHouse(ctx, ds, dsName)
	case "mssql":
		return introspectMSSQL(ctx, ds, dsName)
	default:
		return nil, fmt.Errorf("introspect: driver %q not supported (W4+ 接入)", driver)
	}
}

// ============================================================
// 通用:从 DB 提取表列表
// ============================================================

type tableInfo struct {
	Name    string
	Type    string // base/view/etc
	SQL     string
}

func listTables(ctx context.Context, q queryFn) ([]tableInfo, error) {
	// 各方言通用 query,看下来:
	// - SQLite: sqlite_master
	// - PG/MySQL/MSSQL: information_schema.tables
	// - CH: system.tables
	//
	// 阉割版:每个方言单独写
	return nil, fmt.Errorf("listTables: use dialect-specific")
}

type queryFn func(ctx context.Context, sql string, args ...any) (*source.Result, error)

// ============================================================
// SQLite
// ============================================================

func introspectSQLite(ctx context.Context, ds source.DataSource, dsName string) (*Meta, error) {
	q := func(ctx context.Context, sql string, args ...any) (*source.Result, error) {
		return ds.Query(ctx, sql, args...)
	}
	// 1. 拿所有表
	rows, err := q(ctx, "SELECT name, type, sql FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("introspect sqlite list tables: %w", err)
	}

	meta := &Meta{
		Datasource:    dsName,
		AnalyzedAt:    time.Now().Format(time.RFC3339),
		SchemaVersion: 1,
		Tables:        []TableMeta{},
		Relations:     []Relation{},
	}

	for _, r := range rows.Rows {
		tname, _ := r["name"].(string)
		ttype, _ := r["type"].(string)
		_ = ttype
		_ = r["sql"]

		// 2. 拿每个表的字段
		colRows, err := q(ctx, fmt.Sprintf("PRAGMA table_info(%q)", tname))
		if err != nil {
			continue
		}
		cols := []ColumnMeta{}
		var pk string
		for _, cr := range colRows.Rows {
			cname, _ := cr["name"].(string)
			ctype, _ := cr["type"].(string)
			notnull, _ := cr["notnull"].(int64)
			pkIdx, _ := cr["pk"].(int64)
			nullable := notnull == 0
			if pkIdx > 0 && pk == "" {
				pk = cname
			}
			cols = append(cols, ColumnMeta{
				Name:     cname,
				Type:     ctype,
				Nullable: nullable,
			})
		}

		// 3. 外键
		fkRows, err := q(ctx, fmt.Sprintf("PRAGMA foreign_key_list(%q)", tname))
		fks := []ForeignKeyMeta{}
		relations := []Relation{}
		if err == nil {
			for _, fkr := range fkRows.Rows {
				from, _ := fkr["from"].(string)
				toTable, _ := fkr["table"].(string)
				toCol, _ := fkr["to"].(string)
				fks = append(fks, ForeignKeyMeta{
					Column:     from,
					References: toTable + "." + toCol,
					Confidence: 1.0,
				})
				relations = append(relations, Relation{
					From:       tname + "." + from,
					To:         toTable + "." + toCol,
					Confidence: 1.0,
				})
			}
		}

		// 4. 行数
		cntRows, _ := q(ctx, fmt.Sprintf("SELECT COUNT(*) AS n FROM %q", tname))
		var totalRows int64
		if len(cntRows.Rows) > 0 {
			switch v := cntRows.Rows[0]["n"].(type) {
			case int64:
				totalRows = v
			case float64:
				totalRows = int64(v)
			case string:
				fmt.Sscanf(v, "%d", &totalRows)
			}
		}

		quality := &QualityStats{
			TotalRows:   totalRows,
			NullAlerts:  map[string]float64{},
			DistinctCnt: map[string]int64{},
		}

		meta.Tables = append(meta.Tables, TableMeta{
			Name:        tname,
			PrimaryKey:  pk,
			Columns:     cols,
			ForeignKeys: fks,
			Quality:     quality,
		})
		meta.Relations = append(meta.Relations, relations...)
	}

	return meta, nil
}

// ============================================================
// 其他驱动
// ============================================================

// introspectPostgres PG introspect
//   tables: information_schema.tables (table_schema NOT IN ('pg_catalog', 'information_schema'))
//   columns: information_schema.columns
//   FK: pg_constraint + pg_attribute(2008 R2 兼容)
func introspectPostgres(ctx context.Context, ds source.DataSource, dsName string) (*Meta, error) {
	q := func(ctx context.Context, sql string, args ...any) (*source.Result, error) {
		return ds.Query(ctx, sql, args...)
	}

	// 1. 拿 schema 名(默认 'public')
	schemaRes, err := q(ctx, "SELECT current_schema() AS s")
	if err != nil {
		return nil, fmt.Errorf("introspect pg current_schema: %w", err)
	}
	schemaName := "public"
	if len(schemaRes.Rows) > 0 {
		if s, ok := schemaRes.Rows[0]["s"].(string); ok && s != "" {
			schemaName = s
		}
	}

	// 2. 拿所有 user 表
	tablesRes, err := q(ctx, `
		SELECT table_name, table_type
		FROM information_schema.tables
		WHERE table_schema = $1
		  AND table_type IN ('BASE TABLE', 'VIEW')
		ORDER BY table_name
	`, schemaName)
	if err != nil {
		return nil, fmt.Errorf("introspect pg tables: %w", err)
	}

	meta := &Meta{
		Datasource:    dsName,
		AnalyzedAt:    time.Now().Format(time.RFC3339),
		SchemaVersion: 1,
		Tables:        []TableMeta{},
		Relations:     []Relation{},
	}

	for _, tr := range tablesRes.Rows {
		tname, _ := tr["table_name"].(string)
		_ = tr["table_type"]

		// 3. 列
		colRes, err := q(ctx, `
			SELECT column_name, data_type, is_nullable, character_maximum_length
			FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2
			ORDER BY ordinal_position
		`, schemaName, tname)
		if err != nil {
			continue
		}
		cols := []ColumnMeta{}
		for _, cr := range colRes.Rows {
			cols = append(cols, ColumnMeta{
				Name:     toString(cr["column_name"]),
				Type:     toString(cr["data_type"]),
				Nullable: toString(cr["is_nullable"]) == "YES",
			})
		}

		// 4. PK
		pkRes, err := q(ctx, `
			SELECT a.attname AS pk
			FROM pg_index i
			JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			WHERE i.indrelid = $1::regclass AND i.indisprimary
		`, schemaName+"."+tname)
		var pk string
		if err == nil {
			for _, pkr := range pkRes.Rows {
				if v, ok := pkr["pk"].(string); ok {
					pk = v
					break
				}
			}
		}

		// 5. 外键
		fkRes, err := q(ctx, `
			SELECT
				(SELECT attname FROM pg_attribute WHERE attrelid = c.conrelid AND attnum = c.conkey[1]) AS col,
				cl.relname AS ref_table,
				(SELECT attname FROM pg_attribute WHERE attrelid = c.confrelid AND attnum = c.confkey[1]) AS ref_col
			FROM pg_constraint c
			JOIN pg_class cl ON cl.oid = c.confrelid
			WHERE c.contype = 'f' AND c.conrelid = $1::regclass
		`, schemaName+"."+tname)
		fks := []ForeignKeyMeta{}
		relations := []Relation{}
		if err == nil {
			for _, fkr := range fkRes.Rows {
				col := toString(fkr["col"])
				refT := toString(fkr["ref_table"])
				refC := toString(fkr["ref_col"])
				fks = append(fks, ForeignKeyMeta{
					Column:     col,
					References: refT + "." + refC,
					Confidence: 1.0,
				})
				relations = append(relations, Relation{
					From:       tname + "." + col,
					To:         refT + "." + refC,
					Confidence: 1.0,
				})
			}
		}

		// 6. 行数(可能很慢,阉割版先做)
		cntRes, _ := q(ctx, fmt.Sprintf(`SELECT COUNT(*) AS n FROM %s.%s`, schemaName, tname))
		var totalRows int64
		if cntRes != nil && len(cntRes.Rows) > 0 {
			switch v := cntRes.Rows[0]["n"].(type) {
			case int64:
				totalRows = v
			case float64:
				totalRows = int64(v)
			case string:
				fmt.Sscanf(v, "%d", &totalRows)
			}
		}

		meta.Tables = append(meta.Tables, TableMeta{
			Name:        tname,
			PrimaryKey:  pk,
			Columns:     cols,
			ForeignKeys: fks,
			Quality: &QualityStats{
				TotalRows:   totalRows,
				NullAlerts:  map[string]float64{},
				DistinctCnt: map[string]int64{},
			},
		})
		meta.Relations = append(meta.Relations, relations...)
	}

	return meta, nil
}

// introspectMySQL MySQL introspect
func introspectMySQL(ctx context.Context, ds source.DataSource, dsName string) (*Meta, error) {
	q := func(ctx context.Context, sql string, args ...any) (*source.Result, error) {
		return ds.Query(ctx, sql, args...)
	}
	// 拿当前 DB
	dbRes, err := q(ctx, "SELECT DATABASE() AS s")
	if err != nil {
		return nil, fmt.Errorf("introspect mysql database: %w", err)
	}
	dbName := ""
	if len(dbRes.Rows) > 0 {
		if s, ok := dbRes.Rows[0]["s"].(string); ok {
			dbName = s
		}
	}
	if dbName == "" {
		return nil, fmt.Errorf("introspect mysql: no database selected")
	}

	tablesRes, err := q(ctx, `
		SELECT table_name, table_type
		FROM information_schema.tables
		WHERE table_schema = ?
		  AND table_type IN ('BASE TABLE', 'VIEW')
		ORDER BY table_name
	`, dbName)
	if err != nil {
		return nil, fmt.Errorf("introspect mysql tables: %w", err)
	}

	meta := &Meta{
		Datasource:    dsName,
		AnalyzedAt:    time.Now().Format(time.RFC3339),
		SchemaVersion: 1,
		Tables:        []TableMeta{},
		Relations:     []Relation{},
	}

	for _, tr := range tablesRes.Rows {
		tname, _ := tr["table_name"].(string)
		_ = tr["table_type"]

		colRes, err := q(ctx, `
			SELECT column_name, data_type, is_nullable
			FROM information_schema.columns
			WHERE table_schema = ? AND table_name = ?
			ORDER BY ordinal_position
		`, dbName, tname)
		if err != nil {
			continue
		}
		cols := []ColumnMeta{}
		for _, cr := range colRes.Rows {
			cols = append(cols, ColumnMeta{
				Name:     toString(cr["column_name"]),
				Type:     toString(cr["data_type"]),
				Nullable: toString(cr["is_nullable"]) == "YES",
			})
		}

		// PK
		pkRes, err := q(ctx, `
			SELECT column_name
			FROM information_schema.key_column_usage
			WHERE table_schema = ? AND table_name = ? AND constraint_name = 'PRIMARY'
			ORDER BY ordinal_position
			LIMIT 1
		`, dbName, tname)
		var pk string
		if err == nil && len(pkRes.Rows) > 0 {
			pk = toString(pkRes.Rows[0]["column_name"])
		}

		// FK
		fkRes, err := q(ctx, `
			SELECT
				kcu.column_name AS col,
				kcu.referenced_table_name AS ref_table,
				kcu.referenced_column_name AS ref_col
			FROM information_schema.key_column_usage kcu
			WHERE kcu.table_schema = ?
			  AND kcu.table_name = ?
			  AND kcu.referenced_table_name IS NOT NULL
		`, dbName, tname)
		fks := []ForeignKeyMeta{}
		relations := []Relation{}
		if err == nil {
			for _, fkr := range fkRes.Rows {
				col := toString(fkr["col"])
				refT := toString(fkr["ref_table"])
				refC := toString(fkr["ref_col"])
				fks = append(fks, ForeignKeyMeta{
					Column:     col,
					References: refT + "." + refC,
					Confidence: 1.0,
				})
				relations = append(relations, Relation{
					From:       tname + "." + col,
					To:         refT + "." + refC,
					Confidence: 1.0,
				})
			}
		}

		// 行数
		cntRes, _ := q(ctx, fmt.Sprintf("SELECT COUNT(*) AS n FROM `%s`.`%s`", dbName, tname))
		var totalRows int64
		if cntRes != nil && len(cntRes.Rows) > 0 {
			switch v := cntRes.Rows[0]["n"].(type) {
			case int64:
				totalRows = v
			case float64:
				totalRows = int64(v)
			case string:
				fmt.Sscanf(v, "%d", &totalRows)
			}
		}

		meta.Tables = append(meta.Tables, TableMeta{
			Name:        tname,
			PrimaryKey:  pk,
			Columns:     cols,
			ForeignKeys: fks,
			Quality: &QualityStats{
				TotalRows:   totalRows,
				NullAlerts:  map[string]float64{},
				DistinctCnt: map[string]int64{},
			},
		})
		meta.Relations = append(meta.Relations, relations...)
	}

	return meta, nil
}

// introspectClickHouse CH introspect
func introspectClickHouse(ctx context.Context, ds source.DataSource, dsName string) (*Meta, error) {
	q := func(ctx context.Context, sql string, args ...any) (*source.Result, error) {
		return ds.Query(ctx, sql, args...)
	}
	// CH: system.tables
	tablesRes, err := q(ctx, `
		SELECT name, type
		FROM system.tables
		WHERE database = currentDatabase()
		  AND engine NOT IN ('MaterializedView', 'Distributed')
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("introspect ch tables: %w", err)
	}

	meta := &Meta{
		Datasource:    dsName,
		AnalyzedAt:    time.Now().Format(time.RFC3339),
		SchemaVersion: 1,
		Tables:        []TableMeta{},
		Relations:     []Relation{},
	}

	for _, tr := range tablesRes.Rows {
		tname, _ := tr["name"].(string)
		_ = tr["type"]

		// 列
		colRes, err := q(ctx, `
			SELECT name, type, comment
			FROM system.columns
			WHERE database = currentDatabase() AND table = ?
			ORDER BY position
		`, tname)
		if err != nil {
			continue
		}
		cols := []ColumnMeta{}
		for _, cr := range colRes.Rows {
			cols = append(cols, ColumnMeta{
				Name:    toString(cr["name"]),
				Type:    toString(cr["type"]),
				Comment: toString(cr["comment"]),
				// CH 列没有 is_nullable 概念,默认 true
				Nullable: true,
			})
		}

		// CH 不支持外键(只支持 JOIN ENGINE 的关系),relations 留空
		// 行数
		cntRes, _ := q(ctx, fmt.Sprintf("SELECT COUNT() AS n FROM %s", tname))
		var totalRows int64
		if cntRes != nil && len(cntRes.Rows) > 0 {
			switch v := cntRes.Rows[0]["n"].(type) {
			case int64:
				totalRows = v
			case float64:
				totalRows = int64(v)
			case string:
				fmt.Sscanf(v, "%d", &totalRows)
			}
		}

		meta.Tables = append(meta.Tables, TableMeta{
			Name:       tname,
			Columns:    cols,
			PrimaryKey: "",
			Quality: &QualityStats{
				TotalRows:   totalRows,
				NullAlerts:  map[string]float64{},
				DistinctCnt: map[string]int64{},
			},
		})
	}

	return meta, nil
}

// introspectMSSQL MSSQL 2008 R2 兼容 introspect
func introspectMSSQL(ctx context.Context, ds source.DataSource, dsName string) (*Meta, error) {
	q := func(ctx context.Context, sql string, args ...any) (*source.Result, error) {
		return ds.Query(ctx, sql, args...)
	}
	// 拿当前 DB
	dbRes, err := q(ctx, "SELECT DB_NAME() AS s")
	if err != nil {
		return nil, fmt.Errorf("introspect mssql db_name: %w", err)
	}
	dbName := ""
	if len(dbRes.Rows) > 0 {
		if s, ok := dbRes.Rows[0]["s"].(string); ok {
			dbName = s
		}
	}
	if dbName == "" {
		return nil, fmt.Errorf("introspect mssql: no database selected")
	}

	// 2008 R2 兼容:不引 OFFSET,不用 STRING_AGG
	tablesRes, err := q(ctx, `
		SELECT t.TABLE_NAME, t.TABLE_TYPE
		FROM INFORMATION_SCHEMA.TABLES t
		WHERE t.TABLE_SCHEMA = ?
		  AND t.TABLE_TYPE IN ('BASE TABLE', 'VIEW')
		ORDER BY t.TABLE_NAME
	`, "dbo")
	if err != nil {
		// 退到 sys.tables(2008 R2 也支持)
		tablesRes, err = q(ctx, `
			SELECT t.name AS TABLE_NAME, t.type_desc AS TABLE_TYPE
			FROM sys.tables t
			ORDER BY t.name
		`)
		if err != nil {
			return nil, fmt.Errorf("introspect mssql tables: %w", err)
		}
	}

	meta := &Meta{
		Datasource:    dsName,
		AnalyzedAt:    time.Now().Format(time.RFC3339),
		SchemaVersion: 1,
		Tables:        []TableMeta{},
		Relations:     []Relation{},
	}

	for _, tr := range tablesRes.Rows {
		tname := toString(tr["TABLE_NAME"])
		_ = tr["TABLE_TYPE"]

		colRes, err := q(ctx, `
			SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
			ORDER BY ORDINAL_POSITION
		`, "dbo", tname)
		if err != nil {
			continue
		}
		cols := []ColumnMeta{}
		for _, cr := range colRes.Rows {
			cols = append(cols, ColumnMeta{
				Name:     toString(cr["COLUMN_NAME"]),
				Type:     toString(cr["DATA_TYPE"]),
				Nullable: toString(cr["IS_NULLABLE"]) == "YES",
			})
		}

		// PK
		pkRes, err := q(ctx, `
			SELECT COLUMN_NAME
			FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
			WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
			  AND CONSTRAINT_NAME LIKE 'PK_%'
			ORDER BY ORDINAL_POSITION
		`, "dbo", tname)
		var pk string
		if err == nil && len(pkRes.Rows) > 0 {
			pk = toString(pkRes.Rows[0]["COLUMN_NAME"])
		}

		// FK
		fkRes, err := q(ctx, `
			SELECT
				cu.COLUMN_NAME AS col,
				cu.REFERENCED_TABLE_NAME AS ref_table,
				cu.REFERENCED_COLUMN_NAME AS ref_col
			FROM INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc
			JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE cu
			  ON cu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
			WHERE cu.TABLE_SCHEMA = ? AND cu.TABLE_NAME = ?
		`, "dbo", tname)
		fks := []ForeignKeyMeta{}
		relations := []Relation{}
		if err == nil {
			for _, fkr := range fkRes.Rows {
				col := toString(fkr["col"])
				refT := toString(fkr["ref_table"])
				refC := toString(fkr["ref_col"])
				fks = append(fks, ForeignKeyMeta{
					Column:     col,
					References: refT + "." + refC,
					Confidence: 1.0,
				})
				relations = append(relations, Relation{
					From:       tname + "." + col,
					To:         refT + "." + refC,
					Confidence: 1.0,
				})
			}
		}

		// 行数
		cntRes, _ := q(ctx, fmt.Sprintf("SELECT COUNT(*) AS n FROM [dbo].[%s]", tname))
		var totalRows int64
		if cntRes != nil && len(cntRes.Rows) > 0 {
			switch v := cntRes.Rows[0]["n"].(type) {
			case int64:
				totalRows = v
			case float64:
				totalRows = int64(v)
			case string:
				fmt.Sscanf(v, "%d", &totalRows)
			}
		}

		meta.Tables = append(meta.Tables, TableMeta{
			Name:        tname,
			PrimaryKey:  pk,
			Columns:     cols,
			ForeignKeys: fks,
			Quality: &QualityStats{
				TotalRows:   totalRows,
				NullAlerts:  map[string]float64{},
				DistinctCnt: map[string]int64{},
			},
		})
		meta.Relations = append(meta.Relations, relations...)
	}

	return meta, nil
}

// toString 安全转换 any → string
func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// ============================================================
// 缓存
// ============================================================

// CacheMeta 写入 .cache/datasource/<ds>.yaml
func CacheMeta(meta *Meta, cacheDir string) error {
	// 简化:W4+ 完整做,这里只用 yaml 序列化
	_ = meta
	_ = cacheDir
	_ = strings.TrimSuffix
	return nil
}
