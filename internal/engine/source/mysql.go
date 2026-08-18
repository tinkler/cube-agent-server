package source

import (
	_ "github.com/go-sql-driver/mysql"
)

// NewMysqlSource 构造 MySQL 数据源
// DSN 形式: user:pass@tcp(host:3306)/dbname?parseTime=true
func NewMysqlSource(cfg *DataSourceConfig) (DataSource, error) {
	return openSQL(cfg.Name, "mysql", "mysql", cfg.DSN, cfg.Pool, "?")
}
