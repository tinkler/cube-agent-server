package source

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// NewMSSQLSource 构造 SQL Server 数据源
// driver: sqlserver
// DSN 形式: sqlserver://sa:pass@host:1433?database=erp&encrypt=disable&trustservercertificate=true
//        或 keyword=value 形式
// ⭐ 2008 R2 兼容:DSN 必须带 encrypt=disable + trustservercertificate=true
func NewMSSQLSource(cfg *DataSourceConfig) (DataSource, error) {
	db, err := sql.Open("sqlserver", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("mssql open: %w", err)
	}
	if cfg.Pool.MaxOpen > 0 {
		db.SetMaxOpenConns(cfg.Pool.MaxOpen)
	} else {
		db.SetMaxOpenConns(10)
	}
	if cfg.Pool.MaxIdle > 0 {
		db.SetMaxIdleConns(cfg.Pool.MaxIdle)
	}
	if cfg.Pool.MaxLifeSec > 0 {
		db.SetConnMaxLifetime(time.Duration(cfg.Pool.MaxLifeSec) * time.Second)
	}
	// SQL Server 2008 R2 兼容:连接超时短一些
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mssql ping (检查 DSN 是否带 encrypt=disable+trustservercertificate=true): %w", err)
	}
	return &SQLSource{
		name: cfg.Name, dialect: "mssql", driver: "mssql", dsn: cfg.DSN,
		pool: cfg.Pool, db: db, placeholders: "?",
	}, nil
}
