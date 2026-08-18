// Package rollup 提供时间维度的多粒度内存 rollup
//   目的:避免对同一查询做多次 DB round-trip 来获取不同粒度的数据
//   例:DB 查 day,Go 端 rollup 到 week/month/year,一次返回
//   减轻数据源 DB 运算压力(尤其是 SQL Server 2008 R2 这种老库)
package rollup

import (
	"fmt"
	"sort"
	"time"
)

// Row 一行数据(map:列名 → 值)
type Row = map[string]any

// Options rollup 配置
type Options struct {
	// TimeDimCol 时间维度列名(如 "sales.oper_date")
	TimeDimCol string
	// MeasureCols measure 列名列表(如 ["sales.total_revenue", "sales.count"])
	MeasureCols []string
	// SourceGranularity 源数据粒度(day / hour / week / month / quarter / year)
	// 必须是 time 维度的 granularity
	SourceGranularity string
	// TargetGranularities 目标粒度列表(week / month / quarter / year)
	// 只支持比 sourceGranularity 更粗的粒度
	TargetGranularities []string
}

// Result rollup 结果(每个目标粒度一份 row list)
type Result struct {
	// Rows 原始行(透传)
	Rows Row
	// ByGranularity map[granularity]rows
	ByGranularity map[string][]Row
}

// Rollup 主入口
//   inRows:DB 返回的原始行(粒度 = SourceGranularity)
//   opts:配置
// 返回:每个目标粒度的 rollup 行(measures 已聚合)
func Rollup(inRows []Row, opts Options) (map[string][]Row, error) {
	if opts.TimeDimCol == "" {
		return nil, fmt.Errorf("rollup: TimeDimCol required")
	}
	if len(opts.MeasureCols) == 0 {
		return nil, fmt.Errorf("rollup: MeasureCols required")
	}
	if opts.SourceGranularity == "" {
		return nil, fmt.Errorf("rollup: SourceGranularity required")
	}
	// 目标粒度必须比源粒度粗
	srcOrder := granOrder(opts.SourceGranularity)
	if srcOrder < 0 {
		return nil, fmt.Errorf("rollup: unknown source granularity %q", opts.SourceGranularity)
	}
	for _, tg := range opts.TargetGranularities {
		if granOrder(tg) <= srcOrder {
			return nil, fmt.Errorf("rollup: target granularity %q is not coarser than source %q", tg, opts.SourceGranularity)
		}
	}

	out := make(map[string][]Row, len(opts.TargetGranularities))
	for _, tg := range opts.TargetGranularities {
		out[tg] = rollupTo(inRows, opts.TimeDimCol, opts.MeasureCols, tg)
	}
	return out, nil
}

// rollupTo rollup 到指定粒度
func rollupTo(inRows []Row, timeCol string, measureCols []string, targetGran string) []Row {
	// 1. 找出非 measure、非 time 的列(其他维度,按其聚合)
	measureSet := make(map[string]bool, len(measureCols))
	for _, m := range measureCols {
		measureSet[m] = true
	}
	otherDims := []string{}
	for k := range inRows[0] {
		if k == timeCol {
			continue
		}
		if measureSet[k] {
			continue
		}
		otherDims = append(otherDims, k)
	}
	sort.Strings(otherDims) // 稳定顺序

	// 2. 分组聚合
	// key: bucket_time + "|" + otherDimValues
	type bucket struct {
		time     time.Time
		otherKey string
	}
	groups := make(map[bucket]*Row)
	order := []bucket{} // 保持插入顺序

	for _, row := range inRows {
		t, ok := toTime(row[timeCol])
		if !ok {
			continue
		}
		bucketed := truncateTo(t, targetGran)
		otherKey := ""
		for _, d := range otherDims {
			otherKey += fmt.Sprintf("%v|", row[d])
		}
		b := bucket{time: bucketed, otherKey: otherKey}
		if _, exists := groups[b]; !exists {
			// 初始化新行
			newRow := Row{}
			for _, d := range otherDims {
				newRow[d] = row[d]
			}
			// 初始化 measure 为零值
			for _, m := range measureCols {
				newRow[m] = zeroFor(m)
			}
			// 时间列(用 bucketed 值)
			newRow[timeCol] = formatTime(bucketed)
			groups[b] = &newRow
			order = append(order, b)
		}
		// 累加 measures
		for _, m := range measureCols {
			(*groups[b])[m] = addValues((*groups[b])[m], row[m])
		}
	}

	// 3. 排序并返回
	rows := make([]Row, 0, len(order))
	for _, b := range order {
		rows = append(rows, *groups[b])
	}
	// 按时间 + otherDims 排序
	sort.SliceStable(rows, func(i, j int) bool {
		ti, _ := toTime(rows[i][timeCol])
		tj, _ := toTime(rows[j][timeCol])
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		for _, d := range otherDims {
			if rows[i][d] != rows[j][d] {
				return fmt.Sprintf("%v", rows[i][d]) < fmt.Sprintf("%v", rows[j][d])
			}
		}
		return false
	})
	return rows
}

// truncateTo 把时间截断到目标粒度(返回时间桶的起点)
func truncateTo(t time.Time, gran string) time.Time {
	loc := t.Location()
	switch gran {
	case "hour":
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
	case "day":
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	case "week":
		// 周一为一周的开始(cube.js / SQL Server DATEPART wk 行为)
		offset := int(t.Weekday()) - 1
		if offset < 0 {
			offset = 6 // 周日
		}
		d := t.AddDate(0, 0, -offset)
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	case "month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
	case "quarter":
		qm := ((int(t.Month()) - 1) / 3) * 3 + 1
		return time.Date(t.Year(), time.Month(qm), 1, 0, 0, 0, 0, loc)
	case "year":
		return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, loc)
	}
	return t
}

// toTime 把任意时间值转成 time.Time
func toTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, true
	case string:
		// 尝试 ISO 8601 格式
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return t, true
		}
		if t, err := time.Parse("2006-01-02T15:04:05Z", x); err == nil {
			return t, true
		}
		if t, err := time.Parse("2006-01-02", x); err == nil {
			return t, true
		}
	case float64:
		// SQL Server 的 datetime 是从 1900-01-01 起的 days
		// driver 通常返回 string,这里兜底
		return time.Time{}, false
	}
	return time.Time{}, false
}

// formatTime 格式化时间回原格式(用 RFC3339 Z 形式)
func formatTime(t time.Time) any {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// zeroFor 不同 measure 的零值
func zeroFor(name string) any {
	// 简单处理:字符串用 "0",数字用 0
	// 实际更复杂的(如 DECIMAL)后续可以按列类型细化
	return "0"
}

// addValues 累加两个值(处理 string number 类型)
func addValues(a, b any) any {
	// 优先尝试数字
	fa, okA := toFloat(a)
	fb, okB := toFloat(b)
	if okA && okB {
		// 用高精度加法
		return formatFloat(fa + fb)
	}
	// 兜底:转字符串拼接
	return fmt.Sprintf("%v%v", a, b)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		// driver 返回的 DECIMAL 都是 string
		var f float64
		_, err := fmt.Sscanf(x, "%f", &f)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

func formatFloat(f float64) any {
	// 保留 4 位小数(与 SQL Server DECIMAL 一致)
	return fmt.Sprintf("%.4f", f)
}

// granOrder 粒度粗细排序(数字越大越粗)
func granOrder(g string) int {
	switch g {
	case "hour":
		return 0
	case "day":
		return 1
	case "week":
		return 2
	case "month":
		return 3
	case "quarter":
		return 4
	case "year":
		return 5
	}
	return -1
}
