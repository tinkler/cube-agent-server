// Cube Agent Server - 入口
// W2 D4: gin + plugin manager + schema registry + compiler + SQLite engine
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	configloader "github.com/tinkler/cube-agent-server/config"
	"github.com/tinkler/cube-agent-server/internal/api"
	"github.com/tinkler/cube-agent-server/internal/api/handlers"
	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
	appconfig "github.com/tinkler/cube-agent-server/internal/config"
	"github.com/tinkler/cube-agent-server/internal/cubegen"
	"github.com/tinkler/cube-agent-server/internal/engine"
	"github.com/tinkler/cube-agent-server/internal/engine/source"
	"github.com/tinkler/cube-agent-server/internal/log"
	"github.com/tinkler/cube-agent-server/internal/plugin"
	"github.com/tinkler/cube-agent-server/internal/schema"
	"github.com/tinkler/cube-agent-server/internal/skill"
	"github.com/tinkler/cube-agent-server/internal/skill/datasource"
	"github.com/tinkler/cube-agent-server/internal/skill/llm"

	_ "github.com/tinkler/cube-agent-server/internal/engine/source"
	_ "modernc.org/sqlite"                // 纯 Go SQLite 驱动
	_ "github.com/jackc/pgx/v5/stdlib"   // PG stdlib 驱动
	_ "github.com/go-sql-driver/mysql"   // MySQL 驱动
	_ "github.com/ClickHouse/clickhouse-go/v2" // CH 驱动
	_ "github.com/microsoft/go-mssqldb"  // SQL Server 驱动

	"github.com/prometheus/client_golang/prometheus"
)

const banner = `
   ____      __          __  ___           __     ____
  / __/__ __/ /__  ___  /  |/  /__ _____  / /_   / __/__________ ___ ___
 / _// \ // / -_) _ \/ /|_/ / _ \/ __/ |/ / -_) _\ \/ __/ __/ -_) -_) _ \
/___/\_\\\_\\__/\___/_/  /_/\___/_/  |___/\__/ /___/\__/_/  \__/\__/_//_/

W2 D4  -  SQLite 真数据源 + /v1/load 跑通
`

