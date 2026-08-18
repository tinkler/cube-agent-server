package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/tinkler/cube-agent-server/internal/api/middleware"
	"github.com/tinkler/cube-agent-server/internal/engine"
	"github.com/tinkler/cube-agent-server/internal/engine/source"
)

// PluginAdmin /admin/* 端点依赖
// 实现方: *plugin.Manager
type PluginAdmin interface {
	Reload() error
	LoadedCount() int
}

// DatasourceAdmin /admin/datasources 端点依赖
// 实现方: *engine.Executor
type DatasourceAdmin interface {
	DataSourceConfigs() []*source.DataSourceConfig
	PingAll() map[string]string
	Stats() *engine.Stats
}

// AdminDeps /admin 端点需要的依赖
type AdminDeps struct {
	Manager    PluginAdmin
	Logger     *zap.Logger
	SchemaList func() any // 返回 plugin 列表的函数(从 Registry 出)
}

// ListPlugins GET /admin/plugins
// 返回当前已加载 plugin 列表
func ListPlugins(schemaList func() any) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"plugins": schemaList(),
		})
	}
}

// ReloadPlugins POST /admin/reload
// 手动触发 plugin 重新扫描
// 用于: fsnotify 失效 / 测试场景
func ReloadPlugins(deps AdminDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.Manager == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "plugin manager not configured",
			})
			return
		}
		if err := deps.Manager.Reload(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":      "reload failed",
				"detail":     err.Error(),
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		deps.Logger.Info("admin reload triggered",
			zap.String("request_id", middleware.GetRequestID(c)),
		)
		c.JSON(http.StatusOK, gin.H{
			"status":         "ok",
			"loaded":         deps.Manager.LoadedCount(),
			"request_id":     middleware.GetRequestID(c),
		})
	}
}

// ListDatasources GET /admin/datasources
// 列出已注册的数据源
func ListDatasources(admin DatasourceAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		if admin == nil {
			c.JSON(http.StatusOK, gin.H{"datasources": []any{}})
			return
		}
		cfgs := admin.DataSourceConfigs()
		out := make([]gin.H, 0, len(cfgs))
		for _, cfg := range cfgs {
			out = append(out, gin.H{
				"name":           cfg.Name,
				"type":           cfg.Type,
				"driver":         cfg.Driver,
				"dsn_redacted":   redactDSN(cfg.DSN),
				"pool_max_open":  cfg.Pool.MaxOpen,
				"pool_max_idle":  cfg.Pool.MaxIdle,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"datasources": out,
			"request_id":  middleware.GetRequestID(c),
		})
	}
}

// PingDatasources GET /admin/ping
// 验证所有数据源可达
func PingDatasources(admin DatasourceAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		if admin == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "datasource admin not configured",
			})
			return
		}
		results := admin.PingAll()
		allOK := true
		for _, v := range results {
			if v != "ok" {
				allOK = false
				break
			}
		}
		status := http.StatusOK
		if !allOK {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{
			"results":    results,
			"all_ok":     allOK,
			"request_id": middleware.GetRequestID(c),
		})
	}
}

// Stats GET /admin/stats
// 运行时统计(query 计数、耗时、错误)
func Stats(admin DatasourceAdmin) gin.HandlerFunc {
	return func(c *gin.Context) {
		if admin == nil || admin.Stats() == nil {
			c.JSON(http.StatusOK, gin.H{
				"query_total": 0,
				"per_cube":    map[string]any{},
				"per_datasource": map[string]any{},
			})
			return
		}
		c.JSON(http.StatusOK, admin.Stats().Snapshot())
	}
}

// redactDSN 隐藏 DSN 里的密码
func redactDSN(dsn string) string {
	// 简单实现:把 user:pass@ 替换成 user:***@
	out := []byte(dsn)
	i := 0
	for i < len(out) {
		// 找 "://" 之后第一个 "@"
		if i+3 <= len(out) && out[i] == ':' && out[i+1] == '/' && out[i+2] == '/' {
			j := i + 3
			for j < len(out) && out[j] != '@' {
				j++
			}
			if j < len(out) {
				// 找 user:pass 分隔
				k := i + 3
				for k < j && out[k] != ':' {
					k++
				}
				if k < j {
					// 替换 : 到 @ 为 ***
					return string(out[:k+1]) + "***" + string(out[j:])
				}
			}
			break
		}
		i++
	}
	return dsn
}
