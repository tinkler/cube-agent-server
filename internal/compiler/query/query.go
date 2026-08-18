// Package query 定义 JSON Query 数据结构(对齐 cube.js)
// 以及 pass1 解析
package query

// Query cube.js 兼容的 JSON Query
//   参见 https://cube.dev/docs/query-format
//
// 阉割版约束:
//   - 最多 1 个 cube(无 join / 或单层 join)
//   - 简单时间维度(granularity ∈ {day, week, month, quarter, year, hour})
//   - 简单 filter operator
type Query struct {
	Measures       []string        `json:"measures"`
	Dimensions     []string        `json:"dimensions"`
	Filters        []Filter        `json:"filters"`
	TimeDimensions []TimeDimension `json:"timeDimensions"`
	Segments       []string        `json:"segments"`
	Order          [][]any         `json:"order"` // [field, dir]
	Limit          *int            `json:"limit,omitempty"`
	Offset         *int            `json:"offset,omitempty"`

	// TimeRollup 阉割版扩展:Go 端对 time dim 额外做多粒度 rollup
	//   例:query 查 day,TimeRollup: ["week", "month"] → 返回 daily + weekly + monthly 三种粒度
	//   目的:避免 3 次 DB round-trip,Go 端做内存聚合,减轻 DB 压力
	//   只支持 "day"/"week"/"month"/"quarter"/"year" 这些粒度
	TimeRollup []string `json:"timeRollup,omitempty"`

	// 内部字段(非 cube.js)
	RequestID string `json:"-"`
}

// Filter 过滤
type Filter struct {
	Member   string `json:"member"`
	Operator string `json:"operator"`
	Values   []any  `json:"values"`
	And      []Filter `json:"and,omitempty"`
	Or       []Filter `json:"or,omitempty"`
}

// TimeDimension 时间维度
type TimeDimension struct {
	Dimension   string   `json:"dimension"`
	DateRange   []string `json:"dateRange,omitempty"`   // [from, to]  ISO 8601
	Granularity string   `json:"granularity,omitempty"`  // day/week/month/...
}

// 常量:Operator
const (
	OpEquals        = "equals"
	OpNotEquals     = "notEquals"
	OpContains      = "contains"
	OpNotContains   = "notContains"
	OpStartsWith    = "startsWith"
	OpNotStartsWith = "notStartsWith"
	OpEndsWith      = "endsWith"
	OpNotEndsWith   = "notEndsWith"
	OpGT            = "gt"
	OpGTE           = "gte"
	OpLT            = "lt"
	OpLTE           = "lte"
	OpIn            = "in"
	OpNotIn         = "notIn"
	OpSet           = "set"
	OpNotSet        = "notSet"
	OpBefore        = "before"
	OpAfter         = "after"
	OpInDateRange   = "inDateRange"
	OpNotInDateRange = "notInDateRange"
)

// 常量:Granularity
const (
	GranularityHour    = "hour"
	GranularityDay     = "day"
	GranularityWeek    = "week"
	GranularityMonth   = "month"
	GranularityQuarter = "quarter"
	GranularityYear    = "year"
)

// ValidOperators 阉割版支持的 operator
var ValidOperators = map[string]bool{
	OpEquals:         true,
	OpNotEquals:      true,
	OpContains:       true,
	OpNotContains:    true,
	OpStartsWith:     true,
	OpNotStartsWith:  true,
	OpEndsWith:       true,
	OpNotEndsWith:    true,
	OpGT:             true,
	OpGTE:            true,
	OpLT:             true,
	OpLTE:            true,
	OpIn:             true,
	OpNotIn:          true,
	OpSet:            true,
	OpNotSet:         true,
	OpBefore:         true,
	OpAfter:          true,
	OpInDateRange:    true,
	OpNotInDateRange: true,
}

// ValidGranularities 阉割版支持的时间粒度
var ValidGranularities = map[string]bool{
	GranularityHour:    true,
	GranularityDay:     true,
	GranularityWeek:    true,
	GranularityMonth:   true,
	GranularityQuarter: true,
	GranularityYear:    true,
}
