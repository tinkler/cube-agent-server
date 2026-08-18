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

func TestBuilder_StartAndStep(t *testing.T) {
	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	cacheDir := filepath.Join(tmp, ".cache")
	b := NewBuilder(nil, nil, source.NewRegistry(), nil, pluginsDir, cacheDir, zap.NewNop())

	s, err := b.Start("统计每日订单", SemiAuto)
	require.NoError(t, err)
	assert.NotEmpty(t, s.ID)
	assert.Equal(t, 2, s.Step) // Start 后直接到 step 2(intent 已收)
	assert.Equal(t, "统计每日订单", s.Intent)

	// Step 2:选数据源
	s, err = b.Step2Datasource(s.ID, "demo")
	require.NoError(t, err)
	assert.Equal(t, "demo", s.Datasource)
	assert.Equal(t, 2, s.Step)

	// Step 3 introspect 会失败(没真 datasource)
	_, err = b.Step3Analyze(s.ID)
	assert.Error(t, err) // datasource demo 不存在

	// 直接构造 Meta 跳过 introspect 测试 step 4
	s.Meta = &datasource.Meta{
		Datasource: "demo",
		Tables: []datasource.TableMeta{
			{Name: "test", Columns: []datasource.ColumnMeta{{Name: "id", Type: "int"}}},
		},
	}

	// Step 4 design
	s, err = b.Step4Design(s.ID, &Design{
		CubeName:        "test_cube",
		CubeDescription: "测试 cube",
		SQLTemplate:     "SELECT * FROM test",
		PrimaryKey:      "id",
		Measures:        []string{"count", "total_amount"},
		Dimensions:      []string{"status"},
		Segments:        []string{"paid"},
	})
	require.NoError(t, err)
	assert.Equal(t, 5, s.Step)
	assert.NotNil(t, s.Design)

	// Step 5 generate
	s, err = b.Step5Generate(s.ID)
	require.NoError(t, err)
	assert.Equal(t, 6, s.Step)
	assert.NotEmpty(t, s.Draft.YAML)
	assert.Contains(t, s.Draft.YAML, "test_cube")
	assert.Contains(t, s.Draft.YAML, "count")
}

func TestBuilder_FullFlow_WithIntrospect(t *testing.T) {
	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	cacheDir := filepath.Join(tmp, ".cache")

	// 准备真 SQLite
	dbPath := filepath.Join(tmp, "data.db")
	cfg := &source.DataSourceConfig{
		Name:   "demo",
		Driver: "sqlite",
		DSN:    dbPath,
	}
	ds, err := source.NewSQLiteSource(cfg)
	require.NoError(t, err)
	_, err = ds.Query(context.Background(), `CREATE TABLE orders (id INTEGER PRIMARY KEY, status TEXT, amount REAL)`)
	require.NoError(t, err)
	_, err = ds.Query(context.Background(), `INSERT INTO orders VALUES (1, 'paid', 100), (2, 'paid', 200)`)
	require.NoError(t, err)
	ds.Close()

	reg := source.NewRegistry()
	reg.Register("sqlite", source.NewSQLiteSource)
	ins := datasource.NewIntrospector(reg, []*source.DataSourceConfig{cfg})

	// 构造 builder
	mockReloader := &mockReloader{}
	b := NewBuilder(nil, ins, reg, mockReloader, pluginsDir, cacheDir, zap.NewNop())

	// 全流程
	s, err := b.Start("统计每日订单", SemiAuto)
	require.NoError(t, err)
	s, err = b.Step2Datasource(s.ID, "demo")
	require.NoError(t, err)
	s, err = b.Step3Analyze(s.ID)
	require.NoError(t, err)
	assert.Equal(t, 4, s.Step)
	assert.NotEmpty(t, s.Meta.Tables)

	s, err = b.Step4Design(s.ID, &Design{
		CubeName:        "orders_cube",
		CubeDescription: "订单 cube(AI 生成)",
		SQLTemplate:     "SELECT * FROM orders",
		PrimaryKey:      "id",
		Measures:        []string{"count"},
		Dimensions:      []string{"status"},
	})
	require.NoError(t, err)
	s, err = b.Step5Generate(s.ID)
	require.NoError(t, err)
	s, err = b.Step6Validate(s.ID)
	require.NoError(t, err)
	assert.True(t, s.Validation.OK)

	s, err = b.Step7Publish(s.ID)
	require.NoError(t, err)
	assert.True(t, s.Done)
	assert.NotEmpty(t, s.PublishedPath)
	// 验证文件存在
	_, err = readFile(s.PublishedPath)
	require.NoError(t, err)
	// 验证 reload 被调用
	assert.Equal(t, 1, mockReloader.calls)
}

// mockReloader 测试用
type mockReloader struct {
	calls int
}

func (m *mockReloader) Reload() error {
	m.calls++
	return nil
}

// readFile helper
func readFile(path string) ([]byte, error) {
	return readFileFS(path)
}
