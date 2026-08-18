package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetMeta /v1/meta 和 /cubejs-api/v1/meta 的处理器
// 返回 cube.js 兼容的元数据格式
//
// 格式说明(对齐 cube.js):
//   cubes:  所有 cube 的定义(measures/dimensions/segments)
//   {cube_name}:  单 cube 的 details
//
// D2: 返回硬编码 mock,D3 切到 Registry 真实数据
func GetMeta(provider MetaProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		if provider == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "meta provider not configured",
			})
			return
		}
		c.JSON(http.StatusOK, provider.GetMeta())
	}
}
