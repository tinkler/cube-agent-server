package source

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SQLSource 通用 SQL 数据源基类
//   PG/MySQL/SQL Server 都基于这个,只差 driver name 和 DSN
type SQLSource struct {
	name     string
	dialect  string
	driver   string
	dsn      string
	pool     PoolConfig
	db       *sql.DB
	placeholders string // $1 vs ?
}

// openSQL 打开 SQL 连接
func openSQL(name, dialect, driver, dsn string, pool PoolConfig, placeholder string) (*SQLSource, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("%s open: %w", dialect, err)
	}
	if pool.MaxOpen > 0 {
		db.SetMaxOpenConns(pool.MaxOpen)
	} else {
		db.SetMaxOpenConns(25) // SQL 类默认 25
	}
	if pool.MaxIdle > 0 {
		db.SetMaxIdleConns(pool.MaxIdle)
	}
	if pool.MaxLifeSec > 0 {
		db.SetConnMaxLifetime(time.Duration(pool.MaxLifeSec) * time.Second)
	}
	// 立即 ping 一次,失败立刻报错(避免 lazy 触发)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s ping: %w", dialect, err)
	}
	return &SQLSource{
		name: name, dialect: dialect, driver: driver, dsn: dsn, pool: pool, db: db,
		placeholders: placeholder,
	}, nil
}

func (s *SQLSource) Name() string    { return s.name }
func (s *SQLSource) Dialect() string { return s.dialect }
func (s *SQLSource) Driver() string  { return s.driver }

func (s *SQLSource) Query(ctx context.Context, q string, args ...any) (*Result, error) {
	start := time.Now()
	// SQL Server 用 @p1/@p2 命名占位符,不支持匿名 ?。
	// 把 ? 顺序替换成 @p1, @p2, ..., @pN(只对 mssql dialect 生效)。
	// 业务 SQL 里不会在 string literal 中出现 ?,所以 naive replace 安全。
	if s.dialect == "mssql" && strings.Contains(q, "?") {
		q = rewritePlaceholders(q, len(args))
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s query: %w", s.dialect, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("%s columns: %w", s.dialect, err)
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
		// 对 decimal/numeric/money 列用 string target:go-mssqldb 1.10+ 默认返回
		// base64 编码字符串(为保留精度),Scan 到 string 让 driver 直接给字符串。
		for i := range holders {
			if isDecimalType(colTypes, i) {
				var s string
				holders[i] = &s
				scanDests[i] = &s
			} else {
				scanDests[i] = &holders[i]
			}
		}
		if err := rows.Scan(scanDests...); err != nil {
			return nil, fmt.Errorf("%s scan: %w", s.dialect, err)
		}
		// 解包:对 decimal 列 scan 到 *string 后,放回 holders 的是指针
		for i := range holders {
			if isDecimalType(colTypes, i) {
				if sp, ok := holders[i].(*string); ok {
					if sp != nil {
						holders[i] = *sp
					} else {
						holders[i] = nil
					}
				}
			}
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = holders[i]
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s rows: %w", s.dialect, err)
	}
	result.Stats.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

func (s *SQLSource) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLSource) Close() error {
	return s.db.Close()
}

// isDecimalType 判断第 i 列是否是 decimal/numeric/money 类型
// (go-mssqldb 1.10+ 对这些类型默认 base64 编码 Scan 到 string)
func isDecimalType(colTypes []*sql.ColumnType, i int) bool {
	if colTypes == nil || i >= len(colTypes) {
		return false
	}
	name := strings.ToLower(colTypes[i].DatabaseTypeName())
	switch name {
	case "decimal", "numeric", "money", "smallmoney":
		return true
	}
	return false
}

// rewritePlaceholders 把 SQL 里的 ? 替换成 @p1, @p2, ..., @pN。
// 调用方需保证 args 数量与 ? 数量一致;多则用前 N 个,少则尾部参数是 nil。
func rewritePlaceholders(q string, n int) string {
	var b strings.Builder
	b.Grow(len(q) + 8*n)
	idx := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			idx++
			b.WriteString(fmt.Sprintf("@p%d", idx))
		} else {
			b.WriteByte(q[i])
		}
	}
	_ = n // 参数数量由 args 决定,这里只生成同样数量的占位符
	return b.String()
}
