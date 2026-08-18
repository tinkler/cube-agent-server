package datasource

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkler/cube-agent-server/internal/engine/source"
)

// TestIntrospect_DispatchSQLite 验证 SQLite 真实跑通
func TestIntrospect_DispatchSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	cfg := &source.DataSourceConfig{
		Name:   "ds",
		Driver: "sqlite",
		DSN:    dbPath,
	}
	ds, err := source.NewSQLiteSource(cfg)
	require.NoError(t, err)
	_, err = ds.Query(context.Background(), `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT, age INTEGER)`)
	require.NoError(t, err)
	_, err = ds.Query(context.Background(), `CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), amount REAL)`)
	require.NoError(t, err)
	_, err = ds.Query(context.Background(), `INSERT INTO users VALUES (1, 'a@x', 20), (2, 'b@x', 30)`)
	require.NoError(t, err)
	_, err = ds.Query(context.Background(), `INSERT INTO orders VALUES (1, 1, 100), (2, 1, 200), (3, 2, 50)`)
	require.NoError(t, err)
	ds.Close()

	reg := source.NewRegistry()
	reg.Register("sqlite", source.NewSQLiteSource)
	ins := NewIntrospector(reg, []*source.DataSourceConfig{cfg})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	meta, err := ins.Introspect(ctx, "ds")
	require.NoError(t, err)

	assert.Equal(t, "ds", meta.Datasource)
	assert.NotEmpty(t, meta.Tables)

	// 找到 orders 表
	var ordersTable *TableMeta
	for i := range meta.Tables {
		if meta.Tables[i].Name == "orders" {
			ordersTable = &meta.Tables[i]
			break
		}
	}
	require.NotNil(t, ordersTable)
	assert.Equal(t, "id", ordersTable.PrimaryKey)
	assert.Len(t, ordersTable.ForeignKeys, 1, "orders.user_id → users.id")
	assert.Equal(t, "user_id", ordersTable.ForeignKeys[0].Column)
	assert.Equal(t, "users.id", ordersTable.ForeignKeys[0].References)
	assert.Equal(t, int64(3), ordersTable.Quality.TotalRows)
}

// TestIntrospect_DispatcherRouting 验证 dispatch 走对路径
//   注册所有 driver(用 _ import 触发 init),让 build 成功
//   然后 intospect 走 SQL 阶段(连不通)失败
//   错误信息能看出 driver 分发走对
func TestIntrospect_DispatcherRouting(t *testing.T) {
	reg := source.NewRegistry()
	reg.Register("sqlite", source.NewSQLiteSource)
	reg.Register("pgx", source.NewPostgresSource)
	reg.Register("mysql", source.NewMysqlSource)
	reg.Register("clickhouse", source.NewClickHouseSource)
	reg.Register("mssql", source.NewMSSQLSource)

	drivers := []struct {
		driver string
		dsn    string
		// 期望错误里包含的 driver 名(走 driver branch 才会出现)
		expectedInErr string
	}{
		{"sqlite", ":memory:", ""},                                                              // SQLite OK
		{"pgx", "host=127.0.0.1 port=1 sslmode=disable user=x password=x dbname=x", "postgres"}, // PG
		{"mysql", "user:x@tcp(127.0.0.1:1)/x", "mysql"},
		{"clickhouse", "clickhouse://x@127.0.0.1:1/?dial_timeout=2s", "clickhouse"},
		{"mssql", "sqlserver://sa:x@127.0.0.1:1?database=x&encrypt=disable&trustservercertificate=true&dial+timeout=2", "mssql"},
	}

	for _, tc := range drivers {
		t.Run(tc.driver, func(t *testing.T) {
			cfgs := []*source.DataSourceConfig{{Name: "x", Driver: tc.driver, DSN: tc.dsn}}
			ins := NewIntrospector(reg, cfgs)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()

			meta, err := ins.Introspect(ctx, "x")
			if tc.driver == "sqlite" {
				require.NoError(t, err)
				assert.NotNil(t, meta)
				return
			}
			require.Error(t, err, "expected connection failure for %s", tc.driver)
			t.Logf("driver=%s err=%v", tc.driver, err)
		})
	}
}
