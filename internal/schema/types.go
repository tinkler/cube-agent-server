// Package schema 定义数据模型 + 原子 Registry + YAML 解析 + 校验
package schema

// ============================================================
// DSL 版本
// ============================================================

// APIVersion 当前支持的 DSL 版本
const APIVersion = "cube-agent/v1"

// Kind 固定值
const KindPlugin = "Plugin"

// ============================================================
// 顶层:Plugin
// ============================================================

// Plugin 一个 plugin 描述一组 cube
// 来自 plugins/<name>/plugin.yaml
type Plugin struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind" json:"kind"`
	Metadata   PluginMetadata `yaml:"metadata" json:"metadata"`
	Spec       PluginSpec     `yaml:"spec" json:"spec"`
}

// PluginMetadata plugin 元信息
type PluginMetadata struct {
	Name        string   `yaml:"name" json:"name"`
	Version     string   `yaml:"version" json:"version"`
	Description string   `yaml:"description" json:"description"`
	Datasource  string   `yaml:"datasource" json:"datasource"`
	Owner       string   `yaml:"owner" json:"owner"`
	Tags        []string `yaml:"tags" json:"tags"`
	GeneratedBy string   `yaml:"generated_by" json:"generated_by"` // "skill" | ""
}

// PluginSpec plugin 内容
type PluginSpec struct {
	Cubes []Cube `yaml:"cubes" json:"cubes"`
}

// ============================================================
// Cube
// ============================================================

// Cube 数据立方体
type Cube struct {
	Name        string      `yaml:"name" json:"name"`
	SQL         string      `yaml:"sql" json:"sql"`
	Description string      `yaml:"description" json:"description"`
	PrimaryKey  string      `yaml:"primary_key" json:"primary_key"`
	Measures    []Measure   `yaml:"measures" json:"measures"`
	Dimensions  []Dimension `yaml:"dimensions" json:"dimensions"`
	Segments    []Segment   `yaml:"segments" json:"segments"`
	Joins       []Join      `yaml:"joins" json:"joins"`
}

// Measure 指标
type Measure struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type" json:"type"` // count / sum / avg / min / max
	SQL         string `yaml:"sql" json:"sql"`
	Description string `yaml:"description" json:"description"`
}

// Dimension 维度
type Dimension struct {
	Name        string `yaml:"name" json:"name"`
	SQL         string `yaml:"sql" json:"sql"`
	Type        string `yaml:"type" json:"type"` // string / number / time / boolean
	PrimaryKey  bool   `yaml:"primary_key" json:"primary_key"`
	Description string `yaml:"description" json:"description"`
}

// Segment 段过滤(预定义 WHERE 条件)
type Segment struct {
	Name        string `yaml:"name" json:"name"`
	SQL         string `yaml:"sql" json:"sql"`
	Description string `yaml:"description" json:"description"`
}

// Join 关联
type Join struct {
	Name         string `yaml:"name" json:"name"`
	SQL          string `yaml:"sql" json:"sql"`
	Relationship string `yaml:"relationship" json:"relationship"` // many_to_one / one_to_one
}

// ============================================================
// 类型常量
// ============================================================

// Measure 类型
const (
	MeasureTypeCount = "count"
	MeasureTypeSum   = "sum"
	MeasureTypeAvg   = "avg"
	MeasureTypeMin   = "min"
	MeasureTypeMax   = "max"
)

// Dimension 类型
const (
	DimTypeString  = "string"
	DimTypeNumber  = "number"
	DimTypeTime    = "time"
	DimTypeBoolean = "boolean"
)

// Join 关系
const (
	JoinManyToOne = "many_to_one"
	JoinOneToOne  = "one_to_one"
	JoinOneToMany = "one_to_many" // 阉割版不支持
)
