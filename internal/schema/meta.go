package schema

import "fmt"

// MetaProvider 适配 schema.Registry → cube.js 兼容的 meta 格式
//   供 /v1/meta 和 /cubejs-api/v1/meta 使用
//   D2:返回硬编码 mock,D3 切到 Registry 真实数据
type MetaProvider struct {
	r *Registry
}

// NewMetaProvider 构造 meta provider
func NewMetaProvider(r *Registry) *MetaProvider {
	return &MetaProvider{r: r}
}

// GetMeta 返回 cube.js 兼容的元数据
func (p *MetaProvider) GetMeta() any {
	snap := p.r.Snapshot()
	if snap == nil {
		return map[string]any{"cubes": []any{}}
	}

	cubes := make([]any, 0, len(snap.Cubes))
	for _, c := range snap.AllCubes() {
		cubes = append(cubes, cubeToMeta(c, snap))
	}

	return map[string]any{
		"cubes":   cubes,
		"version": snap.Version,
	}
}

func cubeToMeta(c *Cube, s *Schema) map[string]any {
	measures := make([]any, 0, len(c.Measures))
	for _, m := range c.Measures {
		entry := map[string]any{
			"name":    fmt.Sprintf("%s.%s", c.Name, m.Name),
			"title":   humanTitle(m.Name),
			"type":    m.Type,
			"aggType": m.Type,
		}
		if m.SQL != "" {
			entry["sql"] = m.SQL
		}
		if m.Description != "" {
			entry["description"] = m.Description
		}
		measures = append(measures, entry)
	}

	dimensions := make([]any, 0, len(c.Dimensions))
	for _, d := range c.Dimensions {
		entry := map[string]any{
			"name":  fmt.Sprintf("%s.%s", c.Name, d.Name),
			"title": humanTitle(d.Name),
			"type":  d.Type,
		}
		if d.PrimaryKey {
			entry["primaryKey"] = true
		}
		if d.SQL != "" {
			entry["sql"] = d.SQL
		}
		if d.Description != "" {
			entry["description"] = d.Description
		}
		dimensions = append(dimensions, entry)
	}

	segments := make([]any, 0, len(c.Segments))
	for _, s2 := range c.Segments {
		segments = append(segments, map[string]any{
			"name":        fmt.Sprintf("%s.%s", c.Name, s2.Name),
			"title":       humanTitle(s2.Name),
			"sql":         s2.SQL,
			"description": s2.Description,
		})
	}

	joins := make([]any, 0, len(c.Joins))
	for _, j := range c.Joins {
		joins = append(joins, map[string]any{
			"name":         fmt.Sprintf("%s.%s", c.Name, j.Name),
			"sql":          j.SQL,
			"relationship": j.Relationship,
		})
	}

	_ = s

	return map[string]any{
		"name":        c.Name,
		"title":       humanTitle(c.Name),
		"description": c.Description,
		"sql":         c.SQL,
		"measures":    measures,
		"dimensions":  dimensions,
		"segments":    segments,
		"joins":       joins,
	}
}

// humanTitle 把下划线命名转成可读 Title:
//   "total_amount" → "Total Amount"
//   "created_at"   → "Created At"
// D5 阶段会接中文 title 字段
func humanTitle(s string) string {
	out := make([]rune, 0, len(s)+5)
	upper := true
	for _, r := range s {
		switch {
		case r == '_' || r == '-':
			out = append(out, ' ')
			upper = true
		case upper:
			if r >= 'a' && r <= 'z' {
				r = r - 32
			}
			out = append(out, r)
			upper = false
		default:
			out = append(out, r)
			upper = false
		}
	}
	return string(out)
}
