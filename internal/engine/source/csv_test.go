package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVSource_Basic(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "users.csv")
	csvContent := `id,name,age
1,alice,30
2,bob,25
3,charlie,35
4,dave,40
`
	require.NoError(t, os.WriteFile(csvPath, []byte(csvContent), 0o644))

	ds, err := NewCSVSource(&DataSourceConfig{
		Name:   "users",
		Driver: "csv",
		DSN:    "file:" + csvPath,
	})
	require.NoError(t, err)
	defer ds.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := ds.Query(ctx, `SELECT * FROM "users" WHERE age > '30' ORDER BY id`)
	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	// SQLite type affinity: 列定义 TEXT,所以回 string
	// TODO(W4+): 类型推断,数字列返回 number
	assert.Equal(t, "3", res.Rows[0]["id"])
	assert.Equal(t, "charlie", res.Rows[0]["name"])

	// aggregate: COUNT 总是数字
	res2, err := ds.Query(ctx, `SELECT COUNT(*) AS cnt FROM "users"`)
	require.NoError(t, err)
	require.Len(t, res2.Rows, 1)
	assert.Equal(t, int64(4), res2.Rows[0]["cnt"])
}

func TestCSVSource_BadPath(t *testing.T) {
	_, err := NewCSVSource(&DataSourceConfig{
		Name:   "x",
		Driver: "csv",
		DSN:    "file:/nonexistent/path.csv",
	})
	assert.Error(t, err)
}

func TestCSVSource_SpecialColumnNames(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "data.csv")
	// 列名带空格和特殊字符
	csvContent := `id,user name,age
1,alice,30
`
	require.NoError(t, os.WriteFile(csvPath, []byte(csvContent), 0o644))

	ds, err := NewCSVSource(&DataSourceConfig{
		Name:   "data",
		Driver: "csv",
		DSN:    "file:" + csvPath,
	})
	require.NoError(t, err)
	defer ds.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := ds.Query(ctx, `SELECT "id", "user_name", "age" FROM "data"`)
	require.NoError(t, err)
	require.Len(t, res.Rows, 1)
	assert.Equal(t, "alice", res.Rows[0]["user_name"])
}
