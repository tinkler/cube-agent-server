// Package compiler pass2 reference resolution + pass3 SQL generation
package compiler

import (
	"fmt"
	"strings"

	"github.com/tinkler/cube-agent-server/internal/compiler/query"
	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
	"github.com/tinkler/cube-agent-server/internal/schema"
)

// ResolvedMeasure resolved measure
type ResolvedMeasure struct {
	Name  string         // original ref: "orders.count"
	Cube  string         // "orders"
	Field string         // "count"
	Def   *schema.Measure
	Expr  sqlbuilder.Expr // compiled SQL expression
}

// ResolvedDimension resolved dimension
type ResolvedDimension struct {
	Name  string
	Cube  string
	Field string
	Def   *schema.Dimension
	Expr  sqlbuilder.Expr
}

// ResolvedTimeDimension time dimension (with granularity)
type ResolvedTimeDimension struct {
	Name        string
	Cube        string
	Field       string
	Def         *schema.Dimension
	Granularity string
	DateRange   []string
	Expr        sqlbuilder.Expr // DATE_TRUNC expression
}

// ResolvedFilter resolved filter
type ResolvedFilter struct {
	Source FilterSource
	Op     string
	Values []any
	Expr   sqlbuilder.Expr
}

type FilterSource int

const (
	FilterFromMeasure FilterSource = iota
	FilterFromDimension
	FilterFromSegment
)

// ResolvedIR Pass2 output
type ResolvedIR struct {
	Query          *IR
	PrimaryCube    string             // primary cube
	Cube           *schema.Cube       // primary cube pointer
	Cubes          []*schema.Cube     // all cubes in query (W5: 1-2 cubes)
	Measures       []ResolvedMeasure
	Dimensions     []ResolvedDimension
	TimeDimensions []ResolvedTimeDimension
	Filters        []ResolvedFilter
	Segments       []string
	OrderBy        []sqlbuilder.OrderBy
	Limit          int
	Offset         int
}

// Pass2 reference resolution
// W2: 1 cube only
// W5: 1-2 cubes via explicit join
func Pass2(ir *IR, reg *schema.Registry) (*ResolvedIR, error) {
	snap := reg.Snapshot()
	if snap == nil {
		return nil, fmt.Errorf("compiler: empty schema registry")
	}
	cubes := ir.ReferencedCubes
	if len(cubes) == 0 {
		return nil, fmt.Errorf("compiler: no cubes referenced in query")
	}
	if len(cubes) > 2 {
		return nil, fmt.Errorf("compiler: max 2 cubes supported, got %d", len(cubes))
	}

	cubeName := cubes[0]
	cwp := snap.CubeWithPlugin(cubeName)
	if cwp == nil {
		return nil, fmt.Errorf("compiler: cube %q not found in schema", cubeName)
	}
	cube := cwp.Cube
	cubePtrs := []*schema.Cube{cube}

	if len(cubes) == 2 {
		otherName := cubes[1]
		otherCwp := snap.CubeWithPlugin(otherName)
		if otherCwp == nil {
			return nil, fmt.Errorf("compiler: cube %q not found in schema", otherName)
		}
		join := findJoin(cube, otherName)
		if join == nil {
			return nil, fmt.Errorf("compiler: cube %q has no join to %q", cubeName, otherName)
		}
		cubePtrs = append(cubePtrs, otherCwp.Cube)
	}

	resolved := &ResolvedIR{
		Query:       ir,
		PrimaryCube: cubeName,
		Cube:        cube,
		Cubes:       cubePtrs,
		Limit:       *ir.Query.Limit,
	}
	if ir.Query.Offset != nil {
		resolved.Offset = *ir.Query.Offset
	}

	// 1. Measures
	for _, m := range ir.Query.Measures {
		rm, err := resolveMeasureInCubes(m, cubePtrs)
		if err != nil {
			return nil, err
		}
		resolved.Measures = append(resolved.Measures, rm)
	}

	// 2. Dimensions
	for _, d := range ir.Query.Dimensions {
		rd, err := resolveDimensionInCubes(d, cubePtrs)
		if err != nil {
			return nil, err
		}
		resolved.Dimensions = append(resolved.Dimensions, rd)
	}

	// 3. TimeDimensions
	for _, td := range ir.Query.TimeDimensions {
		rtd, err := resolveTimeDimensionInCubes(td, cubePtrs)
		if err != nil {
			return nil, err
		}
		resolved.TimeDimensions = append(resolved.TimeDimensions, rtd)
	}

	// 4. Filters
	for _, f := range ir.Query.Filters {
		rf, err := resolveFilterInCubes(f, cubePtrs, snap)
		if err != nil {
			return nil, err
		}
		resolved.Filters = append(resolved.Filters, rf)
	}

	// 5. Segments
	resolved.Segments = ir.Query.Segments

	// 6. Order
	for _, o := range ir.Query.Order {
		if len(o) < 1 {
			continue
		}
		fieldName, _ := o[0].(string)
		dir := "asc"
		if len(o) >= 2 {
			if d, ok := o[1].(string); ok {
				dir = strings.ToLower(d)
			}
		}
		expr, err := resolveOrderField(fieldName, resolved)
		if err != nil {
			return nil, err
		}
		resolved.OrderBy = append(resolved.OrderBy, sqlbuilder.OrderBy{
			Expr: expr,
			Desc: dir == "desc",
		})
	}

	return resolved, nil
}

