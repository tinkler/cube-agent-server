// Package plugin 负责 plugins/ 目录的扫描、加载、热更新
package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	"github.com/tinkler/cube-agent-server/internal/schema"
)

// Manager plugin 生命周期管理
//   - 启动时扫描目录加载所有 plugin.yaml
//   - 监听目录的 fsnotify 事件
//   - 事件触发时 debounce 合并,然后 reload
//   - 失败时记录错误,旧 plugin 保持不变(rollback 自然由 Registry 保证)
type Manager struct {
	dir          string
	registry     *schema.Registry
	logger       *zap.Logger
	debounceMs   int
	autoReload   bool
	knownDS      []string

	mu       sync.Mutex
	loaded   map[string]time.Time // plugin_name → 最后成功加载时间
}

// NewManager 构造 manager(还没启动)
func NewManager(
	dir string,
	registry *schema.Registry,
	logger *zap.Logger,
	debounceMs int,
	autoReload bool,
	knownDatasources []string,
) *Manager {
	if debounceMs <= 0 {
		debounceMs = 500
	}
	return &Manager{
		dir:        dir,
		registry:   registry,
		logger:     logger,
		debounceMs: debounceMs,
		autoReload: autoReload,
		knownDS:    knownDatasources,
		loaded:     map[string]time.Time{},
	}
}

// LoadedCount 已成功加载的 plugin 数量
func (m *Manager) LoadedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.loaded)
}

// Run 启动 manager(阻塞到 ctx done)
// 流程:
//   1. 扫描目录初始加载
//   2. 启动 fsnotify
//   3. 事件循环 + debounce
func (m *Manager) Run(ctx context.Context) error {
	// 1. 初始扫描
	if err := m.scanAll(); err != nil {
		m.logger.Error("initial plugin scan failed", zap.Error(err))
	}

	if !m.autoReload {
		// 不监听,只等 ctx done
		<-ctx.Done()
		return nil
	}

	// 2. 启动 watcher
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}
	defer w.Close()

	if err := m.addRecursiveWatch(w, m.dir); err != nil {
		return fmt.Errorf("watch plugin dir %q: %w", m.dir, err)
	}
	m.logger.Info("plugin watcher started", zap.String("dir", m.dir))

	// 3. 事件循环
	var (
		pendingTimer *time.Timer
		pendingMu    sync.Mutex
	)

	scheduleReload := func() {
		pendingMu.Lock()
		defer pendingMu.Unlock()
		if pendingTimer != nil {
			pendingTimer.Stop()
		}
		pendingTimer = time.AfterFunc(time.Duration(m.debounceMs)*time.Millisecond, func() {
			if err := m.scanAll(); err != nil {
				m.logger.Error("reload scan failed", zap.Error(err))
			}
		})
	}

	for {
		select {
		case <-ctx.Done():
			pendingMu.Lock()
			if pendingTimer != nil {
				pendingTimer.Stop()
			}
			pendingMu.Unlock()
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			m.logger.Debug("fsnotify event",
				zap.String("file", ev.Name),
				zap.String("op", ev.Op.String()),
			)
			// 任何 plugin.yaml 的写/创建/删除/重命名都触发 reload
			if filepath.Base(ev.Name) == "plugin.yaml" {
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) != 0 {
					scheduleReload()
				}
			}
			// 子目录创建 → 立即 watch 新目录(以防用户新增 plugin)
			if ev.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(ev.Name); statErr == nil && info.IsDir() {
					if addErr := w.Add(ev.Name); addErr == nil {
						m.logger.Debug("watching new subdir", zap.String("dir", ev.Name))
					}
				}
			}
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			m.logger.Error("fsnotify error", zap.Error(err))
		}
	}
}

// scanAll 全量扫描目录
// 不存在的 plugin → Remove
// 已存在的 plugin → Upsert
func (m *Manager) scanAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	files, err := m.discover()
	if err != nil {
		return err
	}

	// 重新生成 plugin name → file path 映射
	current := map[string]string{} // plugin_name → file path
	plugins := []*schema.Plugin{}

	for _, path := range files {
		p, err := schema.LoadFile(path)
		if err != nil {
			m.logger.Warn("load plugin failed, skip",
				zap.String("path", path), zap.Error(err))
			continue
		}
		// 校验
		opts := schema.DefaultValidateOptions()
		opts.KnownDatasources = m.knownDS
		if err := p.Validate(opts); err != nil {
			m.logger.Warn("validate plugin failed, skip",
				zap.String("plugin", p.Metadata.Name), zap.Error(err))
			continue
		}
		// 同一 plugin 多个文件 → 后到的覆盖
		if old, ok := current[p.Metadata.Name]; ok {
			m.logger.Warn("duplicate plugin name, override",
				zap.String("plugin", p.Metadata.Name),
				zap.String("old", old),
				zap.String("new", path),
			)
		}
		current[p.Metadata.Name] = path
		plugins = append(plugins, p)
	}

	// 计算 Remove 集合
	toRemove := []string{}
	for name := range m.loaded {
		if _, ok := current[name]; !ok {
			toRemove = append(toRemove, name)
		}
	}

	old, err := m.registry.Apply(schema.ApplyPlan{
		Upsert: plugins,
		Remove: toRemove,
	})
	if err != nil {
		m.logger.Error("registry apply failed", zap.Error(err))
		return err
	}

	// 更新 loaded
	m.loaded = map[string]time.Time{}
	for _, p := range plugins {
		m.loaded[p.Metadata.Name] = time.Now()
	}

	m.logger.Info("plugin scan complete",
		zap.Int("loaded", len(plugins)),
		zap.Int("removed", len(toRemove)),
		zap.Int64("old_version", old.Version),
		zap.Int64("new_version", m.registry.Snapshot().Version),
	)
	return nil
}

// discover 找到所有 plugin.yaml(每个子目录一个)
func (m *Manager) discover() ([]string, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 目录不存在不算错
		}
		return nil, fmt.Errorf("read plugin dir %q: %w", m.dir, err)
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(m.dir, e.Name(), "plugin.yaml")
		if _, err := os.Stat(candidate); err == nil {
			paths = append(paths, candidate)
		}
	}
	return paths, nil
}

// addRecursiveWatch 监听 dir 以及它下面所有子目录
// Windows fsnotify 对根目录的 watch 不会收到子目录内文件的事件,需要显式递归
func (m *Manager) addRecursiveWatch(w *fsnotify.Watcher, dir string) error {
	if err := w.Add(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // 读不到就不递归,不致命
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		_ = w.Add(sub)
		// 不深递归,plugin 结构是固定的 plugin/<name>/plugin.yaml 两层
	}
	return nil
}

// Reload 手动触发 reload(给 /admin/reload 用,W1 阶段先实现)
func (m *Manager) Reload() error {
	return m.scanAll()
}
