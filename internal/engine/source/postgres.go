package source

import (
	_ "github.com/jackc/pgx/v5/stdlib" // 暴露成 database/sql driver
)

// NewPostgresSource 构造 PG 数据源
// driver: pgx
// DSN 形式: postgres://user:pass@host:5432/dbname?sslmode=disable
//        或 keyword=value 形式: host=localhost user=postgres ...
func NewPostgresSource(cfg *DataSourceConfig) (DataSource, error) {
	return openSQL(cfg.Name, "postgres", "pgx", cfg.DSN, cfg.Pool, "$%d")
}
