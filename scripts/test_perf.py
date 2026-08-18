"""测试 cold/warm 性能"""
import json
import time
import urllib.request

body = {
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
req = urllib.request.Request(
    "http://localhost:8088/v1/load",
    data=json.dumps(body).encode("utf-8"),
    headers={"Content-Type": "application/json"},
    method="POST",
)
for i in range(3):
    start = time.time()
    with urllib.request.urlopen(req, timeout=120) as resp:
        r = json.loads(resp.read().decode("utf-8"))
        elapsed = time.time() - start
        d = r["data"]
        rollups = r.get("rollups", {})
        print(f"run {i+1}: {elapsed:.2f}s, daily={len(d['Rows'])}, "
              f"week={len(rollups.get('week', []))}, "
              f"month={len(rollups.get('month', []))}, "
              f"db={d.get('Stats', {}).get('DurationMs', 0)}ms")
