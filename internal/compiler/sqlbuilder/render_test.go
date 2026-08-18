package sqlbuilder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender_SimpleSelect(t *testing.T) {
	r := NewRenderer(DialectPostgres)
	stmt := &SelectStmt{
		Columns: []SelectColumn{
			{Expr: Col("id"), Alias: "id"},
			{Expr: Col("name"), Alias: "name"},
		},
		From: &TableRef{Name: "users", Source: "users"},
	}
	sql, err := r.RenderSelect(stmt)
	require.NoError(t, err)
	assert.Equal(t, `SELECT "id" AS "id", "name" AS "name" FROM "users"`, sql)
}

func TestRender_SelectStar(t *testing.T) {
	r := NewRenderer(DialectSQLite)
	stmt := &SelectStmt{
		Columns: []SelectColumn{{Expr: Star_()}},
		From:    &TableRef{Name: "t"},
	}
	sql, err := r.RenderSelect(stmt)
	require.NoError(t, err)
	assert.Equal(t, `SELECT * FROM "t"`, sql)
}

func TestRender_WhereAndGroupBy(t *testing.T) {
	r := NewRenderer(DialectPostgres)
	stmt := &SelectStmt{
		Columns: []SelectColumn{
			{Expr: CountStar(), Alias: "cnt"},
			{Expr: Col("status"), Alias: "status"},
		},
		From:    &TableRef{Name: "orders"},
		Where:   Eq(Col("status"), Lit("paid")),
		GroupBy: []Expr{Col("status")},
	}
	sql, err := r.RenderSelect(stmt)
	require.NoError(t, err)
	assert.Equal(t, `SELECT COUNT(*) AS "cnt", "status" AS "status" FROM "orders" WHERE ("status" = 'paid') GROUP BY "status"`, sql)
}

func TestRender_Aggregate(t *testing.T) {
	r := NewRenderer(DialectPostgres)
	stmt := &SelectStmt{
		Columns: []SelectColumn{
			{Expr: Sum(Col("amount")), Alias: "total"},
		},
		From: &TableRef{Name: "orders"},
	}
	sql, err := r.RenderSelect(stmt)
	require.NoError(t, err)
	assert.Equal(t, `SELECT SUM("amount") AS "total" FROM "orders"`, sql)
}

func TestRender_OrderByLimit(t *testing.T) {
	r := NewRenderer(DialectPostgres)
	stmt := &SelectStmt{
		Columns: []SelectColumn{
			{Expr: Col("id"), Alias: "id"},
		},
		From:    &TableRef{Name: "t"},
		OrderBy: []OrderBy{{Expr: Col("created_at"), Desc: true}},
		Limit:   10,
		Offset:  20,
	}
	sql, err := r.RenderSelect(stmt)
	require.NoError(t, err)
	assert.Equal(t, `SELECT "id" AS "id" FROM "t" ORDER BY "created_at" DESC LIMIT 10 OFFSET 20`, sql)
}

func TestRender_Params(t *testing.T) {
	r := NewRenderer(DialectPostgres)
	stmt := &SelectStmt{
		Columns: []SelectColumn{
			{Expr: Col("id"), Alias: "id"},
		},
		From: &TableRef{Name: "t"},
		Where: &BinaryOp{
			Op: "AND",
			L:  Eq(Col("status"), P(1)),
			R:  Gt(Col("amount"), P(2)),
		},
	}
	sql, err := r.RenderSelect(stmt)
	require.NoError(t, err)
	assert.Equal(t, `SELECT "id" AS "id" FROM "t" WHERE ("status" = ? AND "amount" > ?)`, sql)
}

func TestRender_Join(t *testing.T) {
	r := NewRenderer(DialectPostgres)
	stmt := &SelectStmt{
		Columns: []SelectColumn{
			{Expr: CountStar(), Alias: "cnt"},
		},
		From: &TableRef{Name: "orders", Source: "orders"},
		Joins: []JoinClause{
			{
				Type:  "LEFT",
				Table: &TableRef{Name: "users", Source: "users"},
				On:    Eq(QCol("orders", "user_id"), QCol("users", "id")),
			},
		},
	}
	sql, err := r.RenderSelect(stmt)
	require.NoError(t, err)
	assert.True(t, strings.Contains(sql, `LEFT JOIN "users"`))
	assert.True(t, strings.Contains(sql, `"orders"."user_id" = "users"."id"`))
}

func TestRender_InExpr(t *testing.T) {
	r := NewRenderer(DialectPostgres)
	stmt := &SelectStmt{
		Columns: []SelectColumn{{Expr: Col("id"), Alias: "id"}},
		From:    &TableRef{Name: "t"},
		Where:   In(Col("status"), []Expr{P(1), P(2), P(3)}),
	}
	sql, err := r.RenderSelect(stmt)
	require.NoError(t, err)
	assert.Equal(t, `SELECT "id" AS "id" FROM "t" WHERE ("status" IN (?, ?, ?))`, sql)
}

func TestRender_MySQLBacktick(t *testing.T) {
	r := NewRenderer(DialectMySQL)
	stmt := &SelectStmt{
		Columns: []SelectColumn{{Expr: Col("id"), Alias: "id"}},
		From:    &TableRef{Name: "t"},
	}
	sql, err := r.RenderSelect(stmt)
	require.NoError(t, err)
	assert.Equal(t, "SELECT `id` AS `id` FROM `t`", sql)
}

func TestRender_MSSQLBracket(t *testing.T) {
	r := NewRenderer(DialectMSSQL)
	stmt := &SelectStmt{
		Columns: []SelectColumn{{Expr: Col("id"), Alias: "id"}},
		From:    &TableRef{Name: "t"},
	}
	sql, err := r.RenderSelect(stmt)
	require.NoError(t, err)
	assert.Equal(t, "SELECT [id] AS [id] FROM [t]", sql)
}

func TestRender_LiteralStringEscape(t *testing.T) {
	r := NewRenderer(DialectPostgres)
	stmt := &SelectStmt{
		Columns: []SelectColumn{{Expr: Col("id"), Alias: "id"}},
		From:    &TableRef{Name: "t"},
		Where:   Eq(Col("name"), Lit("O'Reilly")),
	}
	sql, err := r.RenderSelect(stmt)
	require.NoError(t, err)
	assert.Equal(t, `SELECT "id" AS "id" FROM "t" WHERE ("name" = 'O''Reilly')`, sql)
}
