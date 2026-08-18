package engine

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkler/cube-agent-server/internal/compiler"
	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
	"github.com/tinkler/cube-agent-server/internal/engine/source"
	"github.com/tinkler/cube-agent-server/internal/schema"
	"github.com/tinkler/cube-agent-server/internal/security"
)

// TestIntegration_EndToEnd 端到端集成测试
// 流程: schema → pass1 → pass2 → pass3 → SQLite 执行 → 验证结果
func TestIntegration_EndToEnd(t *testing.T) {
	// 1. 准备 SQLite 数据源
	dbPath := filepath.Join(t.TempDir(), "test.db")
	dsCfg := &source.DataSourceConfig{
		Name:   "test",
		Driver: "sqlite",
		DSN:    dbPath + "?_pragma=foreign_keys(1)",
	}
	srcReg := source.NewRegistry()
	srcReg.Register("sqlite", source.NewSQLiteSource)

	// 2. 准备 schema
	pluginYAML := []byte(`
apiVersion: cube-agent/v1
kind: Plugin
metadata:
  name: orders
  version: 0.1.0
  description: 订单
  datasource: test
  owner: test
spec:
  cubes:
    - name: orders
      sql: "SELECT * FROM orders WHERE tenant_id = '${SECURITY.tenant_id}'"
      description: 订单主表
      primary_key: id
      measures:
        - name: count
          type: count
        - name: total_amount
          type: sum
          sql: amount
      dimensions:
        - name: status
          sql: status
          type: string
`)
	p, err := schema.Load(pluginYAML)
	require.NoError(t, err)
	reg := schema.NewRegistry()
	_, err = reg.Apply(schema.ApplyPlan{Add: []*schema.Plugin{p}})
	require.NoError(t, err)

	// 3. 建表 + 插数据
	ds, err := srcReg.Build(dsCfg)
	require.NoError(t, err)
	defer ds.Close()

	ctx := context.Background()
	_, err = ds.Query(ctx, `CREATE TABLE orders (
		id INTEGER PRIMARY KEY,
		tenant_id TEXT,
		amount REAL,
		status TEXT
	)`)
	require.NoError(t, err)
	_, err = ds.Query(ctx, `INSERT INTO orders VALUES
		(1, 't1', 100, 'paid'),
		(2, 't1', 200, 'paid'),
		(3, 't1', 50, 'pending'),
		(4, 't2', 999, 'paid')`)
	require.NoError(t, err)
	ds.Close()

	// 4. 端到端测试
	cases := []struct {
		name         string
		json         string
		tenant       string
		expectedRows int
	}{
		{
			name:         "tenant t1 count",
			json:         `{"measures":["orders.count"]}`,
			tenant:       "t1",
			expectedRows: 1, // 一行:count=3
		},
		{
			name:         "tenant t2 count",
			json:         `{"measures":["orders.count"]}`,
			tenant:       "t2",
			expectedRows: 1, // 一行:count=1
		},
		{
			name:         "tenant t1 sum by status",
			json:         `{"measures":["orders.total_amount"],"dimensions":["orders.status"]}`,
			tenant:       "t1",
			expectedRows: 2, // paid=300, pending=50
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ir, err := compiler.Pass1([]byte(c.json))
			require.NoError(t, err)
			resolved, err := compiler.Pass2(ir, reg)
			require.NoError(t, err)
			sec := security.NewContext(map[string]string{"tenant_id": c.tenant})
			res, err := compiler.Pass3(resolved, sqlbuilder.DialectSQLite, sec)
			require.NoError(t, err)

			// 执行 SQL
			ds, err := srcReg.Build(dsCfg)
			require.NoError(t, err)
			defer ds.Close()
			rs, err := ds.Query(ctx, res.SQL, res.Args...)
			require.NoError(t, err)
			t.Logf("SQL: %s", res.SQL)
			t.Logf("Rows: %d", len(rs.Rows))
			assert.Equal(t, c.expectedRows, len(rs.Rows))
		})
	}
}
