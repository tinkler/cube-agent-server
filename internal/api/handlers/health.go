package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/tinkler/cube-agent-server/internal/api/middleware"
)

// Liveness 存活探针
// 永远 200,只要进程没死就 OK
// k8s livenessProbe 配这个
func Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"request_id": middleware.GetRequestID(c),
	})
}

// Readiness 就绪探针
// D4 阶段: 至少 1 个 plugin 加载成功才 200
// 否则 503(用于 k8s readinessProbe 配合滚动升级)
func Readiness(checker ReadinessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if checker == nil || checker.LoadedCount() == 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":     "not_ready",
				"reason":     "no plugins loaded",
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":         "ok",
			"plugins_loaded": checker.LoadedCount(),
			"request_id":     middleware.GetRequestID(c),
		})
	}
}

// NotImplemented 通用 501 处理器,占位用
func NotImplemented(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error":      "not implemented",
			"endpoint":   name,
			"request_id": middleware.GetRequestID(c),
		})
	}
}
