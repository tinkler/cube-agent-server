// Package engine 查询执行器
// 阉割版: 从 cube 找 datasource,用 DataSource.Query 执行 SQL
package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tinkler/cube-agent-server/internal/engine/source"
	"github.com/tinkler/cube-agent-server/internal/schema"
)

// Executor /v1/load 的执行器
type Executor struct {
	sourceReg  *source.Registry
	schemaReg  *schema.Registry
	cache      map[string]*source.DataSourceConfig // ds name → config
	stats      *Stats
	prom       *PrometheusMetrics
}

// NewExecutor 构造
func NewExecutor(sourceReg *source.Registry, schemaReg *schema.Registry) *Executor {
	return &Executor{
		sourceReg: sourceReg,
		schemaReg: schemaReg,
		cache:     map[string]*source.DataSourceConfig{},
		stats:     NewStats(),
	}
}

// SetPrometheusMetrics 注入 Prometheus(可选)
func (e *Executor) SetPrometheusMetrics(m *PrometheusMetrics) {
	e.prom = m
}

// Stats 返回统计
func (e *Executor) Stats() *Stats { return e.stats }

// SetDatasources 设置数据源配置
func (e *Executor) SetDatasources(cfgs []*source.DataSourceConfig) {
	e.cache = map[string]*source.DataSourceConfig{}
	for _, c := range cfgs {
		if c != nil {
			e.cache[c.Name] = c
		}
	}
}

// DataSourceConfigs 返回所有数据源配置(给 admin 端点用)
func (e *Executor) DataSourceConfigs() []*source.DataSourceConfig {
	out := make([]*source.DataSourceConfig, 0, len(e.cache))
	for _, c := range e.cache {
		out = append(out, c)
	}
	return out
}

// PingAll 验证所有数据源可达
func (e *Executor) PingAll() map[string]string {
	results := make(map[string]string, len(e.cache))
	for name, cfg := range e.cache {
		ds, err := e.sourceReg.Build(cfg)
		if err != nil {
			results[name] = "build failed: " + err.Error()
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = ds.Ping(ctx)
		cancel()
		_ = ds.Close()
		if err != nil {
			results[name] = "ping failed: " + err.Error()
		} else {
			results[name] = "ok"
		}
	}
	return results
}

// Execute gin 适配的查询执行
// 期望 gin.Context 里有 "primary_cube" key(由 LoadFromContext 注入)
func (e *Executor) Execute(c *gin.Context, sql string, args []any) (any, error) {
	cubeName, _ := c.Get("primary_cube")
	cn, _ := cubeName.(string)
	if cn == "" {
		return nil, fmt.Errorf("engine: primary cube not in context")
	}
	cwp := e.schemaReg.Snapshot().CubeWithPlugin(cn)
	if cwp == nil {
		return nil, fmt.Errorf("engine: cube %q not found", cn)
	}
	dsName := cwp.Plugin.Metadata.Datasource
	if dsName == "" {
		return nil, fmt.Errorf("engine: cube %q has no datasource", cn)
	}
	cfg, ok := e.cache[dsName]
	if !ok {
		return nil, fmt.Errorf("engine: datasource %q not registered", dsName)
	}

	start := time.Now()
	ds, err := e.sourceReg.Build(cfg)
	if err != nil {
		e.stats.Record(cn, dsName, cfg.Driver, time.Since(start).Milliseconds(), 0, err)
		e.prom.RecordQuery(cn, dsName, cfg.Driver, time.Since(start).Milliseconds(), 0, err)
		return nil, fmt.Errorf("engine: build datasource %q: %w", dsName, err)
	}
	defer ds.Close()

	res, err := ds.Query(toCtx(c), sql, args...)
	durationMs := time.Since(start).Milliseconds()
	rowCount := 0
	if res != nil {
		rowCount = len(res.Rows)
	}
	e.stats.Record(cn, dsName, cfg.Driver, durationMs, rowCount, err)
	e.prom.RecordQuery(cn, dsName, cfg.Driver, durationMs, rowCount, err)
	if err != nil {
		return nil, fmt.Errorf("engine: query: %w", err)
	}
	return res, nil
}

// toCtx 从 gin.Context 提取 context.Context
func toCtx(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}

// LoadFromContext 把 cube 名塞到 gin.Context(给 Execute 用)
func LoadFromContext(c *gin.Context, cubeName string) {
	c.Set("primary_cube", cubeName)
}
