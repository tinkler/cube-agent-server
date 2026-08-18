// Package security 行级安全上下文
// 把请求带的安全信息(tenant_id 等)塞到 Context,用于 SQL 渲染时
// 替换 ${SECURITY.x} 占位符
package security

// Context 当前请求的安全上下文
// 阉割版: 只支持少量 key(tenant_id / supplier_ids 等)
type Context struct {
	values map[string]string
}

// NewContext 从 map 构造
func NewContext(values map[string]string) *Context {
	if values == nil {
		values = map[string]string{}
	}
	return &Context{values: values}
}

// Get 取值
func (c *Context) Get(key string) (string, bool) {
	if c == nil {
		return "", false
	}
	v, ok := c.values[key]
	return v, ok
}

// Set 写入
func (c *Context) Set(key, value string) {
	if c.values == nil {
		c.values = map[string]string{}
	}
	c.values[key] = value
}

// FromRequest 从 HTTP 请求构造(简化版)
// W3 接入 JWT/cookie 之后这里做实际解析
func FromRequest(headers map[string]string) *Context {
	c := NewContext(nil)
	if v := headers["X-Tenant-Id"]; v != "" {
		c.Set("tenant_id", v)
	}
	if v := headers["X-User-Id"]; v != "" {
		c.Set("user_id", v)
	}
	if v := headers["X-Supplier-Ids"]; v != "" {
		c.Set("supplier_ids", v)
	}
	return c
}
