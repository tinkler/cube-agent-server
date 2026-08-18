// Package source 数据源驱动抽象
// 阉割版实现:
//   - SQLite(测试用)
//   - 文件 CSV
// W3+ 实现:
//   - PostgreSQL / MySQL / ClickHouse / SQL Server / Parquet
package source

import (
	"context"
	"fmt"
)

// DataSource 数据源接口
// 所有驱动实现这个接口
type DataSource interface {
	// 元信息
	Name() string
	Dialect() string
	Driver() string

	// 执行
	Query(ctx context.Context, sql string, args ...any) (*Result, error)
	Ping(ctx context.Context) error
	Close() error
}

// Result 查询结果
type Result struct {
	Columns []Column
	Rows    []map[string]any
	Stats   Stats
}

// Column 列信息
type Column struct {
	Name string
	Type string
}

// Stats 执行统计
type Stats struct {
	RowsAffected int64
	DurationMs   int64
}

// Registry 驱动注册表
type Registry struct {
	factories map[string]DriverFactory
}

// DriverFactory 驱动工厂
type DriverFactory func(cfg *DataSourceConfig) (DataSource, error)

// NewRegistry 构造
func NewRegistry() *Registry {
	return &Registry{factories: map[string]DriverFactory{}}
}

// Register 注册驱动
func (r *Registry) Register(driver string, factory DriverFactory) {
	r.factories[driver] = factory
}

// Build 构造数据源
func (r *Registry) Build(cfg *DataSourceConfig) (DataSource, error) {
	f, ok := r.factories[cfg.Driver]
	if !ok {
		return nil, fmt.Errorf("source: unknown driver %q (registered: %v)", cfg.Driver, r.drivers())
	}
	return f(cfg)
}

func (r *Registry) drivers() []string {
	out := make([]string, 0, len(r.factories))
	for k := range r.factories {
		out = append(out, k)
	}
	return out
}

// DataSourceConfig 数据源配置(对应 datasources.yaml)
type DataSourceConfig struct {
	Name    string         `yaml:"name" mapstructure:"name"`
	Type    string         `yaml:"type" mapstructure:"type"`
	Driver  string         `yaml:"driver" mapstructure:"driver"`
	DSN     string         `yaml:"dsn" mapstructure:"dsn"`
	Pool    PoolConfig     `yaml:"pool" mapstructure:"pool"`
	Compat  CompatConfig   `yaml:"compat" mapstructure:"compat"`
	Extra   map[string]any `yaml:"extra" mapstructure:"extra"`
}

// PoolConfig 连接池
type PoolConfig struct {
	MaxOpen    int `yaml:"max_open" mapstructure:"max_open"`
	MaxIdle    int `yaml:"max_idle" mapstructure:"max_idle"`
	MaxLifeSec int `yaml:"max_lifetime_sec" mapstructure:"max_lifetime_sec"`
}

// CompatConfig 方言兼容(SQL Server 2008 R2 等老版本用)
type CompatConfig struct {
	LegacyPagination bool `yaml:"legacy_pagination" mapstructure:"legacy_pagination"`
	DisableStringAgg bool `yaml:"disable_string_agg" mapstructure:"disable_string_agg"`
	DisableTryParse  bool `yaml:"disable_try_parse" mapstructure:"disable_try_parse"`
	ForceNVarchar    bool `yaml:"force_nvarchar" mapstructure:"force_nvarchar"`
}
