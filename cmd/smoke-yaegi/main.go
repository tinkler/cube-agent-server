// 完整 yaegi 加载 + 插件 BuildSQL + SQL 渲染
package main

import (
	"fmt"
	"log"

	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
	"github.com/tinkler/cube-agent-server/internal/cubegen"
	"github.com/tinkler/cube-agent-server/internal/cubegenapi"
)

func main() {
	loader, err := cubegen.NewYaegiLoader()
	if err != nil {
		log.Fatal(err)
	}

	if err := loader.LoadFile("./plugins/supplier_sales/gen/gen.go"); err != nil {
		log.Fatalf("LoadFile: %v", err)
	}
	fmt.Println("✓ 插件加载成功")

	// 场景 1:不要求 item 维度(只查 supplier / branch 维度)
	ctx1 := &cubegenapi.BuildContext{
		CubeName:            "supplier_sales",
		RequestedMeasures:   []string{"supplier_sales.count"},
		RequestedDimensions: []string{"supplier_sales.main_supcust", "supplier_sales.branch_no"},
	}
	plan1, err := loader.CallBuildSQL("./plugins/supplier_sales/gen/gen.go", ctx1)
	if err != nil {
		log.Fatalf("CallBuildSQL 1: %v", err)
	}
	fmt.Printf("\n=== 场景 1:不要求销量 ===\n")
	if plan1.From != nil {
		fmt.Printf("  From: %s\n", plan1.From.Source)
	}
	fmt.Printf("  Joins: %d\n", len(plan1.Joins))
	if plan1.Where != nil {
		r := sqlbuilder.NewRenderer(sqlbuilder.DialectMSSQL)
		r2, _ := r.RenderExpr(plan1.Where)
		fmt.Printf("  Where: %s\n", r2)
	}

	// 场景 2:要求 item 维度(要 JOIN item 表)
	ctx2 := &cubegenapi.BuildContext{
		CubeName:            "supplier_sales",
		RequestedMeasures:   []string{"supplier_sales.total_revenue", "supplier_sales.total_gross_profit"},
		RequestedDimensions: []string{"supplier_sales.item_brand", "supplier_sales.main_supcust"},
	}
	plan2, err := loader.CallBuildSQL("./plugins/supplier_sales/gen/gen.go", ctx2)
	if err != nil {
		log.Fatalf("CallBuildSQL 2: %v", err)
	}
	fmt.Printf("\n=== 场景 2:要求销量 ===\n")
	if plan2.From != nil {
		fmt.Printf("  From: %s\n", plan2.From.Source)
	}
	fmt.Printf("  Joins: %d\n", len(plan2.Joins))
	for i, j := range plan2.Joins {
		r := sqlbuilder.NewRenderer(sqlbuilder.DialectMSSQL)
		joinSQL, _ := r.RenderExpr(j.On)
		fmt.Printf("    [%d] %s JOIN %s ON %s\n", i, j.Type, j.Table.Source, joinSQL)
	}
	if plan2.Where != nil {
		r := sqlbuilder.NewRenderer(sqlbuilder.DialectMSSQL)
		whereSQL, _ := r.RenderExpr(plan2.Where)
		fmt.Printf("  Where: %s\n", whereSQL)
	}

	// 渲染完整 SQL(从 + joins + where 拼成 derived table)
	fmt.Printf("\n=== 渲染完整 SQL(场景 2) ===\n")
	from := &sqlbuilder.TableRef{Name: "ss", Subquery: &sqlbuilder.SelectStmt{
		Columns: []sqlbuilder.SelectColumn{
			{Expr: sqlbuilder.Star_()},
		},
		From: plan2.From,
		Joins: plan2.Joins,
		Where: plan2.Where,
	}}
	stmt := &sqlbuilder.SelectStmt{
		Columns: []sqlbuilder.SelectColumn{
			{Expr: sqlbuilder.QCol("ss", "main_supcust"), Alias: "main_supcust"},
			{Expr: sqlbuilder.QCol("ss", "sale_money"), Alias: "total_revenue"},
		},
		From: from,
		Limit: 5,
	}
	r := sqlbuilder.NewRenderer(sqlbuilder.DialectMSSQL)
	sql, err := r.RenderSelect(stmt)
	if err != nil {
		log.Fatalf("RenderSelect: %v", err)
	}
	fmt.Println(sql)
}
