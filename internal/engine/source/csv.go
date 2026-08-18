package source

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// CSVSource CSV 文件数据源
//
//   阉割版实现: 把 CSV 内容注入到 SQLite in-memory,然后用 SQLite 跑 query
//   优点: 复用 SQL 引擎,不用自己实现 SQL 解析
//   缺点: 大 CSV 内存占用大;真要支持大文件需要换 DuckDB 或 Polars
//
//   DSN 形式: file:/path/to/file.csv  或  /path/to/file.csv
//   Extra.table_name: 注入到 SQLite 的表名(默认用文件 basename 去后缀)
type CSVSource struct {
	mu       sync.Mutex
	db       *sql.DB
	dsn      string
	columns  []string
	rowCount int
	tableName string
}

// NewCSVSource 构造
func NewCSVSource(cfg *DataSourceConfig) (DataSource, error) {
	// DSN 解析: file:/path 或 /path
	dsn := cfg.DSN
	path := strings.TrimPrefix(dsn, "file:")
	if path == "" {
		return nil, fmt.Errorf("csv: empty path in dsn")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("csv: abs path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("csv: stat %q: %w", abs, err)
	}

	// 表名
	tableName := filepath.Base(abs)
	tableName = strings.TrimSuffix(tableName, filepath.Ext(tableName))
	if tn, ok := cfg.Extra["table_name"].(string); ok && tn != "" {
		tableName = tn
	}

	// 打开 SQLite in-memory
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		return nil, fmt.Errorf("csv: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	// 读 CSV
	cols, count, err := loadCSVIntoSQLite(db, abs, tableName)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return &CSVSource{
		db: db, dsn: abs, columns: cols, rowCount: count, tableName: tableName,
	}, nil
}

func loadCSVIntoSQLite(db *sql.DB, path, tableName string) ([]string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("csv: open: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // 不固定字段数,允许变长

	header, err := r.Read()
	if err != nil {
		return nil, 0, fmt.Errorf("csv: read header: %w", err)
	}
	// 列名转 SQL 标识符(去掉特殊字符)
	cols := make([]string, len(header))
	colDefs := make([]string, len(header))
	for i, h := range header {
		c := sanitizeColumnName(h, i)
		cols[i] = c
		colDefs[i] = fmt.Sprintf(`"%s" TEXT`, c)
	}

	// 建表
	ddl := fmt.Sprintf(`CREATE TABLE "%s" (%s)`, tableName, strings.Join(colDefs, ", "))
	if _, err := db.Exec(ddl); err != nil {
		return nil, 0, fmt.Errorf("csv: create table: %w", err)
	}

	// 插数据
	tx, err := db.Begin()
	if err != nil {
		return nil, 0, fmt.Errorf("csv: begin tx: %w", err)
	}
	placeholders := strings.Repeat("?,", len(cols))
	placeholders = "(" + placeholders[:len(placeholders)-1] + ")"
	stmt, err := tx.Prepare(fmt.Sprintf(`INSERT INTO "%s" VALUES %s`, tableName, placeholders))
	if err != nil {
		_ = tx.Rollback()
		return nil, 0, fmt.Errorf("csv: prepare: %w", err)
	}

	count := 0
	for {
		row, err := r.Read()
		if err != nil {
			break
		}
		// 转 any 列表
		vals := make([]any, len(row))
		for colIdx, v := range row {
			// 尝试解析数字
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				vals[colIdx] = n
			} else if f, err := strconv.ParseFloat(v, 64); err == nil {
				vals[colIdx] = f
			} else {
				vals[colIdx] = v
			}
		}
		if len(vals) < len(cols) {
			// 补 nil
			for j := len(vals); j < len(cols); j++ {
				vals = append(vals, nil)
			}
		} else if len(vals) > len(cols) {
			vals = vals[:len(cols)]
		}
		if _, err := stmt.Exec(vals...); err != nil {
			_ = tx.Rollback()
			return nil, 0, fmt.Errorf("csv: insert row %d: %w", count, err)
		}
		count++
	}
	_ = stmt.Close()
	if err := tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("csv: commit: %w", err)
	}
	return cols, count, nil
}

func sanitizeColumnName(s string, idx int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Sprintf("col_%d", idx)
	}
	// 替换特殊字符
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, s)
	if s[0] >= '0' && s[0] <= '9' {
		s = "_" + s
	}
	return s
}

func (s *CSVSource) Name() string    { return filepath.Base(s.dsn) }
func (s *CSVSource) Dialect() string { return "csv" }
func (s *CSVSource) Driver() string  { return "csv" }

func (s *CSVSource) Query(ctx context.Context, q string, args ...any) (*Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := time.Now()
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("csv query: %w", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	colTypes, _ := rows.ColumnTypes()
	result := &Result{Columns: make([]Column, 0, len(cols))}
	for i, c := range cols {
		typ := ""
		if colTypes != nil && i < len(colTypes) {
			typ = colTypes[i].DatabaseTypeName()
		}
		result.Columns = append(result.Columns, Column{Name: c, Type: typ})
	}
	for rows.Next() {
		holders := make([]any, len(cols))
		scanDests := make([]any, len(cols))
		for i := range holders {
			scanDests[i] = &holders[i]
		}
		if err := rows.Scan(scanDests...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = holders[i]
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result.Stats.DurationMs = time.Since(start).Milliseconds()
	result.Stats.RowsAffected = int64(s.rowCount)
	return result, nil
}

func (s *CSVSource) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *CSVSource) Close() error {
	return s.db.Close()
}
