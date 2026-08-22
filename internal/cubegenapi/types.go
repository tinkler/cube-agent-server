// Package cubegenapi 提供给 yaegi 插件的"接口契约"类型
//
//  拆出独立包的原因:避免 internal/cubegen -> internal/yaegisym -> internal/cubegen
//  的 import 循环。yaegi 插件只 import cubegenapi + sqlbuilder,主程序 import cubegen
//  拿真实实现。
//
// 插件用法(plugins/<name>/gen.go):
//
//   package main
//   import (
//       "github.com/tinkler/cube-agent-server/internal/cubegenapi"
//       "github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
//   )
//   type MyGen struct{}
//   func (g *MyGen) BuildSQL(ctx *cubegenapi.BuildContext) (*cubegenapi.SQLPlan, error) {
//       return &cubegenapi.SQLPlan{
//           From: &sqlbuilder.TableRef{...},
//       }, nil
//   }
//   var Source cubegenapi.SQLSource = &MyGen{}
package cubegenapi

import (
	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
)

// SQLSource 动态 SQL 生成器接口
type SQLSource interface {
	BuildSQL(ctx *BuildContext) (*SQLPlan, error)
}

// SQLPlan 插件返回的 SQL 片段(从 + joins + where + 列)
type SQLPlan struct {
	From  *sqlbuilder.TableRef
	Joins []sqlbuilder.JoinClause
	Where sqlbuilder.Expr
	// Cols 显式选择的列(空 = SELECT main_alias.*)
	// 插件需要时填,避免 JOIN 引起的列重复(item_no 出现在 s + i)
	Cols []sqlbuilder.SelectColumn
	Args []any
}

// BuildContext 传给插件的 query 上下文
type BuildContext struct {
	CubeName            string
	RequestedMeasures   []string
	RequestedDimensions []string
	RequestedTimeDim    *TimeDimensionRequest
	RequestedFilters    []FilterRequest
}

// TimeDimensionRequest 时间维度上下文
type TimeDimensionRequest struct {
	Dimension   string
	Granularity string
	DateRange   []string
}

// FilterRequest 过滤条件
type FilterRequest struct {
	Member   string
	Operator string
	Values   []any
}

// HasMeasure 检查 ctx 是否请求了某个 measure
func (ctx *BuildContext) HasMeasure(name string) bool {
	for _, m := range ctx.RequestedMeasures {
		if m == name {
			return true
		}
		if stripCubePrefix(m) == name {
			return true
		}
	}
	return false
}

// HasDimension 检查 ctx 是否请求了某个 dimension
func (ctx *BuildContext) HasDimension(name string) bool {
	for _, d := range ctx.RequestedDimensions {
		if d == name {
			return true
		}
		if stripCubePrefix(d) == name {
			return true
		}
	}
	return false
}

func stripCubePrefix(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return s[i+1:]
		}
	}
	return s
}
