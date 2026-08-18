// Package datasource 数据源元数据提取 + 缓存
package datasource

// Meta 数据源元数据
type Meta struct {
	Datasource    string       `yaml:"datasource" json:"datasource"`
	AnalyzedAt    string       `yaml:"analyzed_at" json:"analyzed_at"`
	SchemaVersion int          `yaml:"schema_version" json:"schema_version"`
	Tables        []TableMeta  `yaml:"tables" json:"tables"`
	Relations     []Relation   `yaml:"relations" json:"relations"`
	// 推断的(LLM 写入)
	InferredJoinPaths []InferredJoin `yaml:"inferred_join_paths,omitempty" json:"inferred_join_paths,omitempty"`
}

// TableMeta 单个表的元数据
type TableMeta struct {
	Name         string               `yaml:"name" json:"name"`
	Type         string               `yaml:"type,omitempty" json:"type,omitempty"` // fact/dimension/bridge(LLM 推断)
	Description  string               `yaml:"description,omitempty" json:"description,omitempty"`
	Reasoning    string               `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
	PrimaryKey   string               `yaml:"primary_key,omitempty" json:"primary_key,omitempty"`
	Columns      []ColumnMeta         `yaml:"columns" json:"columns"`
	ForeignKeys  []ForeignKeyMeta     `yaml:"foreign_keys,omitempty" json:"foreign_keys,omitempty"`
	// 数据质量(内省时统计)
	Quality      *QualityStats        `yaml:"quality,omitempty" json:"quality,omitempty"`
}

// ColumnMeta 字段元数据
type ColumnMeta struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"`
	Nullable bool   `yaml:"nullable" json:"nullable"`
	Comment  string `yaml:"comment,omitempty" json:"comment,omitempty"`
}

// ForeignKeyMeta 外键
type ForeignKeyMeta struct {
	Column     string  `yaml:"column" json:"column"`
	References string  `yaml:"references" json:"references"`
	Confidence float64 `yaml:"confidence" json:"confidence"`
}

// Relation 关系(从外键或命名约定推断)
type Relation struct {
	From       string  `yaml:"from" json:"from"`             // table.column
	To         string  `yaml:"to" json:"to"`                 // table.column
	Confidence float64 `yaml:"confidence" json:"confidence"` // 0.0-1.0
}

// InferredJoin LLM 推导的多跳 join 路径
type InferredJoin struct {
	From       string   `yaml:"from" json:"from"`
	To         string   `yaml:"to" json:"to"`
	Via        []string `yaml:"via" json:"via"` // 中间表
	Confidence float64  `yaml:"confidence" json:"confidence"`
}

// QualityStats 数据质量统计
type QualityStats struct {
	TotalRows    int64              `yaml:"total_rows" json:"total_rows"`
	NullAlerts   map[string]float64 `yaml:"null_alerts,omitempty" json:"null_alerts,omitempty"`
	DistinctCnt  map[string]int64   `yaml:"distinct_count,omitempty" json:"distinct_count,omitempty"`
}
