# Cube Agent Server

一个**数据查询 Agent 服务** —— 给 AI Agent 用的"数据库大脑"。它把任意数据源(SQLite / PostgreSQL / MySQL / ClickHouse / SQL Server / CSV)包装成 cube.js 兼容的 API,让 AI 通过 `/v1/meta` 发现数据模型,通过 `/v1/load` 拿到聚合结果,完全不用手写 SQL。

它自己也是一个可独立安装的 Agent 二进制:build → 部署 → 启动,其他 AI 系统(Mavis、Claude Desktop、自研 Agent 框架)就可以像调 MCP 工具一样调它。

**两个层面的"Agent"**:

- **本服务本身**:一个常驻 HTTP 服务,作为可被 AI 调用的数据查询 Agent
- **数据模型 Agent(Plugin)**:`plugins/<name>/plugin.yaml`,每个文件是一个"数据域 Agent",由本服务加载并对外暴露

---

## 特性

- 🔥 **热加载 Plugin**:`plugins/<name>/plugin.yaml` 改完保存自动生效,无需重启
- 📊 **cube.js 兼容查询**:`/v1/meta` + `/v1/load` 与 cube.js 一致,Superset / Metabase / 任何 cube 客户端都能直接对接
- 🤖 **AI Skill Builder**:`/admin/skill/auto-build` 一句业务意图自动从数据源生成数据模型 Agent(DeepSeek 驱动)
- 🗄️ **多数据源**:SQLite / PostgreSQL / MySQL / ClickHouse / SQL Server 2008 R2 / CSV
- 🔐 **行级安全**:`${SECURITY.tenant_id}` 模板按请求 header 注入,做多租户隔离
- 📈 **Prometheus 指标**:`/metrics` 端点暴露 query 数 / 耗时 / 错误率,方便监控 Agent 健康

---

## 目录