func main() {
	fmt.Print(banner)

	// 1. 加载配置
	cfg, err := appconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] load config: %v\n", err)
		os.Exit(1)
	}

	// 2. 初始化日志
	logger, err := log.New(log.Config{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
		Output: cfg.Log.Output,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] init logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("cube-agent starting",
		zap.String("http_addr", cfg.Server.HTTPAddr),
		zap.String("plugins_dir", cfg.Plugins.Dir),
		zap.Bool("plugins_watch", cfg.Plugins.Watch),
	)

	// 3. schema Registry
	registry := schema.NewRegistry()

	// 4. 加载数据源
	dsCfgs, err := configloader.LoadDataSources()
	if err != nil {
		logger.Fatal("load datasources", zap.Error(err))
	}
	for _, d := range dsCfgs {
		logger.Info("datasource loaded", zap.String("name", d.Name), zap.String("driver", d.Driver))
	}

	// 5. 注册 source 驱动
	srcReg := source.NewRegistry()
	srcReg.Register("sqlite", source.NewSQLiteSource)
	srcReg.Register("pgx", source.NewPostgresSource)
	srcReg.Register("mysql", source.NewMysqlSource)
	srcReg.Register("clickhouse", source.NewClickHouseSource)
	srcReg.Register("mssql", source.NewMSSQLSource)
	srcReg.Register("csv", source.NewCSVSource)
	// Parquet 留 W4+ 接入(生态不成熟)

	// 6. Executor
	executor := engine.NewExecutor(srcReg, registry)
	executor.SetDatasources(dsCfgs)
	// W5: Prometheus 指标
	promReg := prometheus.NewRegistry()
	promReg.MustRegister(prometheus.NewGoCollector())
	promReg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	promMetrics := engine.NewPrometheusMetrics(promReg)
	executor.SetPrometheusMetrics(promMetrics)

	// per-cube dialect lookup:从 cube 找到它的 datasource,然后映射到 sqlbuilder.Dialect
	dsCache := make(map[string]*source.DataSourceConfig, len(dsCfgs))
	for _, c := range dsCfgs {
		if c != nil {
			dsCache[c.Name] = c
		}
	}
	lookupDialect := func(cubeName string) sqlbuilder.Dialect {
		cwp := registry.Snapshot().CubeWithPlugin(cubeName)
		if cwp == nil {
			return ""
		}
		dsName := cwp.Plugin.Metadata.Datasource
		ds, ok := dsCache[dsName]
		if !ok {
			return ""
		}
		switch ds.Driver {
		case "sqlite":
			return sqlbuilder.DialectSQLite
		case "pgx", "postgres":
			return sqlbuilder.DialectPostgres
		case "mysql":
			return sqlbuilder.DialectMySQL
		case "mssql":
			return sqlbuilder.DialectMSSQL
		case "clickhouse":
			return sqlbuilder.DialectCH
		}
		return ""
	}

	// 7. plugin manager
	pm := plugin.NewManager(
		cfg.Plugins.Dir,
		registry,
		logger,
		cfg.Plugins.ReloadDebounceMs,
		cfg.Plugins.Watch,
		dsNames(dsCfgs),
	)

	// 触发初始 scan(否则 registry.Snapshot() 拿不到 plugin)
	if err := pm.Reload(); err != nil {
		logger.Warn("plugin initial scan", zap.Error(err))
	}

	// 7.5 cubegen: 初始化 yaegi loader,扫描所有 cube 的 dynamic_plugin
	//   失败不致命(各 plugin 各自 fallback)
	cubegenLoader, cerr := cubegen.NewYaegiLoader()
	if cerr != nil {
		logger.Warn("cubegen loader init failed (dynamic plugins disabled)", zap.Error(cerr))
	} else {
		cubegen.SetGlobalLoader(cubegenLoader)
		loaded := 0
		snap := registry.Snapshot()
		for _, p := range snap.Plugins {
			for _, c := range p.Spec.Cubes {
				if c.DynamicPlugin == nil {
					continue
				}
			if lerr := cubegenLoader.LoadFile(c.DynamicPlugin.Path); lerr != nil {
				logger.Warn("cubegen plugin load failed (will fallback to SQL)",
					zap.String("cube", c.Name),
					zap.String("path", c.DynamicPlugin.Path),
					zap.Error(lerr))
			} else {
				loaded++
				logger.Info("cubegen plugin loaded",
					zap.String("cube", c.Name),
					zap.String("path", c.DynamicPlugin.Path))
			}
			}
		}
		logger.Info("cubegen initialized", zap.Int("loaded_plugins", loaded))
	}

	// 8. AI skill builder
	var llmClient llm.Client
	if cfg.AI.LLM.APIKey != "" {
		llmClient, err = llm.NewClient(llm.Config{
			Provider:   cfg.AI.LLM.Provider,
			APIKey:     cfg.AI.LLM.APIKey,
			BaseURL:    cfg.AI.LLM.BaseURL,
			Model:      cfg.AI.LLM.Model,
			Timeout:    cfg.AI.LLM.Timeout,
			MaxRetries: cfg.AI.LLM.MaxRetries,
		})
		if err != nil {
			logger.Warn("LLM client init failed, skill will run in no-LLM mode", zap.Error(err))
		} else {
			logger.Info("LLM client ready", zap.String("model", cfg.AI.LLM.Model))
		}
	}
	introspect := datasource.NewIntrospector(srcReg, dsCfgs)
	builder := skill.NewBuilder(
		llmClient,
		introspect,
		srcReg,
		pm,
		cfg.Plugins.Dir,
		cfg.AI.CacheDir,
		logger,
	)

	// 9. router
	router := api.NewRouter(api.RouterConfig{
		Logger:        logger,
		MetaAPI:       schema.NewMetaProvider(registry),
		PluginManager: pm,
		SchemaReg:     registry,
		QueryDeps: handlers.QueryDeps{
			Registry:      registry,
			Dialect:       sqlbuilder.DialectSQLite, // 兜底,实际走 LookupDialect
			LookupDialect: lookupDialect,
			Executor:      executor,
		},
		DataAdmin:    executor,  // W3:DatasourceAdmin = *engine.Executor
		SkillAdmin:   builder,   // W4:SkillAdmin = *skill.Builder
		PromRegistry: promReg,   // W5:/metrics 端点
	})

	// 9. HTTP server
	srv := &http.Server{
		Addr:              cfg.Server.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
	}

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	go func() {
		logger.Info("http server listening", zap.String("addr", cfg.Server.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("http server failed", zap.Error(err))
		}
	}()

	// 10. plugin manager
	go func() {
		if err := pm.Run(rootCtx); err != nil {
			logger.Error("plugin manager exited", zap.Error(err))
		}
	}()

	// 11. 优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	logger.Info("shutdown signal received", zap.String("signal", sig.String()))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", zap.Error(err))
	}
	rootCancel()
	time.Sleep(100 * time.Millisecond)
	logger.Info("shutdown completed")
}

func dsNames(cfgs []*source.DataSourceConfig) []string {
	out := make([]string, 0, len(cfgs))
	for _, c := range cfgs {
		if c != nil {
			out = append(out, c.Name)
		}
	}
	return out
}
