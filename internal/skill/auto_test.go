package skill

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/tinkler/cube-agent-server/internal/engine/source"
	"github.com/tinkler/cube-agent-server/internal/skill/datasource"
)

func TestAutoBuild_FullFlow(t *testing.T) {
	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	cacheDir := filepath.Join(tmp, ".cache")

	// 准备真 SQLite + 测试数据
	dbPath := filepath.Join(tmp, "data.db")
	cfg := &source.DataSourceConfig{Name: "demo", Driver: "sqlite", DSN: dbPath}
	ds, err := source.NewSQLiteSource(cfg)
	require.NoError(t, err)
	_, err = ds.Query(context.Background(), `CREATE TABLE products (
		id INTEGER PRIMARY KEY,
		name TEXT,
		category TEXT,
		price REAL,
		created_at TEXT
	)`)
	require.NoError(t, err)
	_, err = ds.Query(context.Background(), `INSERT INTO products VALUES
		(1, 'cola', 'drink', 5.0, '2026-08-10'),
		(2, 'chips', 'snack', 8.0, '2026-08-11')`)
	require.NoError(t, err)
	ds.Close()

	reg := source.NewRegistry()
	reg.Register("sqlite", source.NewSQLiteSource)
	ins := datasource.NewIntrospector(reg, []*source.DataSourceConfig{cfg})
	mockReloader := &mockReloader{}
	b := NewBuilder(nil, ins, reg, mockReloader, pluginsDir, cacheDir, zap.NewNop())

	s, err := b.AutoBuild("查询所有商品", "demo")
	require.NoError(t, err)
	assert.True(t, s.Done)
	assert.Equal(t, 7, s.Step)
	assert.NotEmpty(t, s.PublishedPath)

	// 验证 inferDesign 推断出来的 measure/dimension
	require.NotNil(t, s.Design)
	assert.Equal(t, "products", s.Design.CubeName)
	assert.Contains(t, s.Design.Measures, "price", "numeric column → measure")
	assert.Contains(t, s.Design.Dimensions, "name")
	assert.Contains(t, s.Design.Dimensions, "category")
	assert.Contains(t, s.Design.Dimensions, "created_at", "time column → dimension")
}

func TestInferDesign_NumericVsTime(t *testing.T) {
	b := &Builder{}
	design, err := b.inferDesign(&Session{
		Meta: &datasource.Meta{
			Tables: []datasource.TableMeta{
				{
					Name:       "test",
					PrimaryKey: "id",
					Columns: []datasource.ColumnMeta{
						{Name: "id", Type: "int"},
						{Name: "amount", Type: "numeric"},
						{Name: "name", Type: "varchar"},
						{Name: "created_at", Type: "timestamptz"},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, design.Measures, "amount")
	assert.Contains(t, design.Dimensions, "id")
	assert.Contains(t, design.Dimensions, "name")
	assert.Contains(t, design.Dimensions, "created_at")
}

func TestInferDesign_NoMeasureFallback(t *testing.T) {
	b := &Builder{}
	// 没有数字列的情况 → 加 count fallback
	design, err := b.inferDesign(&Session{
		Meta: &datasource.Meta{
			Tables: []datasource.TableMeta{
				{
					Name: "no_num",
					Columns: []datasource.ColumnMeta{
						{Name: "name", Type: "varchar"},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, design.Measures, "count", "fallback when no numeric column")
}
