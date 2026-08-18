package schema

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Schema 当前生效的 schema 快照
// 不可变: 每次 Apply 都生成新对象
type Schema struct {
	Version  int64                 // 单调递增
	LoadedAt time.Time
	Plugins  map[string]*Plugin    // plugin_name → plugin
	Cubes    map[string]*Cube      // cube_name → cube(全局索引)
}

// CubeWithPlugin cube + 所属 plugin(查询时回溯用)
type CubeWithPlugin struct {
	Cube   *Cube
	Plugin *Plugin
}

// ApplyPlan schema 变更计划
type ApplyPlan struct {
	Add    []*Plugin            // 新增
	Remove []string             // 删除 plugin name
	Upsert []*Plugin            // 覆盖(按 metadata.name)
}

// ErrNoChanges 没有任何变更
var ErrNoChanges = errors.New("no schema changes in apply plan")

// Event 变更事件
type Event struct {
	Type    EventType
	Version int64
	Plugins []string // 受影响的 plugin 列表
}

type EventType string

const (
	EventUpdated EventType = "updated"
	EventFailed  EventType = "failed"
)

// Registry 原子切换 + 多读单写
type Registry struct {
	snap  atomic.Pointer[Schema]
	mu    sync.Mutex
	evtCh chan Event
}

// NewRegistry 构造一个空的 registry
func NewRegistry() *Registry {
	initial := &Schema{
		Version:  0,
		LoadedAt: time.Now(),
		Plugins:  map[string]*Plugin{},
		Cubes:    map[string]*Cube{},
	}
	r := &Registry{evtCh: make(chan Event, 64)}
	r.snap.Store(initial)
	return r
}

// Snapshot 无锁读当前快照
func (r *Registry) Snapshot() *Schema {
	return r.snap.Load()
}

// Apply 原子应用变更
//   1. 复制旧 schema(COW)
//   2. 校验: Add/Upsert 必须 Validate 通过
//   3. 不影响已存在 cube 的同 Add 走 Upsert 路径
//   4. Swap 新 schema
//   5. 通知订阅者
//
// 失败时: 任何一步失败都返回错误,旧 schema 不变
func (r *Registry) Apply(plan ApplyPlan) (*Schema, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(plan.Add)+len(plan.Upsert)+len(plan.Remove) == 0 {
		return nil, ErrNoChanges
	}

	old := r.snap.Load()
	next := old.clone()

	// 校验所有待 add/upsert 的 plugin
	for _, p := range append(append([]*Plugin{}, plan.Add...), plan.Upsert...) {
		if err := p.Validate(DefaultValidateOptions()); err != nil {
			r.notify(Event{Type: EventFailed, Plugins: []string{p.Metadata.Name}})
			return nil, err
		}
	}

	// Remove
	for _, name := range plan.Remove {
		oldP, ok := next.Plugins[name]
		if !ok {
			continue
		}
		delete(next.Plugins, name)
		// 同时从 Cubes 索引里删
		for _, cn := range oldP.CubeNames() {
			delete(next.Cubes, cn)
		}
	}

	// Add + Upsert
	affected := []string{}
	for _, p := range append(append([]*Plugin{}, plan.Add...), plan.Upsert...) {
		// 如果是 upsert 且已存在,先删旧的 cube 索引
		if oldP, ok := next.Plugins[p.Metadata.Name]; ok {
			for _, cn := range oldP.CubeNames() {
				delete(next.Cubes, cn)
			}
		}
		next.Plugins[p.Metadata.Name] = p
		for i := range p.Spec.Cubes {
			c := &p.Spec.Cubes[i]
			// 全局 cube 名冲突检查(跨 plugin)
			if _, exists := next.Cubes[c.Name]; exists {
				return nil, errors.New("cube name conflict: " + c.Name)
			}
			next.Cubes[c.Name] = c
		}
		affected = append(affected, p.Metadata.Name)
	}
	sort.Strings(affected)

	next.Version = old.Version + 1
	next.LoadedAt = time.Now()

	r.snap.Store(next)
	r.notify(Event{Type: EventUpdated, Version: next.Version, Plugins: affected})
	return old, nil
}

// Subscribe 订阅 schema 变更事件
// 返回一个 channel 和一个取消函数
func (r *Registry) Subscribe() (<-chan Event, func()) {
	sub := make(chan Event, 16)
	// 简单实现: 用一个 fanout
	id := nextSubID.Add(1)
	subs.Store(id, sub)

	cancel := func() {
		subs.Delete(id)
		close(sub)
	}
	return sub, cancel
}

// Cube 查询 cube
func (s *Schema) Cube(name string) *Cube {
	return s.Cubes[name]
}

// CubeWithPlugin 查询 cube 及其所属 plugin
func (s *Schema) CubeWithPlugin(name string) *CubeWithPlugin {
	c, ok := s.Cubes[name]
	if !ok {
		return nil
	}
	// 反查所属 plugin
	for _, p := range s.Plugins {
		for i := range p.Spec.Cubes {
			if p.Spec.Cubes[i].Name == name {
				return &CubeWithPlugin{Cube: c, Plugin: p}
			}
		}
	}
	return nil
}

// AllCubes 列出所有 cube(按名排序)
func (s *Schema) AllCubes() []*Cube {
	out := make([]*Cube, 0, len(s.Cubes))
	for _, c := range s.Cubes {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// IsReady 是否就绪(D4 用于 readiness 探针)
func (s *Schema) IsReady() bool {
	// 这里可以加更复杂的检查
	// 当前: 至少有一个 plugin 加载成功就算 ready
	return s != nil
}

// ============================================================
// 内部
// ============================================================

// clone 浅拷贝 + 重建 map(COW)
func (s *Schema) clone() *Schema {
	c := &Schema{
		Version:  s.Version,
		LoadedAt: s.LoadedAt,
		Plugins:  make(map[string]*Plugin, len(s.Plugins)),
		Cubes:    make(map[string]*Cube, len(s.Cubes)),
	}
	for k, v := range s.Plugins {
		c.Plugins[k] = v
	}
	for k, v := range s.Cubes {
		c.Cubes[k] = v
	}
	return c
}

// notify 异步通知所有订阅者
// 满了就丢(非阻塞)
func (r *Registry) notify(e Event) {
	go func() {
		subs.Range(func(key, value any) bool {
			ch, ok := value.(chan Event)
			if !ok {
				return true
			}
			select {
			case ch <- e:
			default:
				// channel 满,丢;不阻塞 Apply
			}
			return true
		})
	}()
}

// ============================================================
// 订阅者管理(简单 fanout)
// ============================================================

var (
	nextSubID atomic.Int64
	subs      sync.Map
)
