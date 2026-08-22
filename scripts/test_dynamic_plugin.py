"""
supplier_sales cube 动态 plugin 端到端测试
- 快路径:不查 item 维度 → 0 JOIN
- 慢路径:查 item_* 或 main_supcust → 2 JOIN
- 慢路径 + 聚合:用 total_revenue 等 measure → 2 JOIN + SUM
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


def run_query(label, body, expect_joins_min, expect_joins_max):
    """跑 query,检查 JOIN 数量和数据"""
    print(f"\n{'=' * 60}\n{label}\n{'=' * 60}")
    code, r = post("/v1/load", body)
    if code != 200:
        print(f"   FAIL  HTTP {code}: {r.get('detail', '')[:200]}")
        return False
    sql = r.get("sql", "")
    join_count = sql.upper().count("LEFT JOIN") + sql.upper().count("INNER JOIN") + sql.upper().count("RIGHT JOIN")
    print(f"   JOIN 数: {join_count} (期望 {expect_joins_min}-{expect_joins_max})")
    print(f"   SQL: {sql[:250]}...")
    check("JOIN 数量在范围内", expect_joins_min <= join_count <= expect_joins_max)
    rows = (r.get("data") or {}).get("Rows") or []
    check("有数据", len(rows) > 0, f"rows={len(rows)}")
    if rows:
        print(f"   {len(rows)} 行, 首行: {rows[0]}")
    return True


# ============================================================
# 场景 1:快路径 - 只查 branch_no(不查 item 维度)
#   期望:0 JOIN(plugin 不需要 item 数据)
# ============================================================
run_query(
    "场景 1:快路径 (只查 branch_no,无 item 维度)",
    {
        "measures": ["supplier_sales.count"],
        "dimensions": ["supplier_sales.branch_no"],
        "timeDimensions": [{"dimension": "supplier_sales.oper_date", "dateRange": ["2026-08-15", "2026-08-18"]}],
        "limit": 3,
    },
    expect_joins_min=0, expect_joins_max=0
)

# ============================================================
# 场景 2:慢路径 - 查 main_supcust(供应商)
#   期望:2 JOIN(plugin 需要 item_info 的 main_supcust)
# ============================================================
run_query(
    "场景 2:慢路径 (查 main_supcust → JOIN item_info)",
    {
        "measures": ["supplier_sales.count", "supplier_sales.total_revenue"],
        "dimensions": ["supplier_sales.main_supcust"],
        "timeDimensions": [{"dimension": "supplier_sales.oper_date", "dateRange": ["2026-08-15", "2026-08-18"]}],
        "limit": 3,
    },
    expect_joins_min=2, expect_joins_max=2
)

# ============================================================
# 场景 3:慢路径 - 查 item_brand
#   期望:2 JOIN
# ============================================================
run_query(
    "场景 3:慢路径 (查 item_brand → JOIN item_info)",
    {
        "measures": ["supplier_sales.count", "supplier_sales.total_revenue"],
        "dimensions": ["supplier_sales.item_brand"],
        "timeDimensions": [{"dimension": "supplier_sales.oper_date", "dateRange": ["2026-08-15", "2026-08-18"]}],
        "limit": 3,
    },
    expect_joins_min=2, expect_joins_max=2
)

# ============================================================
# 场景 4:慢路径 - 查 item_clsname(分类名)
#   期望:2 JOIN(t_bd_item_cls 是必要的)
# ============================================================
run_query(
    "场景 4:慢路径 (查 item_clsname → JOIN item_cls)",
    {
        "measures": ["supplier_sales.total_revenue"],
        "dimensions": ["supplier_sales.item_clsname"],
        "timeDimensions": [{"dimension": "supplier_sales.oper_date", "dateRange": ["2026-08-17", "2026-08-18"]}],
        "limit": 3,
    },
    expect_joins_min=2, expect_joins_max=2
)

# ============================================================
# 场景 5:同 cube 无 dynamic_plugin 的(对比 static YAML 路径)
# ============================================================
print(f"\n{'=' * 60}\n场景 5:对比 - sales cube(无 dynamic_plugin,纯 static SQL)\n{'=' * 60}")
code, r = post("/v1/load", {
    "measures": ["sales.count"],
    "dimensions": ["sales.branch_no"],
    "timeDimensions": [{"dimension": "sales.oper_date", "dateRange": ["2026-08-15", "2026-08-18"]}],
    "limit": 3,
})
if code == 200:
    sql = r.get("sql", "")
    print(f"   SQL: {sql[:200]}...")
    print(f"   rows: {len((r.get('data') or {}).get('Rows', []))}")
    check("sales cube 走 static SQL 路径", "t_rm_saleflow" in sql or "DATEADD" in sql)
else:
    print(f"   FAIL: {r}")

# 汇总
print(f"\n{'=' * 60}\n汇总: PASS={passed}  FAIL={failed}\n{'=' * 60}")
if failed > 0:
    sys.exit(1)
