package source

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动(无 CGO)
)

// SQLiteSource SQLite 数据源
// 使用 modernc.org/sqlite(纯 Go,无 CGO)
type SQLiteSource struct {
	db *sql.DB
}

// NewSQLiteSource 构造
func NewSQLiteSource(cfg *DataSourceConfig) (DataSource, error) {
	// DSN 形式: file::memory:?cache=shared  or  ./data.db
	// modernc.org/sqlite 支持 ?_pragma=... 形式
	db, err := sql.Open("sqlite", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	if cfg.Pool.MaxOpen > 0 {
		db.SetMaxOpenConns(cfg.Pool.MaxOpen)
	} else {
		db.SetMaxOpenConns(1) // SQLite 默认单连接
	}
	if cfg.Pool.MaxIdle > 0 {
		db.SetMaxIdleConns(cfg.Pool.MaxIdle)
	}
	if cfg.Pool.MaxLifeSec > 0 {
		db.SetConnMaxLifetime(time.Duration(cfg.Pool.MaxLifeSec) * time.Second)
	}
	return &SQLiteSource{db: db}, nil
}

func (s *SQLiteSource) Name() string   { return "sqlite" }
func (s *SQLiteSource) Dialect() string { return "sqlite" }
func (s *SQLiteSource) Driver() string { return "sqlite" }

func (s *SQLiteSource) Query(ctx context.Context, q string, args ...any) (*Result, error) {
	start := time.Now()
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("sqlite columns: %w", err)
	}
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		// 不致命,继续
		colTypes = nil
	}
	result := &Result{Columns: make([]Column, 0, len(cols))}
	for i, c := range cols {
		typ := ""
		if colTypes != nil && i < len(colTypes) {
			typ = colTypes[i].DatabaseTypeName()
		}
		result.Columns = append(result.Columns, Column{Name: c, Type: typ})
	}
	for rows.Next() {
		// 构造 value holders
		holders := make([]any, len(cols))
		scanDests := make([]any, len(cols))
		for i := range holders {
			scanDests[i] = &holders[i]
		}
		if err := rows.Scan(scanDests...); err != nil {
			return nil, fmt.Errorf("sqlite scan: %w", err)
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = holders[i]
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite rows: %w", err)
	}
	result.Stats.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

func (s *SQLiteSource) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLiteSource) Close() error {
	return s.db.Close()
}
