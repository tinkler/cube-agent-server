// Package cubegen 提供 cube 的动态 SQL 生成能力
//   yaegi 加载 .go 文件,提取 cubegenapi.SQLSource 变量,塞到 Registry
//   Pass3 在 SQL 编译时优先查 Registry,失败 fallback 到 cube.SQL(YAML 静态 SQL)
package cubegen

import (
	"github.com/tinkler/cube-agent-server/internal/cubegenapi"
)

// 兼容老引用(其他代码可能 import "internal/cubegen" 的 SQLSource)
type SQLSource = cubegenapi.SQLSource
type SQLPlan = cubegenapi.SQLPlan
type BuildContext = cubegenapi.BuildContext
type TimeDimensionRequest = cubegenapi.TimeDimensionRequest
type FilterRequest = cubegenapi.FilterRequest