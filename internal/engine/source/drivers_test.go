package source

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_Build_UnknownDriver(t *testing.T) {
	r := NewRegistry()
	r.Register("sqlite", NewSQLiteSource)
	_, err := r.Build(&DataSourceConfig{
		Name:   "x",
		Driver: "mongodb",
		DSN:    "x",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown driver")
}

func TestPostgres_BadDSN(t *testing.T) {
	// 故意连不上的端口,应该 5s 内 ping 失败
	_, err := NewPostgresSource(&DataSourceConfig{
		Name:   "pg_bad",
		Driver: "pgx",
		DSN:    "host=127.0.0.1 port=1 user=x password=x dbname=x sslmode=disable",
		Pool:   PoolConfig{MaxOpen: 1},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "postgres ping")
}

func TestMysql_BadDSN(t *testing.T) {
	_, err := NewMysqlSource(&DataSourceConfig{
		Name:   "mysql_bad",
		Driver: "mysql",
		DSN:    "user:x@tcp(127.0.0.1:1)/x",
		Pool:   PoolConfig{MaxOpen: 1},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mysql ping")
}

func TestSQLite_InMemory(t *testing.T) {
	// in-memory 跑一个 query
	ds, err := NewSQLiteSource(&DataSourceConfig{
		Name:   "test",
		Driver: "sqlite",
		DSN:    "file::memory:?cache=shared",
	})
	require.NoError(t, err)
	defer ds.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = ds.Query(ctx, "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)")
	require.NoError(t, err)
	_, err = ds.Query(ctx, "INSERT INTO t VALUES (1, 'alice'), (2, 'bob')")
	require.NoError(t, err)

	res, err := ds.Query(ctx, "SELECT id, name FROM t ORDER BY id")
	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	assert.Equal(t, "alice", res.Rows[0]["name"])
	assert.Equal(t, "bob", res.Rows[1]["name"])
}

func TestRegistry_MultipleDrivers(t *testing.T) {
	r := NewRegistry()
	r.Register("sqlite", NewSQLiteSource)
	r.Register("pgx", NewPostgresSource)
	r.Register("mysql", NewMysqlSource)
	r.Register("clickhouse", NewClickHouseSource)
	r.Register("mssql", NewMSSQLSource)

	drivers := r.drivers()
	assert.Contains(t, drivers, "sqlite")
	assert.Contains(t, drivers, "pgx")
	assert.Contains(t, drivers, "mysql")
	assert.Contains(t, drivers, "clickhouse")
	assert.Contains(t, drivers, "mssql")
	assert.Len(t, drivers, 5)
}

func TestClickHouse_BadDSN(t *testing.T) {
	_, err := NewClickHouseSource(&DataSourceConfig{
		Name:   "ch_bad",
		Driver: "clickhouse",
		DSN:    "clickhouse://default:@127.0.0.1:1/default?dial_timeout=2s",
		Pool:   PoolConfig{MaxOpen: 1},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "clickhouse")
}

func TestMSSQL_BadDSN(t *testing.T) {
	_, err := NewMSSQLSource(&DataSourceConfig{
		Name:   "mssql_bad",
		Driver: "mssql",
		DSN:    "sqlserver://sa:bad@127.0.0.1:1?database=x&encrypt=disable&trustservercertificate=true&dial+timeout=2",
		Pool:   PoolConfig{MaxOpen: 1},
	})
	assert.Error(t, err)
	// 错误消息提示要带兼容参数
	assert.Contains(t, err.Error(), "encrypt=disable")
}
