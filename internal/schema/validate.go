package schema

import (
	"fmt"
	"strings"
)

// ValidateOptions 校验选项
type ValidateOptions struct {
	MaxCubeJoins          int      // 阉割版默认 3
	RequireSecurityFilter bool     // 是否强制要求有 ${SECURITY.x} 占位符
	KnownDatasources      []string // 数据源白名单(为空则不校验)
}

// DefaultValidateOptions 默认阉割版约束
func DefaultValidateOptions() ValidateOptions {
	return ValidateOptions{
		MaxCubeJoins:          3,
		RequireSecurityFilter: false, // D3 阶段先不强求,D4 接入
	}
}

// Validate plugin 完整性校验
// 错误信息前缀 [E001] [E002] ... 方便定位
func (p *Plugin) Validate(opts ValidateOptions) error {
	if err := p.validateTopLevel(); err != nil {
		return err
	}
	if err := p.Metadata.validate(); err != nil {
		return err
	}
	if len(p.Spec.Cubes) == 0 {
		return fmt.Errorf("[E010] plugin.spec.cubes is empty (plugin %q)", p.Metadata.Name)
	}
	for i := range p.Spec.Cubes {
		if err := p.Spec.Cubes[i].validate(opts, p.Metadata.Name); err != nil {
			return err
		}
	}
	if len(opts.KnownDatasources) > 0 {
		found := false
		for _, ds := range opts.KnownDatasources {
			if ds == p.Metadata.Datasource {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("[E020] plugin %q references unknown datasource %q (known: %v)",
				p.Metadata.Name, p.Metadata.Datasource, opts.KnownDatasources)
		}
	}
	return nil
}

func (p *Plugin) validateTopLevel() error {
	if p.APIVersion != APIVersion {
		return fmt.Errorf("[E001] apiVersion must be %q, got %q", APIVersion, p.APIVersion)
	}
	if p.Kind != KindPlugin {
		return fmt.Errorf("[E002] kind must be %q, got %q", KindPlugin, p.Kind)
	}
	return nil
}

func (m PluginMetadata) validate() error {
	if m.Name == "" {
		return fmt.Errorf("[E003] metadata.name is required")
	}
	if !pluginNameRe.MatchString(m.Name) {
		return fmt.Errorf("[E004] metadata.name %q must match %s", m.Name, pluginNameRe.String())
	}
	if m.Datasource == "" {
		return fmt.Errorf("[E005] metadata.datasource is required (plugin %q)", m.Name)
	}
	if m.Owner == "" {
		return fmt.Errorf("[E006] metadata.owner is required (plugin %q)", m.Name)
	}
	return nil
}

func (c *Cube) validate(opts ValidateOptions, pluginName string) error {
	if c.Name == "" {
		return fmt.Errorf("[E011] cube.name is required (plugin %q)", pluginName)
	}
	if !pluginNameRe.MatchString(c.Name) {
		return fmt.Errorf("[E012] cube.name %q must match %s (plugin %q)", c.Name, pluginNameRe.String(), pluginName)
	}
	if c.SQL == "" {
		return fmt.Errorf("[E013] cube.sql is required (plugin %q / cube %q)", pluginName, c.Name)
	}
	if len(c.Measures) == 0 {
		return fmt.Errorf("[E014] cube must have at least 1 measure (plugin %q / cube %q)", pluginName, c.Name)
	}
	if len(c.Dimensions) == 0 {
		return fmt.Errorf("[E015] cube must have at least 1 dimension (plugin %q / cube %q)", pluginName, c.Name)
	}
	if len(c.Joins) > opts.MaxCubeJoins {
		return fmt.Errorf("[E016] cube %q has %d joins, exceeds max %d (阉割版限制)",
			c.Name, len(c.Joins), opts.MaxCubeJoins)
	}

	// 内部名称去重
	measureNames := map[string]bool{}
	for i, m := range c.Measures {
		if err := m.validate(pluginName, c.Name); err != nil {
			return err
		}
		if measureNames[m.Name] {
			return fmt.Errorf("[E017] duplicate measure %q (cube %q)", m.Name, c.Name)
		}
		measureNames[m.Name] = true
		_ = i
	}
	dimNames := map[string]bool{}
	for _, d := range c.Dimensions {
		if err := d.validate(pluginName, c.Name); err != nil {
			return err
		}
		if dimNames[d.Name] {
			return fmt.Errorf("[E018] duplicate dimension %q (cube %q)", d.Name, c.Name)
		}
		dimNames[d.Name] = true
	}
	segNames := map[string]bool{}
	for _, s := range c.Segments {
		if s.Name == "" {
			return fmt.Errorf("[E019] segment.name is required (cube %q)", c.Name)
		}
		if s.SQL == "" {
			return fmt.Errorf("[E019] segment.sql is required (cube %q / segment %q)", c.Name, s.Name)
		}
		if segNames[s.Name] {
			return fmt.Errorf("[E019] duplicate segment %q (cube %q)", s.Name, c.Name)
		}
		segNames[s.Name] = true
	}
	joinNames := map[string]bool{}
	for _, j := range c.Joins {
		if j.Name == "" || j.SQL == "" {
			return fmt.Errorf("[E01A] join.name and join.sql required (cube %q)", c.Name)
		}
		if j.Relationship != JoinManyToOne && j.Relationship != JoinOneToOne && j.Relationship != JoinOneToMany {
			return fmt.Errorf("[E01A] join %q has invalid relationship %q (cube %q)",
				j.Name, j.Relationship, c.Name)
		}
		if joinNames[j.Name] {
			return fmt.Errorf("[E01A] duplicate join %q (cube %q)", j.Name, c.Name)
		}
		joinNames[j.Name] = true
	}
	return nil
}

func (m Measure) validate(pluginName, cubeName string) error {
	if m.Name == "" {
		return fmt.Errorf("[E017] measure.name is required (plugin %q / cube %q)", pluginName, cubeName)
	}
	if !pluginNameRe.MatchString(m.Name) {
		return fmt.Errorf("[E017] measure.name %q must match %s (cube %q)", m.Name, pluginNameRe.String(), cubeName)
	}
	switch m.Type {
	case MeasureTypeCount:
		// count 不需要 sql
	case MeasureTypeSum, MeasureTypeAvg, MeasureTypeMin, MeasureTypeMax:
		if m.SQL == "" {
			return fmt.Errorf("[E017] measure %q (type %s) requires sql (cube %q)", m.Name, m.Type, cubeName)
		}
	default:
		return fmt.Errorf("[E017] measure %q has invalid type %q (cube %q)", m.Name, m.Type, cubeName)
	}
	return nil
}

func (d Dimension) validate(pluginName, cubeName string) error {
	if d.Name == "" {
		return fmt.Errorf("[E018] dimension.name is required (plugin %q / cube %q)", pluginName, cubeName)
	}
	if !pluginNameRe.MatchString(d.Name) {
		return fmt.Errorf("[E018] dimension.name %q must match %s (cube %q)", d.Name, pluginNameRe.String(), cubeName)
	}
	switch d.Type {
	case DimTypeString, DimTypeNumber, DimTypeTime, DimTypeBoolean:
		// ok
	default:
		return fmt.Errorf("[E018] dimension %q has invalid type %q (cube %q)", d.Name, d.Type, cubeName)
	}
	return nil
}

// String 返回可读摘要(给日志用)
func (p *Plugin) String() string {
	return fmt.Sprintf("plugin{name=%s, version=%s, cubes=[%s]}",
		p.Metadata.Name, p.Metadata.Version, strings.Join(p.CubeNames(), ","))
}