func resolveMeasure(ref string, cube *schema.Cube) (ResolvedMeasure, error) {
	cubeName, fieldName, err := splitCubeField(ref)
	if err != nil {
		return ResolvedMeasure{}, err
	}
	if cubeName != cube.Name {
		return ResolvedMeasure{}, fmt.Errorf("measure %q references wrong cube (expected %q)", ref, cube.Name)
	}
	for i := range cube.Measures {
		m := &cube.Measures[i]
		if m.Name == fieldName {
			return ResolvedMeasure{
				Name:  ref,
				Cube:  cubeName,
				Field: fieldName,
				Def:   m,
				Expr:  buildMeasureExpr(m),
			}, nil
		}
	}
	return ResolvedMeasure{}, fmt.Errorf("measure %q not found in cube %q", ref, cube.Name)
}

func buildMeasureExpr(m *schema.Measure) sqlbuilder.Expr {
	col := sqlbuilder.Col(fieldSQL(m.SQL, m.Name))
	switch m.Type {
	case schema.MeasureTypeCount:
		if m.SQL == "" {
			return sqlbuilder.CountStar()
		}
		return sqlbuilder.Count(col)
	case schema.MeasureTypeSum:
		return sqlbuilder.Sum(col)
	case schema.MeasureTypeAvg:
		return sqlbuilder.Avg(col)
	case schema.MeasureTypeMin:
		return sqlbuilder.Min(col)
	case schema.MeasureTypeMax:
		return sqlbuilder.Max(col)
	default:
		return col
	}
}

func resolveDimension(ref string, cube *schema.Cube) (ResolvedDimension, error) {
	cubeName, fieldName, err := splitCubeField(ref)
	if err != nil {
		return ResolvedDimension{}, err
	}
	if cubeName != cube.Name {
		return ResolvedDimension{}, fmt.Errorf("dimension %q references wrong cube (expected %q)", ref, cube.Name)
	}
	for i := range cube.Dimensions {
		d := &cube.Dimensions[i]
		if d.Name == fieldName {
			return ResolvedDimension{
				Name:  ref,
				Cube:  cubeName,
				Field: fieldName,
				Def:   d,
				Expr:  sqlbuilder.Col(fieldSQL(d.SQL, d.Name)),
			}, nil
		}
	}
	return ResolvedDimension{}, fmt.Errorf("dimension %q not found in cube %q", ref, cube.Name)
}

