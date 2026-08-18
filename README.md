# Cube Agent Server

数据治理层 Agent 服务 —— 阉割版 Cube Core 兼容,支持数据模型 Plugin 热加载。

## 特性

- 🔥 **热加载**:改 plugin.yaml 自动生效,无需重启(fsnotify 递归 watch)
- 📦 **Plugin 化**:数据模型即 YAML,放在 `plugins/<name>/plugin.yaml` 即可
- 🛡️ **原子切换**:Registry 用 `atomic.Pointer` 写时复制,运行中查询零阻塞
- 🔌 **Cube.js 兼容**:`/v1/meta` 返回格式对齐 cube.js,Superset/Metabase 可直接接
- 🇨🇳 **SQL Server 2008 R2 支持**:内置兼容层(ROW_NUMBER 分页、禁用 STRING_AGG)
- 🧠 **3-Pass 编译器**:JSON Query → IR → SQL,支持 measure/dimension/segment/time-dimension/filter/order/limit
- 🗄️ **多数据源**:SQLite/PostgreSQL/MySQL/ClickHouse/SQL Server 2008 R2/CSV 已接入
- 📊 **Per-cube Dialect**:根据 cube 的 datasource 自动选方言渲染 SQL
- 📈 **运行时统计**:`/admin/stats` 看每 cube/datasource 的 query 数、耗时、错误率
- 🤖 **AI Skill Builder**:7 步引导 + 自动模式,DeepSeek 驱动,自动生成 plugin.yaml
- 📊 **Prometheus**:`/metrics` 端点暴露 query/duration/rows 指标
- 🔗 **Cube JOIN(W5)**:单层 join 支持,2 个 cube 跨表查询

## 快速开始

```bash
# 1. 准备环境
cp .env.example .env
# 编辑 .env 填入 DEEPSEEK_API_KEY

# 2. 编译 + 准备 demo 数据库
go build -o bin/agent.exe ./cmd/agent
go build -o bin/seed.exe ./cmd/seed
.\bin\seed.exe   # 创建 ./data/demo.db,10 个 orders

# 3. 启动
.\bin\agent.exe
```

启动后访问 `http://localhost:8088`。

## API 端点

| 端点 | 方法 | 说明 | 状态 |
|------|------|------|------|
| `/livez` | GET | 存活探针 | ✅ W1 |
| `/readyz` | GET | 就绪探针(至少 1 个 plugin) | ✅ W1 |
| `/v1/meta` | GET | 数据模型元数据(cube.js 兼容) | ✅ W1 |
| `/cubejs-api/v1/meta` | GET | 同上,兼容老 BI | ✅ W1 |
| `/v1/sql` | POST | 编译 query → 拿 SQL 不执行 | ✅ W2 |
| `/v1/load` | POST | 编译 query → 执行 → 返结果(支持 JOIN W5) | ✅ W5 |
| `/v1/dry-run` | POST | 编译校验 query(不执行) | ✅ W2 |
| `/metrics` | GET | Prometheus 指标 | ✅ W5 |
| `/admin/plugins` | GET | 已加载 plugin 列表 | ✅ W1 |
| `/admin/reload` | POST | 手动触发 plugin 重新扫描 | ✅ W1 |
| `/admin/datasources` | GET | 已注册数据源列表(DSN 脱敏) | ✅ W3 |
| `/admin/ping` | GET | 验证所有数据源可达 | ✅ W3 |
| `/admin/stats` | GET | 运行时统计(per cube/datasource) | ✅ W3 |
| `/admin/skill/build` | POST | AI skill 启动引导会话(Step 1) | ✅ W4 |
| `/admin/skill/auto-build` | POST | 一键全自动(Step 1-7) | ✅ W5 |
| `/admin/skill/sessions` | GET | 列出所有引导会话 | ✅ W4 |
| `/admin/skill/session/:id` | GET | 取会话状态 | ✅ W4 |
| `/admin/skill/step/datasource` | POST | Step 2:选数据源 | ✅ W4 |
| `/admin/skill/step/analyze` | POST | Step 3:introspect + LLM 分析 | ✅ W4 |
| `/admin/skill/step/design` | POST | Step 4:cube 设计 | ✅ W4 |
| `/admin/skill/step/generate` | POST | Step 5:生成 plugin YAML | ✅ W4 |
| `/admin/skill/step/validate` | POST | Step 6:校验 | ✅ W4 |
| `/admin/skill/step/publish` | POST | Step 7:发布 + 触发 reload | ✅ W4 |

## 项目结构

