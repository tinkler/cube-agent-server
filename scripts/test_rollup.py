"""
timeRollup 端到端测试
- 验证 query timeRollup 字段解析
- 验证 Go 端多粒度 rollup 正确性
- 验证减轻 DB 压力的目标(单次查询返回 daily + weekly + monthly)
"""
import json
import sys
import urllib.request
import urllib.error


URL = "http://localhost:8088"


def post(path, body, timeout=120):
    req = urllib.request.Request(
        f"{URL}{path}",
        data=json.dumps(body).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read().decode("utf-8"))


passed = 0
failed = 0


def check(name, cond, detail=""):
    global passed, failed
    if cond:
        passed += 1
        print(f"   PASS  {name}")
    else:
        failed += 1
        print(f"   FAIL  {name}  {detail}")


# ============================================================
# TEST 1: timeRollup 字段被识别
# ============================================================
print("=" * 60)
print("TEST 1: timeRollup 字段识别(单次查 day+week+month)")
print("=" * 60)
q1 = {
    "measures": ["sales.total_revenue", "sales.count"],
    "timeDimensions": [{
        "dimension": "sales.oper_date",
        "granularity": "day",
        "dateRange": ["2026-08-10", "2026-08-18"]
    }],
    "timeRollup": ["week", "month"],
    "order": [["sales.oper_date", "asc"]],
    "limit": 100,
}
code, r = post("/v1/load", q1)
check("HTTP 200", code == 200, f"got {code}: {r.get('error', r.get('detail'))}")
if code == 200:
    data = r.get("data", {})
    daily_rows = data.get("Rows") or data.get("rows") or []
    rollups = r.get("rollups", {})
    print(f"   daily rows: {len(daily_rows)}")
    print(f"   rollups keys: {list(rollups.keys())}")
    check("daily rows >= 7", len(daily_rows) >= 7, f"got {len(daily_rows)}")
    check("rollups.week present", "week" in rollups)
    check("rollups.month present", "month" in rollups)
    if "week" in rollups:
        week_rows = rollups["week"]
        print(f"   week rows: {len(week_rows)}")
        for w in week_rows[:5]:
            print(f"      {w}")
        check("week rows >= 1", len(week_rows) >= 1)
    if "month" in rollups:
        month_rows = rollups["month"]
        print(f"   month rows: {len(month_rows)}")
        for m in month_rows[:5]:
            print(f"      {m}")
        check("month rows = 1 (跨 8 月)", len(month_rows) == 1)
        if month_rows:
            # 8/10-8/18 共 9 天,8 月全月是 705 万(按 70 万 1 年平均)
            # 但 rollup 只取 query 范围内的数据,所以是 8/10-8/18 的 sum
            total = float(month_rows[0].get("sales.total_revenue", 0))
            daily_total = sum(float(r.get("sales.total_revenue", 0)) for r in daily_rows)
            print(f"   month total = {total}, daily sum = {daily_total}")
            check("month total == sum of daily", abs(total - daily_total) < 1.0,
                  f"diff = {abs(total - daily_total)}")

# ============================================================
# TEST 2: timeRollup 不被接受 coarser-than-source
# ============================================================
print()
print("=" * 60)
print("TEST 2: timeRollup 错误检查(粒度比源粗才行)")
print("=" * 60)
# month → day 不行(源是 month,目标 day 比 month 细)
q2 = {
    "measures": ["sales.total_revenue"],
    "timeDimensions": [{
        "dimension": "sales.oper_date",
        "granularity": "month",
        "dateRange": ["2025-09-01", "2026-08-18"]
    }],
    "timeRollup": ["day"],  # 错的:day 比 month 细
    "limit": 100,
}
code, r = post("/v1/load", q2)
if code == 200:
    if r.get("rollup_warning"):
        print(f"   警告: {r['rollup_warning']}")
        check("rollup warning returned", "coarser" in r.get("rollup_warning", ""))
    else:
        check("rollup rejected", False, "should reject day<month")
else:
    check("HTTP 500/400 (拒绝)", code in (400, 500), f"got {code}")

# ============================================================
# TEST 3: 没 timeRollup 时响应无 rollups 字段
# ============================================================
print()
print("=" * 60)
print("TEST 3: 无 timeRollup → 响应无 rollups 字段")
print("=" * 60)
q3 = {
    "measures": ["sales.total_revenue"],
    "timeDimensions": [{
        "dimension": "sales.oper_date",
        "granularity": "day",
        "dateRange": ["2026-08-15", "2026-08-18"]
    }],
    "limit": 10,
}
code, r = post("/v1/load", q3)
check("HTTP 200", code == 200)
if code == 200:
    check("no rollups field", "rollups" not in r)
    check("data has Rows", "Rows" in r.get("data", {}) or "rows" in r.get("data", {}))

# ============================================================
# TEST 4: timeRollup 配合其他维度(item_no)
# ============================================================
print()
print("=" * 60)
print("TEST 4: timeRollup + 其他维度(按商品分组)")
print("=" * 60)
q4 = {
    "measures": ["sales.total_revenue", "sales.count"],
    "dimensions": ["sales.item_no"],
    "timeDimensions": [{
        "dimension": "sales.oper_date",
        "granularity": "day",
        "dateRange": ["2026-08-15", "2026-08-18"]
    }],
    "timeRollup": ["week"],
    "order": [["sales.total_revenue", "desc"]],
    "limit": 5,  # 限制前 5 商品
}
code, r = post("/v1/load", q4)
check("HTTP 200", code == 200, f"got {code}")
if code == 200:
    daily_rows = (r.get("data") or {}).get("Rows") or []
    rollups = r.get("rollups", {})
    print(f"   daily rows: {len(daily_rows)} (限 5 商品 × 4 天)")
    if "week" in rollups:
        week_rows = rollups["week"]
        print(f"   week rows: {len(week_rows)} (5 商品 × 1 周)")
        for w in week_rows[:3]:
            print(f"      {w}")
        check("week rows > 0", len(week_rows) > 0)
        # 验证:weekly revenue >= 任一 daily revenue for same item
        # (聚合应该更大或相等)

# ============================================================
# 汇总
# ============================================================
print()
print("=" * 60)
print(f"汇总: PASS={passed}  FAIL={failed}")
print("=" * 60)
if failed > 0:
    sys.exit(1)
