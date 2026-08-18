// Package middleware 集中所有 HTTP 中间件
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestIDHeader X-Request-ID 请求/响应头名
const RequestIDHeader = "X-Request-ID"

// RequestIDKey gin.Context 里的 key
const RequestIDKey = "request_id"

// RequestID 中间件:
//  1. 读 X-Request-ID header,有则沿用(支持链路追踪)
//  2. 没有则生成 UUID v4
//  3. 写回 response header(双向都带,方便客户端对账)
//  4. 存进 gin.Context 给后续 handler / 日志中间件用
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(RequestIDHeader)
		if rid == "" {
			rid = uuid.NewString()
		}
		c.Set(RequestIDKey, rid)
		c.Writer.Header().Set(RequestIDHeader, rid)
		c.Next()
	}
}

// GetRequestID 取出当前请求的 request_id(供 handler 使用)
func GetRequestID(c *gin.Context) string {
	if v, ok := c.Get(RequestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
