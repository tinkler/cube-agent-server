package schema

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleYAML = `
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
      sql: "SELECT * FROM public.orders"
      description: 订单主表
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
          primary_key: true
        - name: status
          sql: status
          type: string
        - name: created_at
          sql: created_at
          type: time
`

func TestLoad(t *testing.T) {
	p, err := Load([]byte(sampleYAML))
	require.NoError(t, err)
	assert.Equal(t, APIVersion, p.APIVersion)
	assert.Equal(t, "orders", p.Metadata.Name)
	assert.Equal(t, "pg_main", p.Metadata.Datasource)
	require.Len(t, p.Spec.Cubes, 1)
	assert.Equal(t, "orders", p.Spec.Cubes[0].Name)
	require.Len(t, p.Spec.Cubes[0].Measures, 2)
	require.Len(t, p.Spec.Cubes[0].Dimensions, 3)
}

func TestValidate_OK(t *testing.T) {
	p, err := Load([]byte(sampleYAML))
	require.NoError(t, err)
	assert.NoError(t, p.Validate(DefaultValidateOptions()))
}

func TestValidate_Errors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		code string
	}{
		{
			name: "missing apiVersion",
			yaml: `apiVersion: ""` + "\n" + `kind: Plugin` + "\n" + `metadata: {name: a, owner: x, datasource: y}` + "\n" + `spec: {cubes: [{name: a, sql: "SELECT 1", measures: [{name: c, type: count}], dimensions: [{name: d, sql: d, type: string}]}]}`,
			code: "E001",
		},
		{
			name: "no measures",
			yaml: `apiVersion: cube-agent/v1
kind: Plugin
metadata: {name: a, owner: x, datasource: y}
spec: {cubes: [{name: a, sql: "SELECT 1", measures: [], dimensions: [{name: d, sql: d, type: string}]}]}`,
			code: "E014",
		},
		{
			name: "bad measure type",
			yaml: `apiVersion: cube-agent/v1
kind: Plugin
metadata: {name: a, owner: x, datasource: y}
spec: {cubes: [{name: a, sql: "SELECT 1", measures: [{name: x, type: foobar}], dimensions: [{name: d, sql: d, type: string}]}]}`,
			code: "E017",
		},
		{
			name: "uppercase cube name",
			yaml: `apiVersion: cube-agent/v1
kind: Plugin
metadata: {name: a, owner: x, datasource: y}
spec: {cubes: [{name: BadName, sql: "SELECT 1", measures: [{name: c, type: count}], dimensions: [{name: d, sql: d, type: string}]}]}`,
			code: "E012",
		},
		{
			name: "missing datasource",
			yaml: `apiVersion: cube-agent/v1
kind: Plugin
metadata: {name: a, owner: x}
spec: {cubes: [{name: a, sql: "SELECT 1", measures: [{name: c, type: count}], dimensions: [{name: d, sql: d, type: string}]}]}`,
			code: "E005",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := Load([]byte(c.yaml))
			require.NoError(t, err)
			err = p.Validate(DefaultValidateOptions())
			assert.Error(t, err)
			assert.Contains(t, err.Error(), c.code)
		})
	}
}

func TestRegistry_Apply(t *testing.T) {
	r := NewRegistry()
	p1, err := Load([]byte(sampleYAML))
	require.NoError(t, err)

	// 第一次 Apply
	old, err := r.Apply(ApplyPlan{Add: []*Plugin{p1}})
	require.NoError(t, err)
	assert.Equal(t, int64(0), old.Version)
	assert.Equal(t, int64(1), r.Snapshot().Version)
	require.Len(t, r.Snapshot().Plugins, 1)
	require.Len(t, r.Snapshot().Cubes, 1)

	// Upsert(改 version)
	p2, _ := Load([]byte(sampleYAML))
	p2.Metadata.Version = "0.2.0"
	p2.Metadata.Description = "改了"
	old2, err := r.Apply(ApplyPlan{Upsert: []*Plugin{p2}})
	require.NoError(t, err)
	assert.Equal(t, int64(1), old2.Version)
	assert.Equal(t, "0.2.0", r.Snapshot().Plugins["orders"].Metadata.Version)
	assert.Equal(t, "改了", r.Snapshot().Plugins["orders"].Metadata.Description)

	// Remove
	_, err = r.Apply(ApplyPlan{Remove: []string{"orders"}})
	require.NoError(t, err)
	assert.Empty(t, r.Snapshot().Plugins)
	assert.Empty(t, r.Snapshot().Cubes)
}

func TestRegistry_NoChanges(t *testing.T) {
	r := NewRegistry()
	_, err := r.Apply(ApplyPlan{})
	assert.ErrorIs(t, err, ErrNoChanges)
}

func TestRegistry_InvalidAddRollback(t *testing.T) {
	r := NewRegistry()
	bad, _ := Load([]byte(`apiVersion: cube-agent/v1
kind: Plugin
metadata: {name: bad, owner: x, datasource: y}
spec: {cubes: [{name: bad, sql: "", measures: [{name: c, type: count}], dimensions: [{name: d, sql: d, type: string}]}]}`))
	_, err := r.Apply(ApplyPlan{Add: []*Plugin{bad}})
	assert.Error(t, err)
	assert.Equal(t, int64(0), r.Snapshot().Version)
}

func TestRegistry_ConcurrentRead(t *testing.T) {
	r := NewRegistry()
	p, _ := Load([]byte(sampleYAML))
	_, _ = r.Apply(ApplyPlan{Add: []*Plugin{p}})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := r.Snapshot()
			_ = s.Cube("orders")
		}()
	}
	wg.Wait()
}