- [作为 Agent 安装](#作为-agent-安装)
- [被 AI Agent 调用](#被-ai-agent-调用)
- [配置](#配置)
- [安装数据模型 Agent(Plugin)](#安装数据模型-agentplugin)
- [API 文档](#api-文档)
- [查询示例](#查询示例)
- [常见错误码](#常见错误码)

---

## 作为 Agent 安装

把 Cube Agent Server 当成一个常驻服务部署起来。安装完后,任何能发 HTTP 请求的 AI Agent 都能调用它。

### 环境要求

- **运行时**:无外部依赖(Go 编译成单一二进制,内嵌 SQLite 驱动)
- **编译时**(从源码构建):Go 1.22+(项目用 `go 1.26.3`)
- **操作系统**:Windows / Linux / macOS 都行
- **数据源**:至少一个可用数据库(SQLite 文件 / PG / MySQL / CH / SQL Server / CSV),没数据源服务起不来
- **AI Skill Builder**(可选):`DEEPSEEK_API_KEY`,没配也能跑,只是 AI 自动生成数据模型那一步会失败

### 方式一:本地直接运行

适合开发、自用、小规模场景。

```bash
# 1. 准备配置
cp .env.example .env
# 编辑 .env,至少填 DEEPSEEK_API_KEY(可选,只有用 AI Skill Builder 才需要)

# 2. 编辑数据源
# 修改 config/datasources.yaml,填实际数据库连接

# 3. 编译
go build -o bin/agent.exe ./cmd/agent        # Windows
go build -o bin/agent ./cmd/agent             # Linux / macOS

# 4. 前台启动
./bin/agent.exe                               # Windows
./bin/agent                                   # Linux / macOS
```

启动后监听 `:8088`,日志输出到 stdout。Ctrl+C 退出。

### 方式二:作为系统服务常驻

适合生产/服务器场景,机器重启后自动拉起。

#### Linux (systemd)

创建 `/etc/systemd/system/cube-agent.service`:

```ini
[Unit]
Description=Cube Agent Server
After=network.target

[Service]
Type=simple
User=cube-agent
WorkingDirectory=/opt/cube-agent
ExecStart=/opt/cube-agent/bin/agent
Restart=always
RestartSec=5
EnvironmentFile=/opt/cube-agent/.env

[Install]
WantedBy=multi-user.target
```

启用:

```bash
sudo cp bin/agent /opt/cube-agent/bin/agent
sudo cp -r config plugins .env /opt/cube-agent/
sudo systemctl daemon-reload
sudo systemctl enable --now cube-agent
sudo systemctl status cube-agent
journalctl -u cube-agent -f   # 看实时日志
```

#### Windows (NSSM 包装成服务)

用 [NSSM](https://nssm.cc/) 把 `agent.exe` 注册成 Windows 服务:

```powershell
# 下载 NSSM,假设放在 C:\nssm\
C:\nssm\win64\nssm.exe install CubeAgent `
  C:\cube-agent\bin\agent.exe `
  -AppDirectory C:\cube-agent
C:\nssm\win64\nssm.exe set CubeAgent AppEnvironmentExtra DEEPSEEK_API_KEY=sk-xxx
C:\nssm\win64\nssm.exe set CubeAgent AppStdout C:\cube-agent\logs\service.out.log
C:\nssm\win64\nssm.exe set CubeAgent AppStderr C:\cube-agent\logs\service.err.log
C:\nssm\win64\nssm.exe set CubeAgent AppRotateFiles 1
C:\nssm\win64\nssm.exe set CubeAgent AppRotateBytes 10485760
C:\nssm\win64\nssm.exe start CubeAgent
```

服务管理:

```powershell
Get-Service CubeAgent
Restart-Service CubeAgent
Stop-Service CubeAgent
```

### 方式三:Docker 容器部署

适合容器化、云原生场景。

#### Dockerfile(项目根目录建一个)

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/agent ./cmd/agent

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/agent /app/agent
COPY config /app/config
COPY plugins /app/plugins
EXPOSE 8088
ENV CUBE_AGENT_HTTP_ADDR=:8088
ENTRYPOINT ["/app/agent"]
```

#### 构建并运行

```bash
docker build -t cube-agent:latest .
docker run -d --name cube-agent \
  -p 8088:8088 \
  -v $(pwd)/.env:/app/.env:ro \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/logs:/app/logs \
  -v $(pwd)/plugins:/app/plugins \
  cube-agent:latest
```

#### docker-compose

`docker-compose.yml`:

```yaml
version: "3.8"
services:
  cube-agent:
    build: .
    image: cube-agent:latest
    container_name: cube-agent
    ports:
      - "8088:8088"
    volumes:
      - ./.env:/app/.env:ro
      - ./data:/app/data
      - ./logs:/app/logs
      - ./plugins:/app/plugins
    restart: unless-stopped
```

```bash
docker compose up -d
```

### 安装验证

服务起来后,5 步验证:

```bash
# 1. 进程在跑
curl -s http://localhost:8088/livez
# 期望: 200 OK

# 2. 至少 1 个 plugin 已加载
curl -s http://localhost:8088/readyz
# 期望: 200 OK(没有任何 plugin 时返回 503)

# 3. 元数据可拉
curl -s http://localhost:8088/v1/meta | head -c 500
# 期望: cube 列表的 JSON

# 4. 数据源能 ping 通
curl -s http://localhost:8088/admin/ping
# 期望: {"all_ok":true, ...}

# 5. 实际查一条数据
curl -X POST http://localhost:8088/v1/load \
  -H "Content-Type: application/json" \
  -d '{"measures":["orders.count"]}'
# 期望: {"data":[{"orders.count":N}], ...}
```

5 步全过 = Agent 安装完成,可以被其他 AI 系统调用了。

---

## 被 AI Agent 调用

本服务设计目标就是**给 AI Agent 调用**。调用方只需要 3 步:

### 1. 发现数据模型

Agent 先调 `/v1/meta` 拿到所有可用的 cube / measure / dimension,然后决定能查什么:

```bash
GET http://<agent-host>:8088/v1/meta
```

返回结构(简化):

```json
{
  "cubes": [
    {
      "name": "orders",
      "measures": [
        {"name": "orders.count", "type": "count", "title": "订单数"},
        {"name": "orders.total_amount", "type": "sum", "title": "订单总金额"}
      ],
      "dimensions": [
        {"name": "orders.status", "type": "string"},
        {"name": "orders.created_at", "type": "time"}
      ]
    }
  ]
}
```

### 2. 构造 JSON Query 并查询

拿到元数据后,Agent 选好 measure / dimension / filter,POST 到 `/v1/load`:

```bash
POST http://<agent-host>:8088/v1/load
Content-Type: application/json
X-Tenant-Id: acme        # 多租户场景下传,触发 ${SECURITY.tenant_id} 注入

{
  "measures": ["orders.total_amount", "orders.count"],
  "dimensions": ["orders.status"],
  "timeDimensions": [{
    "dimension": "orders.created_at",
    "dateRange": ["2026-08-10", "2026-08-15"],
    "granularity": "day"
  }]
}
```

返回:

```json
{
  "data": [
    {"orders.status": "paid", "orders.total_amount": 1234.5, "orders.count": 10, "orders.created_at.day": "2026-08-10"},
    ...
  ],
  "sql": "SELECT ...",       // 实际下发的 SQL,方便审计
  "request_id": "..."         // 关联服务端日志
}
```

### 3. 集成模式

| 调用方 | 集成方式 |
|--------|----------|
| Mavis / Claude Desktop / 通用 LLM Agent | 把 `/v1/meta` + `/v1/load` 暴露成 function/tool 工具,Agent 用自然语言选 measure 后调 |
| 自研 Agent 框架 | 直接 HTTP 调用,JSON Query 见 [查询示例](#查询示例) |
| Superset / Metabase / cube.js 客户端 | 配置 cube.js endpoint 为 `http://<host>:8088` 即可,完全兼容 |
| BI 看板 | `/cubejs-api/v1/meta` 路径兼容老 BI |

LLM 集成最简示例(伪代码):

```python
# 1. 启动时拉一次元数据,塞进 system prompt
meta = http.get(f"{AGENT_URL}/v1/meta").json()
schema_desc = format_meta_for_llm(meta)

# 2. Agent 决定查什么 → 构造 JSON Query
query = llm.generate_json_query(schema_desc, user_question)

# 3. 调 Agent
result = http.post(f"{AGENT_URL}/v1/load", json=query).json()
return result["data"]
```

`format_meta_for_llm` 的推荐做法:把 `cubes[].measures` / `dimensions` 平铺成 `<cube>.<field> = <中文描述>` 的清单,LLM 选完你再拼成 cube.js query。

---

## 配置

### 配置文件层级

优先级:**环境变量 > .env 文件 > config/agent.yaml**

| 文件 | 作用 |
|------|------|
| `.env` | 密钥等敏感信息(不进 git) |
| `config/agent.yaml` | 服务端口、日志、Plugin 目录、LLM 配置 |
| `config/datasources.yaml` | 数据源连接信息 |

### 关键配置项

`config/agent.yaml`:

```yaml
server:
  http_addr: ":8088"          # HTTP 监听地址
  shutdown_timeout: 15s

log:
  level: info                 # debug / info / warn / error
  format: json                # json / console
  no_color: false             # Windows 终端设为 true 关闭 ANSI 颜色

plugins:
  dir: "./plugins"            # Plugin YAML 目录
  watch: true                 # 开启热加载
  reload_debounce_ms: 500     # 多次写盘合并,避免抖动

ai:
  llm:
    provider: deepseek
    api_key: "${DEEPSEEK_API_KEY}"
    model: "deepseek-chat"
    timeout: 60s
```

`config/datasources.yaml`:

```yaml
datasources:
  - name: demo                  # 数据源唯一名,Plugin 用这个引用
    type: sqlite
    driver: sqlite
    dsn: "file:./data/demo.db?cache=shared&_pragma=foreign_keys(1)"
    pool:
      max_open: 5
      max_idle: 2

  - name: postgres_prod
    type: postgres
    driver: postgres
    dsn: "postgres://user:pass@localhost:5432/dbname?sslmode=disable"
    pool:
      max_open: 10
      max_idle: 5
```

> **SQL Server 2008 R2 必填字段**:`dsn` 末尾必须带 `encrypt=disable&trustservercertificate=true`,否则 SSL 握手失败。

环境变量覆盖示例(Windows PowerShell):

```powershell
$env:CUBE_AGENT_HTTP_ADDR = ":9000"
$env:DEEPSEEK_API_KEY = "sk-xxx"
./bin/agent.exe
```

---

## 安装数据模型 Agent(Plugin)

一个 Agent = 一个数据模型文件 = 一个 `plugins/<name>/plugin.yaml`。Agent 定义了:

- 数据源(`datasource` 字段,引用 `datasources.yaml` 里的 name)
- SQL 查询模板(支持 `${SECURITY.tenant_id}` 多租户过滤)
- Measure(度量:count / sum / avg / min / max / countDistinct)
- Dimension(维度:string / number / time / boolean)
- Segment(预定义过滤片段)

安装有两种方式:**手动写 YAML** 或 **AI 自动生成**。

### 方式一:手动写 Plugin YAML

在 `plugins/<name>/plugin.yaml` 创建文件,例如 `plugins/orders/plugin.yaml`:

```yaml
apiVersion: cube-agent/v1
kind: Plugin
metadata:
  name: orders                       # Plugin 名,也是 /v1/meta 里的命名空间
  version: 0.1.0
  description: 订单主表数据模型
  datasource: demo                   # 对应 datasources.yaml 里的 name
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

保存后,**500ms 内自动热加载**,`/v1/meta` 立即可见。

### 方式二:AI 自动生成(Skill Builder)

调用 `POST /admin/skill/auto-build`,传一句业务意图和数据源名,LLM 自动 introspect 表结构 → 设计 cube → 生成 YAML → 校验 → 发布:

```bash
curl -X POST http://localhost:8088/admin/skill/auto-build \
  -H "Content-Type: application/json" \
  -d '{
    "intent": "用 dbo.t_bd_item_info 商品主表建 cube,要支持按品牌、供应商聚合",
    "datasource": "hbpos"
  }'
```

需要 `DEEPSEEK_API_KEY` 已在 `.env` 配好。

需要分步控制时,用 7 步交互式流程:

| 步骤 | 端点 | 作用 |
|------|------|------|
| 1 | `POST /admin/skill/build` | 启动会话,传 `intent` |
| 2 | `POST /admin/skill/step/datasource` | 选数据源 |
| 3 | `POST /admin/skill/step/analyze` | introspect + LLM 分析 |
| 4 | `POST /admin/skill/step/design` | 提交 cube 设计 |
| 5 | `POST /admin/skill/step/generate` | 生成 YAML 草稿 |
| 6 | `POST /admin/skill/step/validate` | 编译校验 |
| 7 | `POST /admin/skill/step/publish` | 发布到 `plugins/` 并触发 reload |

每步传 `{"session_id": "<id>"}`,可用 `GET /admin/skill/session/:id` 查状态。

### 卸载 / 重新加载

- 卸载:删除 `plugins/<name>/plugin.yaml`,agent 自动检测
- 手动 reload:`POST /admin/reload`
- 查看已加载:`GET /admin/plugins`

---

## API 文档

服务监听 `:8088`,所有响应包含 `request_id` 字段。

### 健康检查

| 端点 | 方法 | 说明 |
|------|------|------|
| `/livez` | GET | 存活探针 |
| `/readyz` | GET | 就绪探针(至少 1 个 plugin 加载) |
| `/metrics` | GET | Prometheus 指标(需 `?registry` 接入时才有) |

### 元数据

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/meta` | GET | cube.js 兼容的数据模型元数据,Superset / Metabase 可对接 |
| `/cubejs-api/v1/meta` | GET | 同上,兼容老 BI 工具的路径 |

`GET /v1/meta` 返回每个 cube 的 measures、dimensions、segments、joins,BI 工具会调这个来动态生成查询界面。

### 查询

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/sql` | POST | 编译 JSON Query → 返 SQL,不执行 |
| `/v1/load` | POST | 编译 → 执行 → 返结果(支持单层 join + time rollup) |
| `/v1/dry-run` | POST | 编译校验,只返 `{"valid": true/false}` |

#### `POST /v1/sql`

Body 是 [JSON Query](#json-query-结构),返回:

```json
{
  "sql": "SELECT ...",
  "args": [...],
  "request_id": "..."
}
```

#### `POST /v1/load`

Body 同上,返回:

```json
{
  "data": [
    {"orders.status": "paid", "orders.total_amount": 1234.5, "orders.count": 10}
  ],
  "sql": "SELECT ...",
  "request_id": "..."
}
```

如果 query 带了 `timeRollup`,还会多一个 `rollups` 字段(在 Go 端内存聚合,不额外打 DB):

```json
{
  "data": [...],
  "rollups": {
    "week": [...],
    "month": [...]
  }
}
```

#### `POST /v1/dry-run`

只校验语法和引用,不返 SQL 不执行,用于前端做实时校验。

### 管理接口

| 端点 | 方法 | 说明 |
|------|------|------|
| `/admin/plugins` | GET | 已加载 plugin 列表 |
| `/admin/reload` | POST | 手动触发 plugin 重扫 |
| `/admin/datasources` | GET | 数据源列表(DSN 密码脱敏) |
| `/admin/ping` | GET | 验证所有数据源连通性,返回 200 / 503 |
| `/admin/stats` | GET | 运行时统计(per cube / per datasource 的 query 数、耗时、错误率) |

### AI Skill 接口

| 端点 | 方法 | 说明 |
|------|------|------|
| `/admin/skill/build` | POST | 启动引导会话(Step 1) |
| `/admin/skill/auto-build` | POST | 一键全自动(Step 1-7) |
| `/admin/skill/sessions` | GET | 列出所有进行中的会话 |
| `/admin/skill/session/:id` | GET | 取单个会话状态 |
| `DELETE /admin/skill/session/:id` | DELETE | 取消会话 |
| `/admin/skill/step/datasource` | POST | Step 2: 选数据源 |
| `/admin/skill/step/analyze` | POST | Step 3: introspect + LLM 分析 |
| `/admin/skill/step/design` | POST | Step 4: 提交 cube 设计 |
| `/admin/skill/step/generate` | POST | Step 5: 生成 YAML |
| `/admin/skill/step/validate` | POST | Step 6: 编译校验 |
| `/admin/skill/step/publish` | POST | Step 7: 发布 + reload |

---

## 查询示例

### JSON Query 结构

```json
{
  "measures": ["<cube>.<measure>", ...],
  "dimensions": ["<cube>.<dimension>", ...],
  "timeDimensions": [{
    "dimension": "<cube>.<time_dim>",
    "dateRange": ["2026-08-10", "2026-08-15"],
    "granularity": "day"
  }],
  "filters": [
    {"member": "<cube>.<dim>", "operator": "equals", "values": ["v"]}
  ],
  "order": [["<cube>.<measure>", "desc"]],
  "limit": 100
}
```

### 简单查询:按状态聚合

```bash
curl -X POST http://localhost:8088/v1/load \
  -H "Content-Type: application/json" \
  -d '{
    "measures": ["orders.total_amount", "orders.count"],
    "dimensions": ["orders.status"]
  }'
```

### 时间维度:每日销售额

```bash
curl -X POST http://localhost:8088/v1/load \
  -H "Content-Type: application/json" \
  -d '{
    "measures": ["orders.total_amount"],
    "timeDimensions": [{
      "dimension": "orders.created_at",
      "dateRange": ["2026-08-10", "2026-08-15"],
      "granularity": "day"
    }]
  }'
```

### 多粒度 rollup:同一次请求拿 day + week + month

```bash
curl -X POST http://localhost:8088/v1/load \
  -H "Content-Type: application/json" \
  -d '{
    "measures": ["orders.total_amount"],
    "timeDimensions": [{
      "dimension": "orders.created_at",
      "dateRange": ["2026-08-01", "2026-08-18"],
      "granularity": "day"
    }],
    "timeRollup": ["week", "month"]
  }'
```

DB 只查一次(最细粒度 day),Go 端内存聚合到 week / month,响应里 `data` + `rollups.week` + `rollups.month` 一起返回。适合老 SQL Server 这类每次查询都慢的库。

### 多 cube join(单层)

```json
{
  "measures": ["orders.total_amount", "products.price"],
  "dimensions": ["orders.status", "products.brand"],
  "joins": [{
    "cube": "products",
    "on": ["orders.product_id", "products.id"]
  }]
}
```

### 行级安全(多租户)

Plugin YAML 里用 `${SECURITY.<key>}` 模板,请求时用 header 注入:

```yaml
sql: "SELECT * FROM orders WHERE tenant_id = '${SECURITY.tenant_id}'"
```

```bash
curl -X POST http://localhost:8088/v1/load \
  -H "Content-Type: application/json" \
  -H "X-Tenant-Id: acme" \
  -d '{"measures":["orders.count"]}'
```

`${SECURITY.tenant_id}` 会替换成 `acme`,自动注入到 SQL。

### AI Skill 一键生成 Plugin

```bash
curl -X POST http://localhost:8088/admin/skill/auto-build \
  -H "Content-Type: application/json" \
  -d '{
    "intent": "商品主表建 cube,按品牌和供应商聚合",
    "datasource": "hbpos"
  }'
```

返回里 `path` 字段是新生成 YAML 的路径,agent 自动 reload。

---

## 常用错误码

| 状态码 | 含义 |
|--------|------|
| 200 | 成功 |
| 400 | JSON Query 解析失败 / 引用非法 |
| 404 | skill session 不存在 |
| 500 | SQL 生成失败 / 执行失败 / internal error |
| 501 | W2 阶段:`/v1/load` 接入 executor 之前的占位 |
| 503 | `/admin/ping` 有数据源不可达 |

所有错误响应形如:

```json
{"error": "query resolve failed", "detail": "...", "request_id": "..."}
```

`request_id` 可在服务日志里关联定位。

---

## License

MIT
