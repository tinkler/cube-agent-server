package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/tinkler/cube-agent-server/internal/schema"
)

// writePlugin 写一个最小可用的 plugin.yaml 到 subdir/plugin.yaml
func writePlugin(t *testing.T, dir, sub, body string) string {
	t.Helper()
	subDir := filepath.Join(dir, sub)
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	path := filepath.Join(subDir, "plugin.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

// validPluginYAML 返回一个以 name 命名的最小可加载 plugin
func validPluginYAML(name string) string {
	return fmt.Sprintf(`apiVersion: cube-agent/v1
kind: Plugin
metadata:
  name: %s
  version: 0.1.0
  description: test
  datasource: ds1
  owner: tester
spec:
  cubes:
    - name: %s
      sql: "SELECT 1 AS x"
      primary_key: x
      measures:
        - {name: cnt, type: count}
      dimensions:
        - {name: x, sql: x, type: number, primary_key: true}
`, name, name)
}

// TestScanAll_KeepOldOnBrokenFile 回归测试:文件存在但解析失败时,旧 plugin 不应被 Remove
// 这是 hot-reload 的核心 bug:之前会把"还在编辑中"的 plugin 误删
func TestScanAll_KeepOldOnBrokenFile(t *testing.T) {
	dir := t.TempDir()

	// 写一个有效 plugin
	writePlugin(t, dir, "p1", validPluginYAML("p1"))

	registry := schema.NewRegistry()
	mgr := NewManager(dir, registry, zap.NewNop(), 100, false, []string{"ds1"})
	require.NoError(t, mgr.scanAll())
	require.Equal(t, 1, mgr.LoadedCount(), "初始 scan 应该成功加载 1 个 plugin")
	require.NotNil(t, registry.Snapshot().Cube("p1"), "cube p1 应该在 registry 里")

	// 模拟用户编辑中:YAML 损坏
	writePlugin(t, dir, "p1", "this is not: valid: yaml: :::")
	require.NoError(t, mgr.scanAll())

	// 关键断言:旧 plugin 应该被保留,不被 Remove
	assert.Equal(t, 1, mgr.LoadedCount(), "broken file 不应让 plugin 被从 m.loaded 移除")
	assert.NotNil(t, registry.Snapshot().Cube("p1"), "broken file 不应让 cube 从 registry 消失")
	assert.NotNil(t, registry.Snapshot().Plugins["p1"], "broken file 不应让 plugin 从 registry 消失")

	// 再写一个有效版本,扫描后应该恢复
	writePlugin(t, dir, "p1", validPluginYAML("p1"))
	require.NoError(t, mgr.scanAll())
	assert.Equal(t, 1, mgr.LoadedCount(), "恢复后应该能成功加载")
	assert.NotNil(t, registry.Snapshot().Cube("p1"), "恢复后 cube 应在")
}

// TestScanAll_RemoveOnFileDeleted 文件被删除时,plugin 应该被 Remove
func TestScanAll_RemoveOnFileDeleted(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "p1", validPluginYAML("p1"))
	writePlugin(t, dir, "p2", validPluginYAML("p2"))

	registry := schema.NewRegistry()
	mgr := NewManager(dir, registry, zap.NewNop(), 100, false, []string{"ds1"})
	require.NoError(t, mgr.scanAll())
	require.Equal(t, 2, mgr.LoadedCount())

	// 删 p1 的目录(模拟用户删 plugin)
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "p1")))

	require.NoError(t, mgr.scanAll())
	assert.Equal(t, 1, mgr.LoadedCount(), "删除 p1 后应剩 1 个")
	assert.Nil(t, registry.Snapshot().Cube("p1"), "p1 的 cube 应该被 Remove")
	assert.NotNil(t, registry.Snapshot().Cube("p2"), "p2 应该还在")
}

// TestScanAll_AddNewPlugin 新增 plugin 应被加载
func TestScanAll_AddNewPlugin(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "p1", validPluginYAML("p1"))

	registry := schema.NewRegistry()
	mgr := NewManager(dir, registry, zap.NewNop(), 100, false, []string{"ds1"})
	require.NoError(t, mgr.scanAll())
	require.Equal(t, 1, mgr.LoadedCount())

	// 新增 p2
	writePlugin(t, dir, "p2", validPluginYAML("p2"))
	require.NoError(t, mgr.scanAll())
	assert.Equal(t, 2, mgr.LoadedCount(), "新增 p2 后应加载 2 个")
	assert.NotNil(t, registry.Snapshot().Cube("p1"))
	assert.NotNil(t, registry.Snapshot().Cube("p2"))
}

// TestScanAll_UpdateExisting 修改已有 plugin 的 SQL 后,应 Upsert
func TestScanAll_UpdateExisting(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "p1", validPluginYAML("p1"))

	registry := schema.NewRegistry()
	mgr := NewManager(dir, registry, zap.NewNop(), 100, false, []string{"ds1"})
	require.NoError(t, mgr.scanAll())
	oldCube := registry.Snapshot().Cube("p1")
	oldVersion := registry.Snapshot().Version
	require.NotNil(t, oldCube)

	// 改 SQL
	updatedYAML := `apiVersion: cube-agent/v1
kind: Plugin
metadata:
  name: p1
  version: 0.2.0
  description: updated
  datasource: ds1
  owner: tester
spec:
  cubes:
    - name: p1
      sql: "SELECT 999 AS x"
      primary_key: x
      measures:
        - {name: cnt, type: count}
      dimensions:
        - {name: x, sql: x, type: number, primary_key: true}
`
	writePlugin(t, dir, "p1", updatedYAML)
	require.NoError(t, mgr.scanAll())
	newCube := registry.Snapshot().Cube("p1")
	assert.Equal(t, "SELECT 999 AS x", newCube.SQL, "SQL 应该被更新")
	assert.Greater(t, registry.Snapshot().Version, oldVersion, "version 应该上涨")
}

// TestRun_HotReload 端到端:启动 Run 后,改文件,等待 reload
func TestRun_HotReload(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "p1", validPluginYAML("p1"))

	registry := schema.NewRegistry()
	mgr := NewManager(dir, registry, zap.NewNop(), 50, true, []string{"ds1"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = mgr.Run(ctx)
	}()

	// 等 Run 启动 watcher
	require.Eventually(t, func() bool {
		return registry.Snapshot().Cube("p1") != nil
	}, 1*time.Second, 10*time.Millisecond, "初始 scan 应加载 p1")

	// 改 SQL 触发 reload
	updatedYAML := `apiVersion: cube-agent/v1
kind: Plugin
metadata:
  name: p1
  version: 0.3.0
  description: hot reload test
  datasource: ds1
  owner: tester
spec:
  cubes:
    - name: p1
      sql: "SELECT 42 AS x"
      primary_key: x
      measures:
        - {name: cnt, type: count}
      dimensions:
        - {name: x, sql: x, type: number, primary_key: true}
`
	writePlugin(t, dir, "p1", updatedYAML)

	// 等 hot reload 生效
	require.Eventually(t, func() bool {
		c := registry.Snapshot().Cube("p1")
		return c != nil && c.SQL == "SELECT 42 AS x"
	}, 3*time.Second, 50*time.Millisecond, "改 SQL 后应自动热重载")

	cancel()
	wg.Wait()
}
