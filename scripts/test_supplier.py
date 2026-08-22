"""
supplier cube 端到端测试
- 供应商基本信息(suppliers cube)
- 供应商供应商品(supplier_items cube)
- 供应商日销量(supplier_sales cube)
"""
import json
import sys
import urllib.request
import urllib.error


URL = "http://localhost:8088"


def post(path, body, timeout=180):
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
    except Exception as ex:
        return 0, {"error": str(ex)}


def dryrun(body):
    code, r = post("/v1/dry-run", body)
    if not r.get("valid"):
        print(f"   ! DRYRUN FAIL: {r.get('error')}")
    return r.get("valid", False)


def load(body):
    code, r = post("/v1/load", body)
    if code != 200:
        print(f"   ! LOAD FAIL [{code}]: {r.get('error', r.get('detail'))}")
        sql_short = r.get('sql', '(no sql)')
        if len(sql_short) > 200:
            sql_short = sql_short[:200] + '...'
        print(f"   ! SQL: {sql_short}")
        return None
    return r.get("data")


def get_rows_cols(data):
    if not data:
        return [], []
    if isinstance(data, list):
        return data, []
    return (data.get("rows") or data.get("Rows") or []), (data.get("columns") or data.get("Columns") or [])


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
# TEST 1: suppliers cube — 供应商总数
# ============================================================
print("=" * 60)
print("TEST 1: suppliers cube 供应商总数")
print("=" * 60)
q1 = {
    "measures": ["suppliers.count"],
    "limit": 5,
}
ok = dryrun(q1)
check("dry-run OK", ok)
if ok:
    data = load(q1)
    if data:
        rows, _ = get_rows_cols(data)
        if rows:
            cnt = rows[0].get("suppliers.count", 0)
            try:
                cnt = int(cnt)
            except (TypeError, ValueError):
                cnt = 0
            print(f"   供应商总数: {cnt}")
            check("208 家供应商", cnt == 208, f"got {cnt}")

# ============================================================
# TEST 2: suppliers cube — 某供应商(074)基本信息
# ============================================================
print()
print("=" * 60)
print("TEST 2: suppliers cube 某供应商基本信息")
print("=" * 60)
q2 = {
    "measures": ["suppliers.count"],
    "dimensions": ["suppliers.sup_name", "suppliers.sup_man", "suppliers.sup_tel", "suppliers.sup_addr"],
    "filters": [{
        "member": "suppliers.supcust_no",
        "operator": "equals",
        "values": ["074"]
    }],
    "limit": 5,
}
ok = dryrun(q2)
check("dry-run OK", ok)
if ok:
    data = load(q2)
    if data:
        rows, cols = get_rows_cols(data)
        print(f"   rows: {len(rows)}")
        for r in rows[:3]:
            print(f"      {r}")
        check("rows == 1", len(rows) == 1, f"got {len(rows)}")
        if rows:
            check("sup_name 有值", bool(rows[0].get("suppliers.sup_name")))

# ============================================================
# TEST 3: supplier_items cube — 某供应商供的商品数
# ============================================================
print()
print("=" * 60)
print("TEST 3: supplier_items cube 某供应商供的商品数")
print("=" * 60)
q3 = {
    "measures": ["supplier_items.count"],
    "filters": [{
        "member": "supplier_items.supcust_no",
        "operator": "equals",
        "values": ["074"]
    }],
    "limit": 5,
}
ok = dryrun(q3)
check("dry-run OK", ok)
if ok:
    data = load(q3)
    if data:
        rows, _ = get_rows_cols(data)
        if rows:
            cnt = rows[0].get("supplier_items.count", 0)
            try:
                cnt = int(cnt)
            except (TypeError, ValueError):
                cnt = 0
            print(f"   074 供应商供应商品数: {cnt}")
            check("供应商品数 > 0", cnt > 0, f"got {cnt}")

# ============================================================
# TEST 4: supplier_items cube — 某供应商商品列表
# ============================================================
print()
print("=" * 60)
print("TEST 4: supplier_items cube 某供应商商品列表")
print("=" * 60)
q4 = {
    "measures": ["supplier_items.count", "supplier_items.avg_top_price"],
    "dimensions": ["supplier_items.item_no", "supplier_items.item_name", "supplier_items.item_clsname", "supplier_items.last_price"],
    "filters": [{
        "member": "supplier_items.supcust_no",
        "operator": "equals",
        "values": ["074"]
    }],
    "order": [["supplier_items.last_price", "desc"]],
    "limit": 5,
}
ok = dryrun(q4)
check("dry-run OK", ok)
if ok:
    data = load(q4)
    if data:
        rows, _ = get_rows_cols(data)
        print(f"   rows: {len(rows)}")
        for r in rows[:5]:
            print(f"      {r}")
        check("rows > 0", len(rows) > 0)

