// Package api 提供 HTTP API 层
// 路由设计:
//   公共:   /livez, /readyz
//   v1:     /v1/meta, /v1/load, /v1/sql, /v1/dry-run
//   兼容:   /cubejs-api/v1/meta(同 /v1/meta)
//   admin:  /admin/reload, /admin/plugins, /admin/skill/*
package api

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/tinkler/cube-agent-server/internal/api/handlers"
	"github.com/tinkler/cube-agent-server/internal/api/middleware"
	"github.com/tinkler/cube-agent-server/internal/engine"
	"github.com/tinkler/cube-agent-server/internal/schema"
)

// RouterConfig router 装配参数
type RouterConfig struct {
	Logger        *zap.Logger
	MetaAPI       handlers.MetaProvider
	PluginManager handlers.PluginAdmin
	SchemaReg     *schema.Registry        // D5 新增,用于 /admin/plugins
	QueryDeps     handlers.QueryDeps      // W2 新增,用于 /v1/load /v1/sql /v1/dry-run
	DataAdmin     handlers.DatasourceAdmin // W3 新增,用于 /admin/datasources
	SkillAdmin    handlers.SkillAdmin      // W4 新增,用于 /admin/skill/*
	PromRegistry  *prometheus.Registry    // W5 新增,可选,接 /metrics
}

// NewRouter 构造 gin.Engine
func NewRouter(cfg RouterConfig) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// 中间件栈(顺序敏感)
	r.Use(middleware.RequestID())
	r.Use(middleware.Recovery(cfg.Logger))
	r.Use(middleware.Logger(cfg.Logger))

	// 公共健康端点
	r.GET("/livez", handlers.Liveness)
	r.GET("/readyz", handlers.Readiness(cfg.PluginManager))
	// Prometheus 指标(可选)
	if cfg.PromRegistry != nil {
		r.GET("/metrics", engine.MetricsHandler(cfg.PromRegistry))
	}

	// v1 主接口
	v1 := r.Group("/v1")
	{
		v1.GET("/meta", handlers.GetMeta(cfg.MetaAPI))
		v1.POST("/load", handlers.Load(cfg.QueryDeps))
		v1.POST("/sql", handlers.Sql(cfg.QueryDeps))
		v1.POST("/dry-run", handlers.DryRun(cfg.QueryDeps))
	}

	// cube.js 兼容路径(老 BI 工具用)
	cubejs := r.Group("/cubejs-api/v1")
	{
		cubejs.GET("/meta", handlers.GetMeta(cfg.MetaAPI))
	}

	// admin 接口
	admin := r.Group("/admin")
	{
		admin.GET("/plugins", handlers.ListPlugins(func() any {
			if cfg.SchemaReg == nil {
				return []any{}
			}
			return cfg.SchemaReg.ListPlugins()
		}))
		admin.POST("/reload", handlers.ReloadPlugins(handlers.AdminDeps{
			Manager: cfg.PluginManager,
			Logger:  cfg.Logger,
		}))
		admin.GET("/datasources", handlers.ListDatasources(cfg.DataAdmin))
		admin.GET("/ping", handlers.PingDatasources(cfg.DataAdmin))
		admin.GET("/stats", handlers.Stats(cfg.DataAdmin))
		// W4: AI skill 引导
		admin.POST("/skill/build", handlers.SkillStart(cfg.SkillAdmin))
		admin.POST("/skill/auto-build", handlers.SkillAutoBuild(cfg.SkillAdmin)) // W5: 全自动
		admin.GET("/skill/sessions", handlers.SkillList(cfg.SkillAdmin))
		admin.GET("/skill/session/:id", handlers.SkillGet(cfg.SkillAdmin))
		admin.DELETE("/skill/session/:id", handlers.SkillCancel(cfg.SkillAdmin))
		admin.POST("/skill/step/datasource", handlers.SkillStep2(cfg.SkillAdmin))
		admin.POST("/skill/step/analyze", handlers.SkillStep3(cfg.SkillAdmin))
		admin.POST("/skill/step/design", handlers.SkillStep4(cfg.SkillAdmin))
		admin.POST("/skill/step/generate", handlers.SkillStep5(cfg.SkillAdmin))
		admin.POST("/skill/step/validate", handlers.SkillStep6(cfg.SkillAdmin))
		admin.POST("/skill/step/publish", handlers.SkillStep7(cfg.SkillAdmin))
	}

	return r
}
