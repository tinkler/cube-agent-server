package compiler

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
	"github.com/tinkler/cube-agent-server/internal/schema"
	"github.com/tinkler/cube-agent-server/internal/security"
)

// 准备 2 个 cube + 1 个 join
const ordersWithJoinYAML = `
apiVersion: cube-agent/v1
kind: Plugin
metadata:
  name: orders
  version: 0.1.0
  description: orders
  datasource: pg_main
  owner: test
spec:
  cubes:
    - name: orders
      sql: "SELECT * FROM orders WHERE tenant_id = '${SECURITY.tenant_id}'"
      description: orders
      primary_key: id
      measures:
        - name: count
          type: count
        - name: total_amount
          type: sum
          sql: amount
      dimensions:
        - name: id
          sql: id
          type: number
        - name: customer_id
          sql: customer_id
          type: number
        - name: status
          sql: status
          type: string
        - name: created_at
          sql: created_at
          type: time
      joins:
        - name: customers
          sql: "{CUBE}.customer_id = {customers}.id"
          relationship: many_to_one
---
apiVersion: cube-agent/v1
kind: Plugin
metadata:
  name: customers
  version: 0.1.0
  description: customers
  datasource: pg_main
  owner: test
spec:
  cubes:
    - name: customers
      sql: "SELECT * FROM customers"
      description: customers
      primary_key: id
      measures:
        - name: count
          type: count
      dimensions:
        - name: id
          sql: id
          type: number
        - name: name
          sql: name
          type: string
        - name: tier
          sql: tier
          type: string
`

func TestPass2_TwoCubes_Join(t *testing.T) {
	// 加载 2 个 plugin
	docs := splitYAMLDocs(t, ordersWithJoinYAML)
	reg := schema.NewRegistry()
	for _, d := range docs {
		p, err := schema.Load([]byte(d))
		require.NoError(t, err)
		_, err = reg.Apply(schema.ApplyPlan{Add: []*schema.Plugin{p}})
		require.NoError(t, err)
	}

	// Query 跨 2 cube
	ir, err := Pass1([]byte(`{
		"measures": ["orders.count", "orders.total_amount"],
		"dimensions": ["orders.status", "customers.tier"]
	}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"orders", "customers"}, ir.ReferencedCubes)

	resolved, err := Pass2(ir, reg)
	require.NoError(t, err)
	assert.Equal(t, 2, len(resolved.Cubes))
	assert.Equal(t, "orders", resolved.PrimaryCube)
	assert.Equal(t, 2, len(resolved.Measures))
	assert.Equal(t, 2, len(resolved.Dimensions))
}

func TestPass2_TwoCubes_NoJoin(t *testing.T) {
	// 加载 2 个 plugin,但 orders 没有定义 join 关系
	docs := splitYAMLDocs(t, ordersWithJoinYAML)
	// 把 orders 的 joins 字段去掉
	noJoinYAML := strings.Replace(ordersWithJoinYAML,
		`      joins:
        - name: customers
          sql: "{CUBE}.customer_id = {customers}.id"
          relationship: many_to_one
`, "",
		1)
	docs = splitYAMLDocs(t, noJoinYAML)
	reg := schema.NewRegistry()
	for _, d := range docs {
		p, err := schema.Load([]byte(d))
		require.NoError(t, err)
		_, err = reg.Apply(schema.ApplyPlan{Add: []*schema.Plugin{p}})
		require.NoError(t, err)
	}

	ir, _ := Pass1([]byte(`{
		"measures": ["orders.count", "customers.count"]
	}`))
	_, err := Pass2(ir, reg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no join")
}

func TestPass3_TwoCubes_Join(t *testing.T) {
	docs := splitYAMLDocs(t, ordersWithJoinYAML)
	reg := schema.NewRegistry()
	for _, d := range docs {
		p, err := schema.Load([]byte(d))
		require.NoError(t, err)
		_, err = reg.Apply(schema.ApplyPlan{Add: []*schema.Plugin{p}})
		require.NoError(t, err)
	}

	ir, _ := Pass1([]byte(`{
		"measures": ["orders.total_amount"],
		"dimensions": ["customers.tier", "orders.status"]
	}`))
	resolved, err := Pass2(ir, reg)
	require.NoError(t, err)
	res, err := Pass3(resolved, sqlbuilder.DialectPostgres, security.NewContext(map[string]string{
		"tenant_id": "1",
	}))
	require.NoError(t, err)
	// SQL 包含 LEFT JOIN
	assert.Contains(t, res.SQL, "LEFT JOIN")
	assert.Contains(t, res.SQL, `"customers"`) // join target
	assert.Contains(t, res.SQL, "orders.customer_id = customers.id") // join condition
}

// splitYAMLDocs helper:把多 YAML doc 字符串 split 成单个 doc
//   按行扫描,遇到单独一行的 "---" 就分割
func splitYAMLDocs(t *testing.T, multi string) []string {
	t.Helper()
	lines := []byte{}
	for _, ch := range multi {
		lines = append(lines, byte(ch))
	}
	// 简化:用 \n---\n 分割
	parts := []string{}
	cur := ""
	for i := 0; i < len(multi); i++ {
		cur += string(multi[i])
		if i+4 < len(multi) && multi[i] == '\n' && multi[i+1] == '-' && multi[i+2] == '-' && multi[i+3] == '-' && multi[i+4] == '\n' {
			parts = append(parts, cur[:len(cur)-1]) // 去掉末尾 \n
			cur = ""
			i += 4
		}
	}
	if cur != "" {
		parts = append(parts, cur)
	}
	docs := []string{}
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			docs = append(docs, trimmed)
		}
	}
	_ = context.Background
	return docs
}
