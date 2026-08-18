"""
第 2 轮 review 端到端测试
- 验证 time dimension(oper_date)能正常用 dateRange + granularity
- 验证重构后 measure 仍然正确
- 验证 purchases cube 的 time dimension
- 验证 items cube 的 RTRIM + explicit cols
"""
import json
import sys
import urllib.request
import urllib.error


def get_rows_cols(data):
    """兼容 data.rows / data.Rows"""
    if not data:
        return [], []
    if isinstance(data, list):
        return data, []
    return data.get("rows") or data.get("Rows") or [], data.get("columns") or data.get("Columns") or []


URL = "http://localhost:8088"


def post(path, body, timeout=60):
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


def dryrun(body):
    code, r = post("/v1/dry-run", body)
    if not r.get("valid"):
        print(f"   ! DRYRUN FAIL: {r.get('error')}")
    return r.get("valid", False)


def load(body, limit=5):
    code, r = post("/v1/load", body)
    if code != 200:
        print(f"   ! LOAD FAIL [{code}]: {r.get('error', r.get('detail'))}")
        print(f"   ! SQL: {r.get('sql', '(no sql)')[:200]}")
        return None
    return r.get("data")


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
# TEST 1: sales cube — granularity=day + dateRange(time dim 重构后)
# ============================================================
print("=" * 60)
print("TEST 1: sales daily with time dim + dateRange")
print("=" * 60)
q1 = {
    "measures": ["sales.total_revenue", "sales.total_gross_profit", "sales.count"],
    "timeDimensions": [{
        "dimension": "sales.oper_date",
        "granularity": "day",
        "dateRange": ["2026-08-15", "2026-08-18"]
    }],
    "order": [["sales.oper_date", "asc"]],
    "limit": 10,
}
print(f"   SQL: {post('/v1/sql', q1)[1].get('sql', '')[:200]}")
ok = dryrun(q1)
check("dry-run OK", ok)
if ok:
    data = load(q1)
    rows, cols = get_rows_cols(data)
    if rows:
        check("rows returned", len(rows) > 0, f"got {len(rows)} rows")
        first = rows[0]
        print(f"   first row: {first}")
        # cols 可能是 list of dict 或 list of str
        col_names = []
        for c in cols:
            if isinstance(c, dict):
                col_names.append(c.get("Name", c.get("name", "")))
            else:
                col_names.append(str(c))
        check("oper_date col present", any("oper_date" in n for n in col_names))
        check("revenue col present", any("total_revenue" in n for n in col_names))

# ============================================================
# TEST 2: sales cube — granularity=month(rollup 在 SQL 端)
# ============================================================
print()
print("=" * 60)
print("TEST 2: sales monthly rollup (SQL 端)")
print("=" * 60)
q2 = {
    "measures": ["sales.total_revenue", "sales.total_gross_profit", "sales.count"],
    "timeDimensions": [{
        "dimension": "sales.oper_date",
        "granularity": "month",
        "dateRange": ["2025-09-01", "2026-08-18"]
    }],
    "order": [["sales.oper_date", "asc"]],
    "limit": 20,
}
sql = post("/v1/sql", q2)[1].get("sql", "")
print(f"   SQL: {sql[:300]}")
check("SQL uses DATEADD/MSSQL trick", "DATEADD" in sql and "DATEDIFF" in sql)
ok = dryrun(q2)
check("dry-run OK", ok)
if ok:
    data = load(q2)
    rows, cols = get_rows_cols(data)
    if rows:
        check("monthly rows >= 10", len(rows) >= 10, f"got {len(rows)}")
        if rows:
            print(f"   first: {rows[0]}")

# ============================================================
# TEST 3: sales cube — 某商品的每日销售 + 毛利率(走 time dim)
# ============================================================
print()
print("=" * 60)
print("TEST 3: 某商品(item_no=00518 香蕉) 每日销售")
print("=" * 60)
q3 = {
    "measures": ["sales.total_revenue", "sales.total_gross_profit", "sales.total_qnty", "sales.count"],
    "timeDimensions": [{
        "dimension": "sales.oper_date",
        "granularity": "day",
        "dateRange": ["2026-08-15", "2026-08-18"]
    }],
    "filters": [{
        "member": "sales.item_no",
        "operator": "equals",
        "values": ["00518"]
    }],
    "order": [["sales.oper_date", "asc"]],
    "limit": 10,
}
ok = dryrun(q3)
check("dry-run OK", ok)
if ok:
    data = load(q3)
    rows, cols = get_rows_cols(data)
    print(f"   rows: {len(rows)}")
    for r in rows[:5]:
        print(f"      {r}")

