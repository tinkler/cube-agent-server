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

// loadedFile 记录某个 plugin 上次成功加载时的文件路径和加载时间
// 路径用于区分"文件已删除"(Remove plugin) vs "文件存在但 YAML 解析失败"(保留旧 plugin)
type loadedFile struct {
	path string
	at   time.Time
}

// loadedPair 一次成功加载的 (path, plugin) 对,scanAll 内部用
type loadedPair struct {
	path   string
	plugin *schema.Plugin
}

// Manager plugin 生命周期管理
//   - 启动时扫描目录加载所有 plugin.yaml
//   - 监听目录的 fsnotify 事件
//   - 事件触发时 debounce 合并,然后 reload
//   - 失败时记录错误,旧 plugin 保持不变(rollback 自然由 Registry 保证)
type Manager struct {
	dir        string
	registry   *schema.Registry
	logger     *zap.Logger
	debounceMs int
	autoReload bool
	knownDS    []string

	mu     sync.Mutex
	loaded map[string]loadedFile // plugin_name → file path + 最后成功加载时间
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
		loaded:     map[string]loadedFile{},
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
			// 任何 plugin.yaml 的写/创建/删除/重命名都触发 reload
			// 提升到 Info:用户/运维需要能看到 fsnotify 是否收到事件
			// (Debug 在生产默认 info 级别下会被吞,造成"改了没反应"的错觉)
			if filepath.Base(ev.Name) == "plugin.yaml" {
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) != 0 {
					m.logger.Info("plugin.yaml changed, scheduling reload",
						zap.String("file", ev.Name),
						zap.String("op", ev.Op.String()),
					)
					scheduleReload()
				}
			} else {
				m.logger.Debug("fsnotify event (non-plugin)",
					zap.String("file", ev.Name),
					zap.String("op", ev.Op.String()),
				)
			}
			// 子目录创建 → 立即 watch 新目录(以防用户新增 plugin)
			if ev.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(ev.Name); statErr == nil && info.IsDir() {
					if addErr := w.Add(ev.Name); addErr == nil {
						m.logger.Info("watching new plugin subdir", zap.String("dir", ev.Name))
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
// 行为:
//   - 文件已被删除 (从 discover 结果中消失) → Remove plugin
//   - 文件存在但 YAML 解析/校验失败         → 保留旧 plugin(用户在编辑中,旧版继续可用)
//   - 文件存在且加载成功                    → Upsert 新 plugin
//
// 这个区分很关键:之前把所有"不在 successes 里"的 plugin 都加到 toRemove,
// 会导致编辑器中途保存半成品 YAML 时,plugin 被静默摘掉,造成"改了不生效"的错觉。
func (m *Manager) scanAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	files, err := m.discover()
	if err != nil {
		return err
	}

	// 磁盘上当前所有 plugin.yaml 路径
	onDisk := map[string]bool{}
	for _, p := range files {
		onDisk[p] = true
	}

	// 先快照旧的 m.loaded,后面算"preserved"要用
	oldLoaded := m.loaded

	var successes []loadedPair
	plugins := []*schema.Plugin{}
	failedPaths := map[string]bool{} // 这次加载失败的 path

	for _, path := range files {
		p, err := schema.LoadFile(path)
		if err != nil {
			m.logger.Warn("plugin file exists but failed to parse; keeping previous version",
				zap.String("path", path), zap.Error(err))
			failedPaths[path] = true
			continue
		}
		// 校验
		opts := schema.DefaultValidateOptions()
		opts.KnownDatasources = m.knownDS
		if err := p.Validate(opts); err != nil {
			m.logger.Warn("plugin file failed validation; keeping previous version",
				zap.String("plugin", p.Metadata.Name),
				zap.String("path", path), zap.Error(err))
			failedPaths[path] = true
			continue
		}
		// 同一 plugin 多个文件 → 后到的覆盖(警告)
		if old, ok := oldLoaded[p.Metadata.Name]; ok && old.path != path {
			m.logger.Warn("duplicate plugin name, override",
				zap.String("plugin", p.Metadata.Name),
				zap.String("old", old.path),
				zap.String("new", path),
			)
		}
		successes = append(successes, loadedPair{path: path, plugin: p})
		plugins = append(plugins, p)
	}

	// 计算 Remove 集合:只有"上次成功加载的文件已从磁盘消失"的 plugin 才真正删除
	//   (用户主动删除子目录,或 mv 到外部)
	// 之前这里只看 name,会把"文件还在但解析失败"的 plugin 误删
	toRemove := []string{}
	for name, lf := range oldLoaded {
		if !onDisk[lf.path] {
			toRemove = append(toRemove, name)
		}
	}

	// 统计"preserved":上次成功加载的 plugin,这次文件还在但解析失败 — 走"保留旧版"路径
	preserved := 0
	for _, lf := range oldLoaded {
		if onDisk[lf.path] && failedPaths[lf.path] {
			preserved++
		}
	}

	// 没有变化就不要调 Apply(Apply 会因为空 plan 返回 ErrNoChanges)
	// 这种情况典型的就是"用户编辑中,所有 plugin 都没变"或"全部解析失败"
	if len(plugins) == 0 && len(toRemove) == 0 {
		// 但 preserved > 0 时要打日志,让用户知道旧 plugin 还在
		if preserved > 0 {
			m.logger.Warn("plugin scan: all files failed to parse/validate; keeping previous versions",
				zap.Int("preserved", preserved),
			)
		}
		m.loaded = buildLoadedMap(successes, oldLoaded, onDisk, failedPaths, time.Now())
		return nil
	}

	old, err := m.registry.Apply(schema.ApplyPlan{
		Upsert: plugins,
		Remove: toRemove,
	})
	if err != nil {
		m.logger.Error("registry apply failed", zap.Error(err))
		return err
	}

	// 更新 loaded:这次成功加载的 plugin + preserved 的旧 plugin
	// 解析失败的 plugin 不进 m.loaded,但因为没加到 toRemove,registry 里旧版仍在
	m.loaded = buildLoadedMap(successes, oldLoaded, onDisk, failedPaths, time.Now())

	m.logger.Info("plugin scan complete",
		zap.Int("loaded", len(plugins)),
		zap.Int("removed", len(toRemove)),
		zap.Int("preserved", preserved),
		zap.Int64("old_version", old.Version),
		zap.Int64("new_version", m.registry.Snapshot().Version),
	)
	return nil
}

// buildLoadedMap 构造新的 m.loaded:
//   - 这次成功加载的 plugin 用新 path + 新时间
//   - preserved 的 plugin(文件存在但解析失败)沿用旧 path + 旧时间
//   - 其他 plugin 丢弃(它们要么不在磁盘,要么会通过 Apply 路径处理)
func buildLoadedMap(
	successes []loadedPair,
	oldLoaded map[string]loadedFile,
	onDisk map[string]bool,
	failedPaths map[string]bool,
	now time.Time,
) map[string]loadedFile {
	out := make(map[string]loadedFile, len(successes))
	for _, l := range successes {
		out[l.plugin.Metadata.Name] = loadedFile{path: l.path, at: now}
	}
	for name, lf := range oldLoaded {
		if onDisk[lf.path] && failedPaths[lf.path] {
			out[name] = lf
		}
	}
	return out
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