// findJoin lookup join in cube to targetName
func findJoin(cube *schema.Cube, targetName string) *schema.Join {
	for i := range cube.Joins {
		if cube.Joins[i].Name == targetName {
			return &cube.Joins[i]
		}
	}
	return nil
}

// resolveMeasureInCubes lookup measure across multiple cubes
func resolveMeasureInCubes(ref string, cubes []*schema.Cube) (ResolvedMeasure, error) {
	cubeName, _, _ := splitCubeField(ref)
	for _, c := range cubes {
		if c.Name != cubeName {
			continue
		}
		return resolveMeasure(ref, c)
	}
	return ResolvedMeasure{}, fmt.Errorf("measure %q: cube %q not in query context", ref, cubeName)
}

// resolveDimensionInCubes lookup dimension across multiple cubes
func resolveDimensionInCubes(ref string, cubes []*schema.Cube) (ResolvedDimension, error) {
	cubeName, _, _ := splitCubeField(ref)
	for _, c := range cubes {
		if c.Name != cubeName {
			continue
		}
		return resolveDimension(ref, c)
	}
	return ResolvedDimension{}, fmt.Errorf("dimension %q: cube %q not in query context", ref, cubeName)
}

// resolveTimeDimensionInCubes lookup time dimension across multiple cubes
func resolveTimeDimensionInCubes(td query.TimeDimension, cubes []*schema.Cube) (ResolvedTimeDimension, error) {
	cubeName, _, _ := splitCubeField(td.Dimension)
	for _, c := range cubes {
		if c.Name != cubeName {
			continue
		}
		return resolveTimeDimension(td, c)
	}
	return ResolvedTimeDimension{}, fmt.Errorf("time dimension %q: cube %q not in query context", td.Dimension, cubeName)
}

// resolveFilterInCubes lookup filter across multiple cubes
func resolveFilterInCubes(f query.Filter, cubes []*schema.Cube, snap *schema.Schema) (ResolvedFilter, error) {
	cubeName, _, _ := splitCubeField(f.Member)
	for _, c := range cubes {
		if c.Name != cubeName {
			continue
		}
		return resolveFilter(f, c, snap)
	}
	return ResolvedFilter{}, fmt.Errorf("filter %q: cube %q not in query context", f.Member, cubeName)
}

func resolveTimeDimension(td query.TimeDimension, cube *schema.Cube) (ResolvedTimeDimension, error) {
	cubeName, fieldName, err := splitCubeField(td.Dimension)
	if err != nil {
		return ResolvedTimeDimension{}, err
	}
	if cubeName != cube.Name {
		return ResolvedTimeDimension{}, fmt.Errorf("time dimension %q references wrong cube", td.Dimension)
	}
	for i := range cube.Dimensions {
		d := &cube.Dimensions[i]
		if d.Name == fieldName {
			if d.Type != schema.DimTypeTime {
				return ResolvedTimeDimension{}, fmt.Errorf("dimension %q is not a time type", fieldName)
			}
			baseCol := sqlbuilder.Col(fieldSQL(d.SQL, d.Name))
			var expr sqlbuilder.Expr = baseCol
			if td.Granularity != "" {
				expr = sqlbuilder.DateTrunc(td.Granularity, baseCol)
			}
			return ResolvedTimeDimension{
				Name:        td.Dimension,
				Cube:        cubeName,
				Field:       fieldName,
				Def:         d,
				Granularity: td.Granularity,
				DateRange:   td.DateRange,
				Expr:        expr,
			}, nil
		}
	}
	return ResolvedTimeDimension{}, fmt.Errorf("time dimension %q not found in cube %q", td.Dimension, cube.Name)
}