# ============================================================
# TEST 5: supplier_sales cube — 某供应商日销量
# ============================================================
print()
print("=" * 60)
print("TEST 5: supplier_sales cube 某供应商日销量")
print("=" * 60)
q5 = {
    "measures": ["supplier_sales.total_revenue", "supplier_sales.total_qnty", "supplier_sales.count", "supplier_sales.total_gross_profit"],
    "timeDimensions": [{
        "dimension": "supplier_sales.oper_date",
        "granularity": "day",
        "dateRange": ["2026-08-15", "2026-08-18"]
    }],
    "filters": [{
        "member": "supplier_sales.main_supcust",
        "operator": "equals",
        "values": ["074"]
    }],
    "order": [["supplier_sales.oper_date", "asc"]],
    "limit": 10,
}
ok = dryrun(q5)
check("dry-run OK", ok)
if ok:
    data = load(q5)
    if data:
        rows, _ = get_rows_cols(data)
        print(f"   rows: {len(rows)}")
        for r in rows[:5]:
            print(f"      {r}")
        check("rows >= 1", len(rows) >= 1, f"got {len(rows)}")

# ============================================================
# TEST 6: supplier_sales cube — 某供应商月销量 + 毛利率
# ============================================================
print()
print("=" * 60)
print("TEST 6: supplier_sales cube 某供应商月销量 + 毛利率")
print("=" * 60)
q6 = {
    "measures": ["supplier_sales.total_revenue", "supplier_sales.total_gross_profit", "supplier_sales.count"],
    "timeDimensions": [{
        "dimension": "supplier_sales.oper_date",
        "granularity": "month",
        "dateRange": ["2026-01-01", "2026-08-18"]
    }],
    "filters": [{
        "member": "supplier_sales.main_supcust",
        "operator": "equals",
        "values": ["074"]
    }],
    "order": [["supplier_sales.oper_date", "asc"]],
    "limit": 20,
}
ok = dryrun(q6)
check("dry-run OK", ok)
if ok:
    data = load(q6)
    if data:
        rows, _ = get_rows_cols(data)
        print(f"   月份数: {len(rows)}")
        for r in rows[:5]:
            print(f"      {r}")
        check("rows > 0", len(rows) > 0)

# ============================================================
# TEST 7: supplier_sales cube — TOP 10 供应商某月销量
# ============================================================
print()
print("=" * 60)
print("TEST 7: supplier_sales cube TOP 10 供应商某月销量")
print("=" * 60)
q7 = {
    "measures": ["supplier_sales.total_revenue", "supplier_sales.count"],
    "dimensions": ["supplier_sales.main_supcust"],
    "timeDimensions": [{
        "dimension": "supplier_sales.oper_date",
        "dateRange": ["2026-08-01", "2026-08-18"]
    }],
    "order": [["supplier_sales.total_revenue", "desc"]],
    "limit": 10,
}
ok = dryrun(q7)
check("dry-run OK", ok)
if ok:
    data = load(q7)
    if data:
        rows, _ = get_rows_cols(data)
        print(f"   rows: {len(rows)}")
        for r in rows[:5]:
            print(f"      {r}")
        check("rows > 0", len(rows) > 0)

# ============================================================
# TEST 8: 某分类某供应商的销量(组合 dim)
# ============================================================
print()
print("=" * 60)
print("TEST 8: supplier_sales cube 分类+供应商组合")
print("=" * 60)
q8 = {
    "measures": ["supplier_sales.total_revenue", "supplier_sales.count"],
    "dimensions": ["supplier_sales.item_clsname", "supplier_sales.main_supcust"],
    "timeDimensions": [{
        "dimension": "supplier_sales.oper_date",
        "dateRange": ["2026-08-15", "2026-08-18"]
    }],
    "filters": [{
        "member": "supplier_sales.item_clsno",
        "operator": "startsWith",
        "values": ["03"]
    }],
    "order": [["supplier_sales.total_revenue", "desc"]],
    "limit": 10,
}
ok = dryrun(q8)
check("dry-run OK", ok)
if ok:
    data = load(q8)
    if data:
        rows, _ = get_rows_cols(data)
        print(f"   rows: {len(rows)}")
        for r in rows[:5]:
            print(f"      {r}")
        check("rows > 0", len(rows) > 0)

# ============================================================
# 结果汇总
# ============================================================
print()
print("=" * 60)
print(f"汇总: PASS={passed}  FAIL={failed}")
print("=" * 60)
if failed > 0:
    sys.exit(1)
