package compiler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
	"github.com/tinkler/cube-agent-server/internal/schema"
	"github.com/tinkler/cube-agent-server/internal/security"
)

var _ = schema.NewRegistry

const ordersPluginYAML = `
apiVersion: cube-agent/v1
kind: Plugin
metadata:
  name: orders
  version: 0.1.0
  description: 订单
  datasource: pg_main
  owner: data-team
spec:
  cubes:
    - name: orders
      sql: "SELECT * FROM public.orders WHERE tenant_id = '${SECURITY.tenant_id}'"
      description: 订单主表
      primary_key: id
      measures:
        - name: count
          type: count
          description: 订单数
        - name: total_amount
          type: sum
          sql: amount
          description: 订单总金额
      dimensions:
        - name: id
          sql: id
          type: number
          primary_key: true
        - name: status
          sql: status
          type: string
        - name: customer_id
          sql: customer_id
          type: number
        - name: created_at
          sql: created_at
          type: time
      segments:
        - name: paid
          sql: "{CUBE}.status IN ('paid', 'shipped', 'done')"
        - name: unpaid
          sql: "{CUBE}.status IN ('created', 'pending')"
`

func setupRegistry(t *testing.T) *schema.Registry {
	t.Helper()
	p, err := schema.Load([]byte(ordersPluginYAML))
	require.NoError(t, err)
	reg := schema.NewRegistry()
	_, err = reg.Apply(schema.ApplyPlan{Add: []*schema.Plugin{p}})
	require.NoError(t, err)
	return reg
}