func resolveFilter(f query.Filter, cube *schema.Cube, snap *schema.Schema) (ResolvedFilter, error) {
	cubeName, fieldName, err := splitCubeField(f.Member)
	if err != nil {
		return ResolvedFilter{}, err
	}
	if cubeName != cube.Name {
		return ResolvedFilter{}, fmt.Errorf("filter %q references wrong cube", f.Member)
	}
	for i := range cube.Dimensions {
		d := &cube.Dimensions[i]
		if d.Name == fieldName {
			col := sqlbuilder.Col(fieldSQL(d.SQL, d.Name))
			expr := buildFilterExpr(col, f, d.Type)
			src := FilterFromDimension
			return ResolvedFilter{Source: src, Op: f.Operator, Values: f.Values, Expr: expr}, nil
		}
	}
	return ResolvedFilter{}, fmt.Errorf("filter field %q not found in cube %q", fieldName, cube.Name)
}

func resolveOrderField(fieldName string, ir *ResolvedIR) (sqlbuilder.Expr, error) {
	for _, m := range ir.Measures {
		if m.Name == fieldName {
			return m.Expr, nil
		}
	}
	for _, d := range ir.Dimensions {
		if d.Name == fieldName {
			return d.Expr, nil
		}
	}
	for _, td := range ir.TimeDimensions {
		if td.Name == fieldName {
			return td.Expr, nil
		}
	}
	return nil, fmt.Errorf("order field %q not found in query", fieldName)
}

func splitCubeField(ref string) (cube, field string, err error) {
	idx := strings.Index(ref, ".")
	if idx < 0 {
		return "", "", fmt.Errorf("field %q must be in 'cube.field' format", ref)
	}
	return ref[:idx], ref[idx+1:], nil
}

func fieldSQL(sql, name string) string {
	if sql != "" {
		return sql
	}
	return name
}

func buildFilterExpr(col sqlbuilder.Expr, f query.Filter, dimType string) sqlbuilder.Expr {
	values := f.Values
	switch f.Operator {
	case "equals":
		if len(values) >= 1 {
			return sqlbuilder.Eq(col, sqlbuilder.Lit(values[0]))
		}
	case "notEquals":
		if len(values) >= 1 {
			return sqlbuilder.Ne(col, sqlbuilder.Lit(values[0]))
		}
	case "gt":
		if len(values) >= 1 {
			return sqlbuilder.Gt(col, sqlbuilder.Lit(values[0]))
		}
	case "gte":
		if len(values) >= 1 {
			return sqlbuilder.Ge(col, sqlbuilder.Lit(values[0]))
		}
	case "lt":
		if len(values) >= 1 {
			return sqlbuilder.Lt(col, sqlbuilder.Lit(values[0]))
		}
	case "lte":
		if len(values) >= 1 {
			return sqlbuilder.Le(col, sqlbuilder.Lit(values[0]))
		}
	case "contains":
		if len(values) >= 1 {
			return sqlbuilder.Like(col, sqlbuilder.Lit("%"+toString(values[0])+"%"))
		}
	case "notContains":
		if len(values) >= 1 {
			return sqlbuilder.Not_(sqlbuilder.Like(col, sqlbuilder.Lit("%"+toString(values[0])+"%")))
		}
	case "startsWith":
		if len(values) >= 1 {
			return sqlbuilder.Like(col, sqlbuilder.Lit(toString(values[0])+"%"))
		}
	case "endsWith":
		if len(values) >= 1 {
			return sqlbuilder.Like(col, sqlbuilder.Lit("%"+toString(values[0])))
		}
	case "in":
		params := make([]sqlbuilder.Expr, len(values))
		for i, v := range values {
			params[i] = sqlbuilder.Lit(v)
		}
		return sqlbuilder.In(col, params)
	case "notIn":
		params := make([]sqlbuilder.Expr, len(values))
		for i, v := range values {
			params[i] = sqlbuilder.Lit(v)
		}
		return sqlbuilder.NotIn(col, params)
	case "set":
		return sqlbuilder.IsNotNull(col)
	case "notSet":
		return sqlbuilder.IsNull(col)
	}
	return sqlbuilder.Eq(col, col)
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
