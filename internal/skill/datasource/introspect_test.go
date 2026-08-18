package datasource

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkler/cube-agent-server/internal/engine/source"
)

func TestIntrospect_SQLite(t *testing.T) {
	// 1. 准备一个临时 SQLite
	dbPath := filepath.Join(t.TempDir(), "test.db")
	cfg := &source.DataSourceConfig{
		Name:   "test_ds",
		Driver: "sqlite",
		DSN:    dbPath + "?_pragma=foreign_keys(1)",
	}

	// 建表
	ds, err := source.NewSQLiteSource(cfg)
	require.NoError(t, err)
	defer ds.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = ds.Query(ctx, `CREATE TABLE orders (
		id INTEGER PRIMARY KEY,
		tenant_id TEXT,
		customer_id INTEGER REFERENCES customers(id),
		amount REAL,
		status TEXT,
		created_at TEXT
	)`)
	require.NoError(t, err)
	_, err = ds.Query(ctx, `CREATE TABLE customers (
		id INTEGER PRIMARY KEY,
		name TEXT,
		email TEXT
	)`)
	require.NoError(t, err)
	_, err = ds.Query(ctx, `CREATE TABLE order_items (
		id INTEGER PRIMARY KEY,
		order_id INTEGER REFERENCES orders(id),
		product_id INTEGER REFERENCES products(id),
		quantity INTEGER
	)`)
	require.NoError(t, err)
	_, err = ds.Query(ctx, `CREATE TABLE products (
		id INTEGER PRIMARY KEY,
		name TEXT
	)`)
	require.NoError(t, err)
	_, err = ds.Query(ctx, `INSERT INTO customers VALUES (1, 'alice', 'a@x.com'), (2, 'bob', 'b@x.com')`)
	require.NoError(t, err)
	_, err = ds.Query(ctx, `INSERT INTO products VALUES (1, 'cola'), (2, 'chips')`)
	require.NoError(t, err)
	_, err = ds.Query(ctx, `INSERT INTO orders VALUES
		(1, 't1', 1, 100, 'paid', '2026-08-10'),
		(2, 't1', 2, 200, 'paid', '2026-08-11'),
		(3, 't1', 1, 50, 'pending', '2026-08-12')`)
	require.NoError(t, err)
	_, err = ds.Query(ctx, `INSERT INTO order_items VALUES
		(1, 1, 1, 5), (2, 1, 2, 3), (3, 2, 1, 2), (4, 3, 2, 4)`)
	require.NoError(t, err)
	ds.Close()

	// 2. introspect
	reg := source.NewRegistry()
	reg.Register("sqlite", source.NewSQLiteSource)
	ins := NewIntrospector(reg, []*source.DataSourceConfig{cfg})

	meta, err := ins.Introspect(ctx, "test_ds")
	require.NoError(t, err)
	assert.Equal(t, "test_ds", meta.Datasource)
	assert.Equal(t, 4, len(meta.Tables), "should have 4 tables")

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
	require.NotNil(t, ordersTable.Quality)
	assert.Equal(t, int64(3), ordersTable.Quality.TotalRows)
	// 应该有 1 个外键 (customer_id -> customers.id)
	assert.Len(t, ordersTable.ForeignKeys, 1)

	// 关系
	assert.NotEmpty(t, meta.Relations)
}

func TestIntrospect_UnknownDatasource(t *testing.T) {
	reg := source.NewRegistry()
	ins := NewIntrospector(reg, nil)
	_, err := ins.Introspect(context.Background(), "missing")
	assert.Error(t, err)
}

func TestIntrospect_BadDriver(t *testing.T) {
	reg := source.NewRegistry()
	ins := NewIntrospector(reg, []*source.DataSourceConfig{
		{Name: "x", Driver: "oracle", DSN: "x"},
	})
	_, err := reg.Build(&source.DataSourceConfig{Name: "x", Driver: "oracle", DSN: "x"})
	require.Error(t, err)
	_, err = ins.Introspect(context.Background(), "x")
	require.Error(t, err)
}

// 静默 lint
var _ = sql.ErrNoRows
