// Package handlers gin 处理器集合
package handlers

// MetaProvider /v1/meta 的数据源抽象
// 实现方:
//   - MockMetaProvider(D2 测试用)
//   - schema.MetaProvider(D3 之后,生产用)
type MetaProvider interface {
	GetMeta() any
}

// ReadinessChecker /readyz 的依赖
//   至少 1 个 plugin 加载成功才返回 ready
// 实现方: *plugin.Manager
type ReadinessChecker interface {
	LoadedCount() int
}
