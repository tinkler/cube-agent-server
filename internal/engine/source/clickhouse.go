package source

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

// NewClickHouseSource 构造 ClickHouse 数据源
// driver: clickhouse
// DSN 形式: clickhouse://user:pass@host:9000/dbname?dial_timeout=10s
func NewClickHouseSource(cfg *DataSourceConfig) (DataSource, error) {
	db, err := sql.Open("clickhouse", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}
	if cfg.Pool.MaxOpen > 0 {
		db.SetMaxOpenConns(cfg.Pool.MaxOpen)
	} else {
		db.SetMaxOpenConns(10) // CH 默认 10
	}
	if cfg.Pool.MaxIdle > 0 {
		db.SetMaxIdleConns(cfg.Pool.MaxIdle)
	}
	if cfg.Pool.MaxLifeSec > 0 {
		db.SetConnMaxLifetime(time.Duration(cfg.Pool.MaxLifeSec) * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}
	return &SQLSource{
		name: cfg.Name, dialect: "clickhouse", driver: "clickhouse", dsn: cfg.DSN,
		pool: cfg.Pool, db: db, placeholders: "?",
	}, nil
}
