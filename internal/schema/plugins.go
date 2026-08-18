package schema

// PluginListEntry /admin/plugins 列表返回的条目
type PluginListEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Datasource  string `json:"datasource"`
	Owner       string `json:"owner"`
	Tags        []string `json:"tags"`
	CubeCount   int    `json:"cube_count"`
	GeneratedBy string `json:"generated_by,omitempty"`
}

// ListPlugins 返回 plugin 列表(供 /admin/plugins 使用)
func (r *Registry) ListPlugins() []PluginListEntry {
	snap := r.Snapshot()
	out := make([]PluginListEntry, 0, len(snap.Plugins))
	for _, p := range snap.Plugins {
		out = append(out, PluginListEntry{
			Name:        p.Metadata.Name,
			Version:     p.Metadata.Version,
			Description: p.Metadata.Description,
			Datasource:  p.Metadata.Datasource,
			Owner:       p.Metadata.Owner,
			Tags:        p.Metadata.Tags,
			CubeCount:   len(p.Spec.Cubes),
			GeneratedBy: p.Metadata.GeneratedBy,
		})
	}
	return out
}
