// Code generated for yaegi. DO NOT EDIT.
//
// 给 yaegi 解释器的 symbols。手动生成(yaegi extract 跟我们项目名
// cube-agent-server 含 dash 冲突)。
package yaegisym

import (
	"reflect"

	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
	"github.com/tinkler/cube-agent-server/internal/cubegenapi"
)

var Symbols = map[string]map[string]reflect.Value{
	"github.com/tinkler/cube-agent-server/internal/cubegenapi":           cubegenapiSymbols(),
	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder": sqlbuilderSymbols(),
}

func cubegenapiSymbols() map[string]reflect.Value {
	m := map[string]reflect.Value{}
	m["SQLSource"] = reflect.ValueOf((*cubegenapi.SQLSource)(nil))
	m["BuildContext"] = reflect.ValueOf((*cubegenapi.BuildContext)(nil))
	m["SQLPlan"] = reflect.ValueOf((*cubegenapi.SQLPlan)(nil))
	m["TimeDimensionRequest"] = reflect.ValueOf((*cubegenapi.TimeDimensionRequest)(nil))
	m["FilterRequest"] = reflect.ValueOf((*cubegenapi.FilterRequest)(nil))
	return m
}

func sqlbuilderSymbols() map[string]reflect.Value {
	m := map[string]reflect.Value{}
	// 标识符 / 字面量
	m["Col"] = reflect.ValueOf(sqlbuilder.Col)
	m["QCol"] = reflect.ValueOf(sqlbuilder.QCol)
	m["Lit"] = reflect.ValueOf(sqlbuilder.Lit)
	m["P"] = reflect.ValueOf(sqlbuilder.P)
	m["Star_"] = reflect.ValueOf(sqlbuilder.Star_)
	m["DatePart_"] = reflect.ValueOf(sqlbuilder.DatePart_)
	// 二元
	m["Eq"] = reflect.ValueOf(sqlbuilder.Eq)
	m["Ne"] = reflect.ValueOf(sqlbuilder.Ne)
	m["Lt"] = reflect.ValueOf(sqlbuilder.Lt)
	m["Le"] = reflect.ValueOf(sqlbuilder.Le)
	m["Gt"] = reflect.ValueOf(sqlbuilder.Gt)
	m["Ge"] = reflect.ValueOf(sqlbuilder.Ge)
	m["And"] = reflect.ValueOf(sqlbuilder.And)
	m["Or"] = reflect.ValueOf(sqlbuilder.Or)
	m["Like"] = reflect.ValueOf(sqlbuilder.Like)
	// 集合
	m["In"] = reflect.ValueOf(sqlbuilder.In)
	m["NotIn"] = reflect.ValueOf(sqlbuilder.NotIn)
	// 一元
	m["IsNull"] = reflect.ValueOf(sqlbuilder.IsNull)
	m["IsNotNull"] = reflect.ValueOf(sqlbuilder.IsNotNull)
	m["Not_"] = reflect.ValueOf(sqlbuilder.Not_)
	// 聚合
	m["Count"] = reflect.ValueOf(sqlbuilder.Count)
	m["CountStar"] = reflect.ValueOf(sqlbuilder.CountStar)
	m["CountDistinct"] = reflect.ValueOf(sqlbuilder.CountDistinct)
	m["Sum"] = reflect.ValueOf(sqlbuilder.Sum)
	m["Avg"] = reflect.ValueOf(sqlbuilder.Avg)
	m["Min"] = reflect.ValueOf(sqlbuilder.Min)
	m["Max"] = reflect.ValueOf(sqlbuilder.Max)
	m["Coalesce"] = reflect.ValueOf(sqlbuilder.Coalesce)
	// 字符串
	m["RTRIM"] = reflect.ValueOf(sqlbuilder.RTRIM)
	m["LTRIM"] = reflect.ValueOf(sqlbuilder.LTRIM)
	m["TRIM"] = reflect.ValueOf(sqlbuilder.TRIM)
	m["IsNullOrEmpty"] = reflect.ValueOf(sqlbuilder.IsNullOrEmpty)
	// 时间
	m["DateTrunc"] = reflect.ValueOf(sqlbuilder.DateTrunc)
	// 原始 SQL
	m["Raw"] = reflect.ValueOf(sqlbuilder.Raw)
	// 结构体类型
	m["SelectStmt"] = reflect.ValueOf((*sqlbuilder.SelectStmt)(nil))
	m["SelectColumn"] = reflect.ValueOf((*sqlbuilder.SelectColumn)(nil))
	m["TableRef"] = reflect.ValueOf((*sqlbuilder.TableRef)(nil))
	m["JoinClause"] = reflect.ValueOf((*sqlbuilder.JoinClause)(nil))
	m["OrderBy"] = reflect.ValueOf((*sqlbuilder.OrderBy)(nil))
	return m
}
