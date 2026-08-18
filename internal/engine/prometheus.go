package engine

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusMetrics 包装 Prometheus 指标
type PrometheusMetrics struct {
	QueryTotal     *prometheus.CounterVec
	QueryDuration  *prometheus.HistogramVec
	QueryErrors    *prometheus.CounterVec
	RowsReturned   *prometheus.HistogramVec
	PluginLoaded   prometheus.Gauge
}

// NewPrometheusMetrics 构造 + 注册到默认 registry
func NewPrometheusMetrics(reg prometheus.Registerer) *PrometheusMetrics {
	m := &PrometheusMetrics{
		QueryTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "cube_agent",
			Subsystem: "query",
			Name:      "total",
			Help:      "Total number of queries executed",
		}, []string{"cube", "datasource", "status"}),
		QueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "cube_agent",
			Subsystem: "query",
			Name:      "duration_ms",
			Help:      "Query execution duration in milliseconds",
			Buckets:   []float64{1, 5, 10, 50, 100, 500, 1000, 5000, 30000},
		}, []string{"cube", "datasource"}),
		QueryErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "cube_agent",
			Subsystem: "query",
			Name:      "errors",
			Help:      "Total number of query errors",
		}, []string{"cube", "datasource", "error_kind"}),
		RowsReturned: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "cube_agent",
			Subsystem: "query",
			Name:      "rows",
			Help:      "Number of rows returned per query",
			Buckets:   []float64{0, 1, 10, 100, 1000, 10000, 100000},
		}, []string{"cube", "datasource"}),
		PluginLoaded: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "cube_agent",
			Subsystem: "plugin",
			Name:      "loaded",
			Help:      "Number of loaded plugins",
		}),
	}
	reg.MustRegister(m.QueryTotal, m.QueryDuration, m.QueryErrors, m.RowsReturned, m.PluginLoaded)
	return m
}

// RecordQuery 上报一次 query
func (m *PrometheusMetrics) RecordQuery(cube, dsName, driver string, durationMs int64, rowCount int, err error) {
	if m == nil {
		return
	}
	status := "success"
	errorKind := ""
	if err != nil {
		status = "error"
		errorKind = "exec"
		if cube == "" {
			errorKind = "build"
		}
		m.QueryErrors.WithLabelValues(cube, dsName, errorKind).Inc()
	}
	m.QueryTotal.WithLabelValues(cube, dsName, status).Inc()
	m.QueryDuration.WithLabelValues(cube, dsName).Observe(float64(durationMs))
	m.RowsReturned.WithLabelValues(cube, dsName).Observe(float64(rowCount))
}

// MetricsHandler 返回 gin handler for /metrics
func MetricsHandler(reg *prometheus.Registry) gin.HandlerFunc {
	h := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: false,
	})
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

// Helper: convert int64 to string
func int64ToStr(n int64) string { return strconv.FormatInt(n, 10) }

// 用 example 时间避免 unused import
var _ = time.Second
