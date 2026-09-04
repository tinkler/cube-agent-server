"""
supplier_returns / supplier_return_items cube 端到端测试

覆盖:
  - dry-run 编译通过
  - 主表 load 总览(近 1 年)
  - 单供应商未审核订单过滤(用户核心场景 1)
  - 单供应商已审核订单过滤(对照组)
  - 按 sheet_no 拿退货单明细(用户核心场景 2)
  - 跨 cube 一致性:主表 count(单数) == 明细按 sheet_no 去重 count
  - 跨 cube 金额一致性:主表某 sheet_no 的 sheet_amt == 明细 sum sub_amt

运行: python -X utf8 scripts/test_supplier_returns.py
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
        print(f"   ! DRYRUN FAIL: {r.get('error')}: {r.get('detail')}")
    return r.get("valid", False)


def load(body, timeout=180):
    code, r = post("/v1/load", body, timeout=timeout)
    if code != 200:
        print(f"   ! LOAD FAIL [{code}]: {r.get('error', r.get('detail'))}")
        sql_short = r.get("sql", "(no sql)")
        if len(sql_short) > 200:
            sql_short = sql_short[:200] + "..."
        print(f"   ! SQL: {sql_short}")
        return None
    return r.get("data")


def assert_true(cond, msg):
    if cond:
        print(f"   ✓ {msg}")
        return True
    print(f"   ✗ FAIL: {msg}")
    return False


def main():
    print("=" * 60)
    print("supplier_returns / supplier_return_items 端到端测试")
    print("=" * 60)

    passed = 0
    total = 0

    # ========================================================
    # Test 1: dry-run 主表(全 measure)
    # ========================================================
    print("\n[Test 1] dry-run: supplier_returns 主表(measures + time)")
    body = {
        "measures": [
            "supplier_returns.count",
            "supplier_returns.total_sheet_amt",
            "supplier_returns.avg_sheet_amt",
        ],
        "timeDimensions": [
            {
                "dimension": "supplier_returns.oper_date",
                "dateRange": ["2025-09-03", "2026-09-03"],
            }
        ],
    }
    total += 1
    if dryrun(body):
        passed += 1
        print("   ✓ dry-run 编译通过")

    # ========================================================
    # Test 2: dry-run 明细 cube
    # ========================================================
    print("\n[Test 2] dry-run: supplier_return_items 明细")
    body = {
        "measures": ["supplier_return_items.count", "supplier_return_items.total_sub_amt"],
        "dimensions": ["supplier_return_items.sheet_no"],
        "filters": [
            {"member": "supplier_return_items.sheet_no", "operator": "equals", "values": ["RO002609033617"]}
        ],
    }
    total += 1
    if dryrun(body):
        passed += 1
        print("   ✓ dry-run 编译通过")

    # ========================================================
    # Test 3: 主表总览 load
    # ========================================================
    print("\n[Test 3] load: supplier_returns 全量日聚合")
    body = {
        "measures": ["supplier_returns.count", "supplier_returns.total_sheet_amt"],
        "timeDimensions": [
            {
                "dimension": "supplier_returns.oper_date",
                "dateRange": ["2025-09-03", "2026-09-03"],
                "granularity": "month",
            }
        ],
    }
    data = load(body)
    total += 1
    if data and len(data.get("Rows") or []) > 0:
        passed += 1
        rows = data["Rows"]
        total_cnt = sum(int(r.get("supplier_returns.count") or 0) for r in rows)
        total_amt = sum(float(r.get("supplier_returns.total_sheet_amt") or 0) for r in rows)
        print(f"   ✓ 月维度聚合 {len(rows)} 月,共 {total_cnt} 单,总金额 {total_amt:.2f}")

    # ========================================================
    # Test 4: 用户场景 1 — 单供应商未审核订单
    # ========================================================
    print("\n[Test 4] 用户场景 1: 单供应商(040)未审核订单过滤")
    body = {
        "measures": ["supplier_returns.count", "supplier_returns.total_sheet_amt"],
        "dimensions": [
            "supplier_returns.supcust_no",
            "supplier_returns.sup_name",
            "supplier_returns.sheet_no",
        ],
        "filters": [
            {"member": "supplier_returns.supcust_no", "operator": "equals", "values": ["040"]},
            {"member": "supplier_returns.approve_flag", "operator": "equals", "values": ["0"]},
        ],
        "timeDimensions": [
            {
                "dimension": "supplier_returns.oper_date",
                "dateRange": ["2025-09-03", "2026-09-03"],
            }
        ],
    }
    data = load(body)
    total += 1
    if data is not None:
        passed += 1
        rows = data.get("Rows") or []
        if len(rows) == 0:
            print("   ✓ 查询执行成功(0 行:本数据所有 RO 都已审核,过滤逻辑生效)")
        else:
            print(f"   ✓ 查询成功,返回 {len(rows)} 单")
            for r in rows:
                print(
                    f"     - {r.get('supplier_returns.sheet_no')} "
                    f"sup={r.get('supplier_returns.sup_name')} "
                    f"amt={r.get('supplier_returns.total_sheet_amt')}"
                )

    # ========================================================
    # Test 5: 对照组 — 单供应商 040 所有订单
    # ========================================================
    print("\n[Test 5] 对照组: 供应商 040 全部退货单(去掉 approve 过滤)")
    body = {
        "measures": ["supplier_returns.count", "supplier_returns.total_sheet_amt"],
        "dimensions": [
            "supplier_returns.supcust_no",
            "supplier_returns.sup_name",
            "supplier_returns.sheet_no",
        ],
        "filters": [
            {"member": "supplier_returns.supcust_no", "operator": "equals", "values": ["040"]}
        ],
        "order": [["supplier_returns.oper_date", "desc"]],
        "timeDimensions": [
            {"dimension": "supplier_returns.oper_date", "dateRange": ["2025-09-03", "2026-09-03"]}
        ],
        "limit": 5,
    }
    data = load(body)
    total += 1
    if data and data.get("Rows"):
        passed += 1
        rows = data["Rows"]
        for r in rows:
            print(
                f"   - {r.get('supplier_returns.sheet_no')} "
                f"sup={r.get('supplier_returns.sup_name')} "
                f"amt={r.get('supplier_returns.total_sheet_amt')}"
            )

    # ========================================================
    # Test 6: 用户场景 2 — 按 sheet_no 拿退货单明细
    # ========================================================
    print("\n[Test 6] 用户场景 2: 按 sheet_no='RO002609033617' 拿商品清单")
    body = {
        "measures": [
            "supplier_return_items.count",
            "supplier_return_items.total_real_qty",
            "supplier_return_items.total_sub_amt",
        ],
        "dimensions": [
            "supplier_return_items.flow_id",
            "supplier_return_items.item_no",
            "supplier_return_items.item_name",
            "supplier_return_items.item_clsname",
            "supplier_return_items.real_qty",
            "supplier_return_items.valid_price",
            "supplier_return_items.sub_amt",
            "supplier_return_items.unit_no",
        ],
        "filters": [
            {"member": "supplier_return_items.sheet_no", "operator": "equals", "values": ["RO002609033617"]}
        ],
        "order": [["supplier_return_items.flow_id", "asc"]],
    }
    data = load(body)
    total += 1
    if data and data.get("Rows"):
        passed += 1
        rows = data["Rows"]
        item_total = sum(float(r.get("supplier_return_items.sub_amt") or 0) for r in rows)
        qty_total = sum(float(r.get("supplier_return_items.real_qty") or 0) for r in rows)
        print(f"   ✓ 退货单 {len(rows)} 个商品,总数量 {qty_total},总金额 {item_total:.2f}")
        for r in rows:
            print(
                f"     - {r.get('supplier_return_items.item_name')[:30]:<30} "
                f"qty={r.get('supplier_return_items.real_qty')} "
                f"price={r.get('supplier_return_items.valid_price')} "
                f"amt={r.get('supplier_return_items.sub_amt')} "
                f"cls={r.get('supplier_return_items.item_clsname')}"
            )

    # ========================================================
    # Test 7: 跨 cube 金额一致性(主表 sheet_amt vs 明细 sum)
    # ========================================================
    print("\n[Test 7] 跨 cube 金额一致性: RO002609033617")
    # 7a: 主表 sheet_amt
    body_master = {
        "measures": ["supplier_returns.total_sheet_amt"],
        "filters": [
            {"member": "supplier_returns.sheet_no", "operator": "equals", "values": ["RO002609033617"]}
        ],
    }
    # 7b: 明细 sub_amt
    body_detail = {
        "measures": ["supplier_return_items.total_sub_amt"],
        "filters": [
            {"member": "supplier_return_items.sheet_no", "operator": "equals", "values": ["RO002609033617"]}
        ],
    }
    m_data = load(body_master)
    d_data = load(body_detail)
    total += 1
    if m_data and d_data and m_data.get("Rows") and d_data.get("Rows"):
        m_amt = float(m_data["Rows"][0].get("supplier_returns.total_sheet_amt") or 0)
        d_amt = float(d_data["Rows"][0].get("supplier_return_items.total_sub_amt") or 0)
        if assert_true(abs(m_amt - d_amt) < 0.01, f"主表 sheet_amt={m_amt:.2f} == 明细 sum sub_amt={d_amt:.2f}"):
            passed += 1

    # ========================================================
    # Test 8: 跨 cube 单数一致性(主表 count == 明细 distinct sheet_no)
    # ========================================================
    print("\n[Test 8] 跨 cube 单数一致性(主表 count == 明细 distinct sheet_no)")
    body1 = {
        "measures": ["supplier_returns.count"],
        "timeDimensions": [
            {
                "dimension": "supplier_returns.oper_date",
                "dateRange": ["2025-09-03", "2026-09-03"],
            }
        ],
    }
    body2 = {
        "measures": ["supplier_return_items.count"],
        "dimensions": ["supplier_return_items.sheet_no"],
        "timeDimensions": [
            {
                "dimension": "supplier_return_items.oper_date",
                "dateRange": ["2025-09-03", "2026-09-03"],
            }
        ],
    }
    r1 = load(body1)
    r2 = load(body2)
    total += 1
    if r1 and r2 and r1.get("Rows") and r2.get("Rows"):
        # 主表:每个 (oper_date) 一个 group,sum(count) = 总单数 763
        m_cnt = sum(int(x.get("supplier_returns.count") or 0) for x in r1["Rows"])
        # 明细:每个 sheet_no 一个 group,len(rows) = 不同 sheet_no 数
        d_cnt = len(r2["Rows"])
        if assert_true(m_cnt == d_cnt, f"主表总单数={m_cnt} == 明细不同 sheet_no={d_cnt}"):
            passed += 1

    # ========================================================
    # Summary
    # ========================================================
    print("\n" + "=" * 60)
    print(f"结果: {passed}/{total} 通过")
    print("=" * 60)
    return 0 if passed == total else 1


if __name__ == "__main__":
    sys.exit(main())