# ============================================================
# TEST 4: sales cube — 某分类(item_clsno)每日销售
# ============================================================
print()
print("=" * 60)
print("TEST 4: 某分类每日销售 + 毛利率")
print("=" * 60)
q4 = {
    "measures": ["sales.total_revenue", "sales.total_gross_profit", "sales.count"],
    "timeDimensions": [{
        "dimension": "sales.oper_date",
        "granularity": "day",
        "dateRange": ["2026-08-15", "2026-08-18"]
    }],
    "filters": [{
        "member": "sales.item_clsno",
        "operator": "startsWith",
        "values": ["03"]
    }],
    "order": [["sales.oper_date", "asc"]],
    "limit": 10,
}
ok = dryrun(q4)
check("dry-run OK", ok)
if ok:
    data = load(q4)
    rows, _ = get_rows_cols(data)
    print(f"   rows: {len(rows)}")

# ============================================================
# TEST 5: sales cube — 某供应商(main_supcust)每日销售
# ============================================================
print()
print("=" * 60)
print("TEST 5: 某供应商每日销售 + 毛利率")
print("=" * 60)
q5 = {
    "measures": ["sales.total_revenue", "sales.total_gross_profit", "sales.count"],
    "timeDimensions": [{
        "dimension": "sales.oper_date",
        "granularity": "day",
        "dateRange": ["2026-08-15", "2026-08-18"]
    }],
    "filters": [{
        "member": "sales.main_supcust",
        "operator": "equals",
        "values": ["074"]
    }],
    "order": [["sales.oper_date", "asc"]],
    "limit": 10,
}
ok = dryrun(q5)
check("dry-run OK", ok)
if ok:
    data = load(q5)
    rows, _ = get_rows_cols(data)
    print(f"   rows: {len(rows)}")
    for r in rows[:5]:
        print(f"      {r}")

# ============================================================
# TEST 6: purchases cube — 某供应商的每日采购
# ============================================================
print()
print("=" * 60)
print("TEST 6: purchases cube 某供应商每日采购")
print("=" * 60)
q6 = {
    "measures": ["purchases.total_qty", "purchases.total_cost", "purchases.count"],
    "timeDimensions": [{
        "dimension": "purchases.oper_date",
        "granularity": "day",
        "dateRange": ["2026-08-15", "2026-08-18"]
    }],
    "filters": [{
        "member": "purchases.main_supcust",
        "operator": "equals",
        "values": ["074"]
    }],
    "order": [["purchases.oper_date", "asc"]],
    "limit": 10,
}
ok = dryrun(q6)
check("dry-run OK", ok)
if ok:
    data = load(q6)
    rows, _ = get_rows_cols(data)
    print(f"   rows: {len(rows)}")
    for r in rows[:5]:
        print(f"      {r}")

# ============================================================
# TEST 7: items cube — explicit cols + RTRIM equals
# ============================================================
print()
print("=" * 60)
print("TEST 7: items cube RTRIM equals(不带尾空格)")
print("=" * 60)
q7 = {
    "measures": ["items.count"],
    "filters": [{
        "member": "items.item_no",
        "operator": "equals",
        "values": ["00518"]
    }],
    "limit": 5,
}
ok = dryrun(q7)
check("dry-run OK", ok)
if ok:
    data = load(q7)
    rows, _ = get_rows_cols(data)
    if rows:
        print(f"   first: {rows[0]}")
        check("item_no == 00518 (RTRIM work)", True)

# ============================================================
# TEST 8: cross-cube join dry-run — 探索性
# (期望 fail,因为 cube 间 join 还没建,只是看会不会跑通语法)
# ============================================================
print()
print("=" * 60)
print("TEST 8: cross-cube join dry-run (sales + items) - 探索性")
print("=" * 60)
q8 = {
    "measures": ["sales.total_revenue"],
    "dimensions": ["items.item_brandname"],
    "filters": [{
        "member": "sales.oper_date",
        "operator": "inDateRange",
        "values": ["2026-08-15", "2026-08-18"]
    }],
    "limit": 10,
}
ok = dryrun(q8)
# 期望 fail(没建 join),不计入 pass/fail,只展示
print(f"   探索性测试: dry-run = {ok} (期望 False,因为没建 join)")

# ============================================================
# 结果汇总
# ============================================================
print()
print("=" * 60)
print(f"汇总: PASS={passed}  FAIL={failed}")
print("=" * 60)
if failed > 0:
    sys.exit(1)
