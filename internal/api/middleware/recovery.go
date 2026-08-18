package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Recovery panic 恢复中间件:
//  1. 捕获后续 handler 抛出的 panic
//  2. 记录错误日志(含 stacktrace)
//  3. 返回 500 + JSON 错误体,保证 server 不挂
func Recovery(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				rid := GetRequestID(c)
				logger.Error("panic recovered",
					zap.String("request_id", rid),
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())),
					zap.String("path", c.Request.URL.Path),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":      "internal server error",
					"request_id": rid,
				})
			}
		}()
		c.Next()
	}
}
