package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ParseError 解析错误(含字段路径)
type ParseError struct {
	Field   string
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("query: %s: %s", e.Field, e.Message)
}

// Parse 从 JSON 字节解析 + 基础合法性校验
//   阉割版: 至少要有 measures 或 dimensions 之一
//   任何 measure/dimension 必须是 "cube.field" 形式
func Parse(data []byte) (*Query, error) {
	q := &Query{}
	if err := json.Unmarshal(data, q); err != nil {
		return nil, &ParseError{Field: "root", Message: err.Error()}
	}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	return q, nil
}

// Validate 基础合法性校验
func (q *Query) Validate() error {
	if len(q.Measures) == 0 && len(q.Dimensions) == 0 && len(q.TimeDimensions) == 0 {
		return &ParseError{Field: "measures/dimensions", Message: "at least one of measures/dimensions/timeDimensions is required"}
	}

	// 字段名必须是 cube.field 形式
	checkDotted := func(field string, names []string) error {
		for _, n := range names {
			if !strings.Contains(n, ".") {
				return &ParseError{Field: field, Message: fmt.Sprintf("field %q must be in 'cube.field' format", n)}
			}
		}
		return nil
	}
	if err := checkDotted("measures", q.Measures); err != nil {
		return err
	}
	if err := checkDotted("dimensions", q.Dimensions); err != nil {
		return err
	}
	if err := checkDotted("segments", q.Segments); err != nil {
		return err
	}
	for i, td := range q.TimeDimensions {
		if !strings.Contains(td.Dimension, ".") {
			return &ParseError{Field: fmt.Sprintf("timeDimensions[%d].dimension", i), Message: fmt.Sprintf("field %q must be in 'cube.field' format", td.Dimension)}
		}
		if td.Granularity != "" && !ValidGranularities[td.Granularity] {
			return &ParseError{Field: fmt.Sprintf("timeDimensions[%d].granularity", i), Message: fmt.Sprintf("invalid granularity %q", td.Granularity)}
		}
	}
	// timeRollup 验证
	for i, g := range q.TimeRollup {
		if !ValidGranularities[g] {
			return &ParseError{Field: fmt.Sprintf("timeRollup[%d]", i), Message: fmt.Sprintf("invalid granularity %q", g)}
		}
	}
	for i, f := range q.Filters {
		if !strings.Contains(f.Member, ".") && len(f.And) == 0 && len(f.Or) == 0 {
			return &ParseError{Field: fmt.Sprintf("filters[%d].member", i), Message: fmt.Sprintf("field %q must be in 'cube.field' format", f.Member)}
		}
		if !ValidOperators[f.Operator] {
			return &ParseError{Field: fmt.Sprintf("filters[%d].operator", i), Message: fmt.Sprintf("invalid operator %q", f.Operator)}
		}
	}
	// 阉割版:limit 兜底
	if q.Limit == nil {
		def := 10000
		q.Limit = &def
	}
	if *q.Limit > 50000 {
		return &ParseError{Field: "limit", Message: "limit exceeds max 50000 (阉割版约束)"}
	}
	return nil
}

// ReferencedCubes 提取 query 涉及的所有 cube 名称
func (q *Query) ReferencedCubes() []string {
	set := map[string]struct{}{}
	add := func(name string) {
		idx := strings.Index(name, ".")
		if idx < 0 {
			return
		}
		set[name[:idx]] = struct{}{}
	}
	for _, m := range q.Measures {
		add(m)
	}
	for _, d := range q.Dimensions {
		add(d)
	}
	for _, s := range q.Segments {
		add(s)
	}
	for _, f := range q.Filters {
		add(f.Member)
	}
	for _, td := range q.TimeDimensions {
		add(td.Dimension)
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	return out
}

// ErrNoQuery 空 query
var ErrNoQuery = errors.New("empty query")