func TestPass1_Basic(t *testing.T) {
	ir, err := Pass1([]byte(`{
		"measures": ["orders.count"],
		"dimensions": ["orders.status"]
	}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"orders"}, ir.ReferencedCubes)
}

func TestPass2_MeasureAndDimension(t *testing.T) {
	reg := setupRegistry(t)
	ir, err := Pass1([]byte(`{
		"measures": ["orders.count", "orders.total_amount"],
		"dimensions": ["orders.status"]
	}`))
	require.NoError(t, err)
	r, err := Pass2(ir, reg)
	require.NoError(t, err)
	assert.Equal(t, "orders", r.PrimaryCube)
	require.Len(t, r.Measures, 2)
	require.Len(t, r.Dimensions, 1)
	assert.Equal(t, "count", r.Measures[0].Field)
	assert.Equal(t, schema.MeasureTypeCount, r.Measures[0].Def.Type)
}

func TestPass2_UnknownMeasure(t *testing.T) {
	reg := setupRegistry(t)
	ir, _ := Pass1([]byte(`{"measures": ["orders.unknown"]}`))
	_, err := Pass2(ir, reg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "measure")
}

func TestPass2_CrossCube(t *testing.T) {
	// 构造 2 个独立 plugin,无 join 关系
	orders := []byte(`
apiVersion: cube-agent/v1
kind: Plugin
metadata: { name: orders, owner: x, datasource: y }
spec:
  cubes:
    - name: orders
      sql: "SELECT 1"
      measures: [{name: count, type: count}]
      dimensions: [{name: id, sql: id, type: number}]
`)
	products := []byte(`
apiVersion: cube-agent/v1
kind: Plugin
metadata: { name: products, owner: x, datasource: y }
spec:
  cubes:
    - name: products
      sql: "SELECT 1"
      measures: [{name: count, type: count}]
      dimensions: [{name: id, sql: id, type: number}]
`)
	reg := schema.NewRegistry()
	for _, d := range [][]byte{orders, products} {
		p, err := schema.Load(d)
		require.NoError(t, err)
		_, err = reg.Apply(schema.ApplyPlan{Add: []*schema.Plugin{p}})
		require.NoError(t, err)
	}
	ir, _ := Pass1([]byte(`{
		"measures": ["orders.count", "products.count"]
	}`))
	_, err := Pass2(ir, reg)
	assert.Error(t, err)
	// W5: 跨 cube 但没 join 关系,报 no join
	assert.Contains(t, err.Error(), "no join")
}

func TestPass3_SimpleCount(t *testing.T) {
	reg := setupRegistry(t)
	ir, err := Pass1([]byte(`{
		"measures": ["orders.count"]
	}`))
	require.NoError(t, err)
	r, err := Pass2(ir, reg)
	require.NoError(t, err)
	res, err := Pass3(r, sqlbuilder.DialectPostgres, security.NewContext(map[string]string{
		"tenant_id": "42",
	}))
	require.NoError(t, err)
	// SQL 包含 SELECT COUNT(*) 和 FROM 后的子查询
	assert.Contains(t, res.SQL, "SELECT COUNT(*)")
	assert.Contains(t, res.SQL, "FROM (")
	assert.Contains(t, res.SQL, "'42'") // security 注入
	assert.Contains(t, res.SQL, `"orders"`) // cube alias
}

func TestPass3_WithFilterAndGroupBy(t *testing.T) {
	reg := setupRegistry(t)
	ir, _ := Pass1([]byte(`{
		"measures": ["orders.total_amount"],
		"dimensions": ["orders.status"],
		"filters": [{"member": "orders.status", "operator": "equals", "values": ["paid"]}],
		"order": [["orders.total_amount", "desc"]],
		"limit": 10
	}`))
	r, _ := Pass2(ir, reg)
	res, err := Pass3(r, sqlbuilder.DialectPostgres, security.NewContext(map[string]string{
		"tenant_id": "1",
	}))
	require.NoError(t, err)
	assert.Contains(t, res.SQL, `SUM("amount")`)
	assert.Contains(t, res.SQL, `GROUP BY "status"`)
	assert.Contains(t, res.SQL, `"status" = 'paid'`)
	// ORDER BY measure 时使用其聚合表达式(SUM("amount")),这是 cube.js 标准行为
	assert.Contains(t, res.SQL, `ORDER BY SUM("amount") DESC`)
	assert.Contains(t, res.SQL, `LIMIT 10`)
}

func TestPass3_WithSegment(t *testing.T) {
	reg := setupRegistry(t)
	ir, _ := Pass1([]byte(`{
		"measures": ["orders.count"],
		"segments": ["orders.paid"]
	}`))
	r, _ := Pass2(ir, reg)
	res, err := Pass3(r, sqlbuilder.DialectPostgres, security.NewContext(map[string]string{
		"tenant_id": "1",
	}))
	require.NoError(t, err)
	// segment 渲染为裸 SQL 片段,别名不带引号
	assert.Contains(t, res.SQL, `orders.status IN ('paid', 'shipped', 'done')`)
}

func TestPass3_WithTimeDimension(t *testing.T) {
	reg := setupRegistry(t)
	ir, _ := Pass1([]byte(`{
		"measures": ["orders.count"],
		"timeDimensions": [{
			"dimension": "orders.created_at",
			"dateRange": ["2026-08-01", "2026-08-15"],
			"granularity": "day"
		}]
	}`))
	r, _ := Pass2(ir, reg)
	res, err := Pass3(r, sqlbuilder.DialectPostgres, security.NewContext(map[string]string{
		"tenant_id": "1",
	}))
	require.NoError(t, err)
	assert.Contains(t, res.SQL, "DATE_TRUNC('day'")
	assert.Contains(t, res.SQL, "'2026-08-01'")
	assert.Contains(t, res.SQL, "'2026-08-15'")
	assert.Contains(t, res.SQL, "GROUP BY DATE_TRUNC('day'")
}

func TestPass3_DialectMySQL(t *testing.T) {
	reg := setupRegistry(t)
	ir, _ := Pass1([]byte(`{"measures": ["orders.count"], "dimensions": ["orders.status"]}`))
	r, _ := Pass2(ir, reg)
	res, err := Pass3(r, sqlbuilder.DialectMySQL, security.NewContext(map[string]string{
		"tenant_id": "1",
	}))
	require.NoError(t, err)
	assert.True(t, strings.Contains(res.SQL, "`orders`"))
	assert.True(t, strings.Contains(res.SQL, "`status`"))
}

func TestPass3_DialectMSSQL(t *testing.T) {
	reg := setupRegistry(t)
	ir, _ := Pass1([]byte(`{"measures": ["orders.count"], "dimensions": ["orders.status"]}`))
	r, _ := Pass2(ir, reg)
	res, err := Pass3(r, sqlbuilder.DialectMSSQL, security.NewContext(map[string]string{
		"tenant_id": "1",
	}))
	require.NoError(t, err)
	assert.True(t, strings.Contains(res.SQL, "[orders]"))
	assert.True(t, strings.Contains(res.SQL, "[status]"))
}

// TestPass3_MSSQL_TimeDimension_DatePartUnquoted 守 SQL Server 2008 R2 的
// DATEADD 第一参数必须是 datepart 标识符 (dd/mm/yy) 而不是字符串 'dd'/'mm'/'yy'。
// 之前用 sqlbuilder.Lit 会渲染成 'dd',SQL Server 报类型错。
func TestPass3_MSSQL_TimeDimension_DatePartUnquoted(t *testing.T) {
	reg := setupRegistry(t)
	ir, _ := Pass1([]byte(`{
		"measures": ["orders.count"],
		"timeDimensions": [{
			"dimension": "orders.created_at",
			"dateRange": ["2026-08-01", "2026-08-15"],
			"granularity": "month"
		}]
	}`))
	r, _ := Pass2(ir, reg)
	res, err := Pass3(r, sqlbuilder.DialectMSSQL, security.NewContext(map[string]string{
		"tenant_id": "1",
	}))
	require.NoError(t, err)
	// 关键断言:DATEADD 的第一参数是 mm(裸标识符),不是 'mm'
	assert.Contains(t, res.SQL, "DATEADD(mm, DATEDIFF(mm, 0,")
	assert.NotContains(t, res.SQL, "DATEADD('mm'")
	assert.NotContains(t, res.SQL, `'mm'`)
}