```
cube-agent-server/
├── cmd/
│   ├── agent/                    # 主入口
│   └── seed/                     # demo 数据 seed 脚本
├── config/
│   ├── agent.yaml                # 主配置
│   ├── datasources.yaml          # 数据源配置
│   └── datasources.go            # 数据源加载器
├── plugins/                      # plugin 部署目录
│   ├── orders/plugin.yaml
│   └── products/plugin.yaml
├── data/                         # 运行时数据(SQLite 文件等)
├── internal/
│   ├── api/                      # HTTP 层 (gin)
│   │   ├── router.go
│   │   ├── middleware/           # request_id / recovery / logging
│   │   └── handlers/             # health / meta / admin / query
│   ├── compiler/                 # ⭐ 3-Pass Query 编译器
│   │   ├── pass1.go              #   JSON → IR
│   │   ├── ir.go                 #   Pass2 引用解析
│   │   ├── pass3.go              #   IR → SQL
│   │   ├── query/                #   JSON Query 数据结构
│   │   └── sqlbuilder/           #   SQL AST + Renderer
│   ├── config/                   # viper + godotenv
│   ├── engine/                   # ⭐ 查询执行
│   │   ├── executor.go           #   cube → datasource 路由
│   │   ├── integration_test.go
│   │   └── source/               #   DataSource 抽象 + SQLite 实现
│   ├── log/                      # zap
│   ├── plugin/                   # fsnotify 热加载
│   ├── schema/                   # ⭐ 数据模型核心
│   │   ├── types.go              #   Plugin / Cube / Measure / Dimension
│   │   ├── dsl.go                #   YAML 解析
│   │   ├── validate.go           #   校验规则
│   │   ├── registry.go           #   原子切换 + 多读单写
│   │   ├── meta.go               #   cube.js 兼容 adapter
│   │   ├── plugins.go            #   /admin/plugins 列表
│   │   └── schema_test.go        #   ✅ 7 个 case
│   ├── security/                 # 行级安全上下文
│   └── skill/                    # W4: AI skill 引导
├── .env.example
├── .gitignore
└── README.md
```

## Plugin DSL 示例

`plugins/orders/plugin.yaml`:

```yaml
apiVersion: cube-agent/v1
kind: Plugin
metadata:
  name: orders
  version: 0.1.0
  description: 订单主表数据模型
  datasource: demo
  owner: data-team
spec:
  cubes:
    - name: orders
      sql: "SELECT * FROM orders WHERE tenant_id = '${SECURITY.tenant_id}'"
      description: 订单主表
      primary_key: id
      measures:
        - name: count
          type: count
        - name: total_amount
          type: sum
          sql: amount
      dimensions:
        - name: id
          sql: id
          type: number
          primary_key: true
        - name: status
          sql: status
          type: string
        - name: created_at
          sql: created_at
          type: time
      segments:
        - name: paid
          sql: "{CUBE}.status IN ('paid', 'shipped', 'done')"
```

改完保存,agent 在 500ms 内自动热加载,`/v1/meta` 立即看到新内容。

## JSON Query 例子

`POST /v1/load`:

```json
{
  "measures": ["orders.total_amount", "orders.count"],
  "dimensions": ["orders.status"],
  "timeDimensions": [{
    "dimension": "orders.created_at",
    "dateRange": ["2026-08-10", "2026-08-15"],
    "granularity": "day"
  }],
  "filters": [
    {"member": "orders.customer_id", "operator": "equals", "values": [1]}
  ],
  "order": [["orders.total_amount", "desc"]],
  "limit": 100
}
```

`X-Tenant-Id: acme` header 注入到 `${SECURITY.tenant_id}` 实现行级过滤。

## 里程碑

| 阶段 | 周期 | 状态 | 交付 |
|------|------|------|------|
| **W1** | D1-D5 | ✅ 完成 | 骨架 + gin + schema + 热加载 + admin |
| **W2** | D1-D5 | ✅ 完成 | SQL AST + 3-Pass 编译器 + 真数据源(SQLite) |
| **W3** | D1-D5 | ✅ 完成 | PG/MySQL/CH/MSSQL/CSV 驱动 + per-cube dialect + 监控 + admin 扩展 |
| **W4** | D1-D5 | ✅ 完成 | AI skill 7 步引导 + DeepSeek 客户端 + introspect + LLM 分析层 |
| **W5** | D1-D5 | ✅ 完成 | PG/MySQL/CH/MSSQL introspect + Prometheus /metrics + AI skill 自动模式 + 单层 JOIN |

## 测试

```bash
# 单元测试
go test ./...

# 集成测试
go test ./internal/engine -run TestIntegration_EndToEnd

# 端到端 demo
.\bin\agent.exe  # 启动
curl http://localhost:8088/v1/meta
curl -X POST http://localhost:8088/v1/sql -H "Content-Type: application/json" -d '{"measures":["orders.count"]}' -H "X-Tenant-Id: acme"
curl -X POST http://localhost:8088/v1/load -H "Content-Type: application/json" -d '{"measures":["orders.total_amount","orders.count"],"dimensions":["orders.status"]}' -H "X-Tenant-Id: acme"
```

## 已知约束(阉割版)

- 单层 join(最多 2 个 cube,W5),阉割版不支持多跳 join
- 无 subquery / 无 CTE
- 无 pre-aggregation
- 行级安全过滤(支持 `${SECURITY.x}` 模板),无 member-level
- 时间维度只支持 `day/week/month/quarter/year/hour`
- 一个 plugin = 一个数据源,不允许跨源 join
- 一个 cube 最多 3 个 join edge
- 时间字段 dateRange 当前按字符串比较,真实 timestamptz 字段需要 PG 真实数据源
- CSV 数据源数字列返回 string(SQLite type affinity,需要类型推断)
- Parquet 数据源未实现(生态不成熟,评估 DuckDB 集成)
- W5 AI skill 自动模式:无 LLM 时按简单规则推断 measure/dimension(W4 阉割版)
- W5 introspect:PG/MySQL/CH/MSSQL 真实 DB 测试需用户配置 DSN

完整规则见 `docs/RULES.md`(W2 阶段产出,可在 W3/W5 完善)。
