package rollup

import (
	"testing"
	"time"
)

func TestRollup_DayToMonth(t *testing.T) {
	in := []Row{
		{"sales.oper_date": "2026-08-01T00:00:00Z", "sales.total_revenue": "100.0000", "sales.count": "10"},
		{"sales.oper_date": "2026-08-15T00:00:00Z", "sales.total_revenue": "200.0000", "sales.count": "20"},
		{"sales.oper_date": "2026-08-20T00:00:00Z", "sales.total_revenue": "50.0000", "sales.count": "5"},
		{"sales.oper_date": "2026-09-05T00:00:00Z", "sales.total_revenue": "300.0000", "sales.count": "30"},
	}
	out, err := Rollup(in, Options{
		TimeDimCol:         "sales.oper_date",
		MeasureCols:        []string{"sales.total_revenue", "sales.count"},
		SourceGranularity:  "day",
		TargetGranularities: []string{"month"},
	})
	if err != nil {
		t.Fatal(err)
	}
	monthly := out["month"]
	if len(monthly) != 2 {
		t.Fatalf("expected 2 months, got %d", len(monthly))
	}
	// 8 月:350 元 35 笔
	// 9 月:300 元 30 笔
	if monthly[0]["sales.total_revenue"] != "350.0000" {
		t.Errorf("8月 revenue = %v, want 350.0000", monthly[0]["sales.total_revenue"])
	}
	if monthly[0]["sales.count"] != "35.0000" {
		t.Errorf("8月 count = %v, want 35.0000", monthly[0]["sales.count"])
	}
	if monthly[1]["sales.total_revenue"] != "300.0000" {
		t.Errorf("9月 revenue = %v, want 300.0000", monthly[1]["sales.total_revenue"])
	}
}

func TestRollup_DayToWeekAndMonth(t *testing.T) {
	in := []Row{
		{"sales.oper_date": "2026-08-03T00:00:00Z", "sales.total_revenue": "100.0000"},
		{"sales.oper_date": "2026-08-04T00:00:00Z", "sales.total_revenue": "200.0000"},
		{"sales.oper_date": "2026-08-15T00:00:00Z", "sales.total_revenue": "300.0000"},
		{"sales.oper_date": "2026-08-16T00:00:00Z", "sales.total_revenue": "400.0000"},
	}
	out, err := Rollup(in, Options{
		TimeDimCol:         "sales.oper_date",
		MeasureCols:        []string{"sales.total_revenue"},
		SourceGranularity:  "day",
		TargetGranularities: []string{"week", "month"},
	})
	if err != nil {
		t.Fatal(err)
	}

	weekly := out["week"]
	// 8/3 周一, 8/4 周二 → 同周(8/3-8/9), 合计 300
	// 8/15 周六, 8/16 周日 → 同一周(8/17-8/23, ISO 周一为周首)
	//   8/15 Sat 的 ISO 周:往前数 5 天到 8/10 Mon
	if len(weekly) != 2 {
		t.Errorf("expected 2 weeks, got %d", len(weekly))
	}

	monthly := out["month"]
	if len(monthly) != 1 {
		t.Errorf("expected 1 month, got %d", len(monthly))
	}
	if monthly[0]["sales.total_revenue"] != "1000.0000" {
		t.Errorf("8月 total = %v, want 1000.0000", monthly[0]["sales.total_revenue"])
	}
}

func TestRollup_GroupByOtherDim(t *testing.T) {
	in := []Row{
		{"sales.oper_date": "2026-08-01T00:00:00Z", "sales.item_no": "A", "sales.total_revenue": "100.0000"},
		{"sales.oper_date": "2026-08-15T00:00:00Z", "sales.item_no": "A", "sales.total_revenue": "200.0000"},
		{"sales.oper_date": "2026-08-15T00:00:00Z", "sales.item_no": "B", "sales.total_revenue": "50.0000"},
		{"sales.oper_date": "2026-09-01T00:00:00Z", "sales.item_no": "A", "sales.total_revenue": "300.0000"},
	}
	out, err := Rollup(in, Options{
		TimeDimCol:         "sales.oper_date",
		MeasureCols:        []string{"sales.total_revenue"},
		SourceGranularity:  "day",
		TargetGranularities: []string{"month"},
	})
	if err != nil {
		t.Fatal(err)
	}
	monthly := out["month"]
	// 期望 3 行:(8月, A)=300, (8月, B)=50, (9月, A)=300
	if len(monthly) != 3 {
		t.Errorf("expected 3 monthly rows, got %d: %+v", len(monthly), monthly)
	}
}

func TestRollup_RejectsCoarserSource(t *testing.T) {
	// month → day 不允许
	_, err := Rollup([]Row{}, Options{
		TimeDimCol:         "x",
		MeasureCols:        []string{"m"},
		SourceGranularity:  "month",
		TargetGranularities: []string{"day"},
	})
	if err == nil {
		t.Error("expected error for coarser source")
	}
}

func TestGranOrder(t *testing.T) {
	if granOrder("day") >= granOrder("week") {
		t.Error("day should be < week")
	}
	if granOrder("year") <= granOrder("month") {
		t.Error("year should be > month")
	}
}

func TestTruncateTo(t *testing.T) {
	t1 := time.Date(2026, 8, 15, 14, 30, 45, 0, time.UTC)
	if got := truncateTo(t1, "day"); got != time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC) {
		t.Errorf("day = %v", got)
	}
	if got := truncateTo(t1, "month"); got != time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("month = %v", got)
	}
	if got := truncateTo(t1, "year"); got != time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("year = %v", got)
	}
	// quarter: 8月 → 7月1日
	if got := truncateTo(t1, "quarter"); got != time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("quarter = %v", got)
	}
	// 8/15 是周六, ISO 周:周一 8/10
	if got := truncateTo(t1, "week"); got != time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC) {
		t.Errorf("week(8/15 Sat) = %v, want 8/10 Mon", got)
	}
}
