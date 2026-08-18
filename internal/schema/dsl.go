package schema

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

// pluginNameRe plugin / cube 命名规范
// 允许:小写字母、数字、下划线、中划线
var pluginNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// LoadFile 加载并解析 plugin YAML 文件
func LoadFile(path string) (*Plugin, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin file %q: %w", path, err)
	}
	return Load(data)
}

// Load 从字节流解析 plugin
func Load(data []byte) (*Plugin, error) {
	p := &Plugin{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // 严格模式,未知字段报错
	if err := dec.Decode(p); err != nil {
		return nil, fmt.Errorf("decode plugin yaml: %w", err)
	}
	return p, nil
}

// IDs 返回 plugin 的稳定 ID 列表(cube names)
// 用于版本/快照比对
func (p *Plugin) CubeNames() []string {
	names := make([]string, 0, len(p.Spec.Cubes))
	for _, c := range p.Spec.Cubes {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return names
}

// FindCube 按名查找 cube
func (p *Plugin) FindCube(name string) *Cube {
	for i := range p.Spec.Cubes {
		if p.Spec.Cubes[i].Name == name {
			return &p.Spec.Cubes[i]
		}
	}
	return nil
}
