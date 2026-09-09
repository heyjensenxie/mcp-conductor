<div align="center">

<img src="web/public/logo.png" alt="MCP Conductor" width="180" />

# MCP Conductor

**The control plane for your MCP ecosystem.**

_Route. Govern. Observe. Evaluate. Improve._

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](go.mod)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/badge/Release-v1.0.0-blue)](CHANGELOG.md)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

</div>

<p align="center"><strong>简体中文</strong> | <a href="README_EN.md">English</a></p>

MCP gateway and control plane for **aggregation, routing, governance, observability, testing, evaluation, and optimization** of MCP servers. MCP Conductor is **not just another MCP proxy** — its long-term value is **Runtime Governance + MCP Quality Engineering**: governing MCP traffic on one side and MCP quality on the other, across the whole MCP lifecycle (`Develop → Register → Test → Evaluate → Deploy → Observe → Optimize → Re-evaluate`).

> **当前版本：v1.0.0** — 一个 Go 二进制承载 Gateway + Control Plane，已经跑通「注册 Server → 自动发现 Tools → 统一端点聚合 → 按命名空间路由调用 → 治理与观测」的完整基础闭环。

---

## Overview

- **Aggregation** — 一个统一 MCP Endpoint（`/mcp`）聚合多个上游 MCP Server；自动发现 Tools，并直接暴露上游原始工具名（如 `github.create_issue`）。跨 Server 重名会在预演中提示，实际同步跳过冲突项。
- **Gateway** — 协议校验 → 认证 → 授权 → 限流 → 路由 → 负载均衡 → 上游调用 → 观测的中间件管线，每个阶段职责单一。
- **Governance** — 领域模型优先：Server / Tool / Route / AccessKey / Credential / Traffic，配以 Control Plane API（`/api/*`）。
- **Observability** — 每次 `tools/call` 记录 `request_id / trace_id / server / tool / status / latency / timestamp`，按采样率落库并输出结构化日志；默认不记录敏感参数/返回值。

## Features (v1.0)

| 能力 | 状态 |
| --- | --- |
| MCP Server Registry + CRUD | ✅ MySQL 5.7+ 驱动（`internal/storage/mysql`）；默认 memory 可通过 `database.driver=mysql` 切换 |
| 自动发现 MCP Tools + Tool Namespace | ✅ |
| 平台工具目录与元数据覆盖 | ✅ 可重命名对外 Tool，维护描述与入参 JSON Schema；重新发现保留人工覆盖并支持恢复上游定义 |
| 统一 MCP Endpoint（`tools/list` 聚合、`tools/call` 路由） | ✅ streamable HTTP 无状态模式 |
| 健康检查按实例（initialize 握手 + 周期巡检；Server 级状态由实例聚合） | ✅ |
| 路由解析 + Round Robin 负载均衡（健康感知） | ✅ |
| **Server 多实例**：实例 CRUD · 启停 · 单实例测试 · 列表/详情水合 | ✅ |
| **tools/call 多实例健康感知 Round-Robin 真实生效**（拨测被选实例；故障/停用实例剔除） | ✅ **带失败反馈**：单实例调用失败（传输/超时，不含工具的 `isError` 业务失败）即短期冷却约 5s，不再把后续调用打向疑似故障实例，并触发该 Server 快速健康探活；探活确认健康后放回 |
| Tool/Route 管理闭环 + Route 覆盖转发 | ✅ Tool 启停；Route 编辑·启停·删除；启用 Route 命中 `tool_names` 时把该工具调用目标 Server 覆盖为 route 指向的 Server（恒等即原样；目标须已发现同名上游工具；同一工具仅允许一条启用覆盖规则） |
| 鉴权（默认开启） | ✅ Console 用管理员账号（`admin`/`admin_password`）登录换会话；/api 接受会话或 `operator_token`；/mcp 数据面用 API Key（HMAC 哈希落库、按 key×工具白名单），与登录分离 |
| Gateway→上游 Credential 管理（API Key / Static Token） | ✅ 值 AES-256-GCM 加密落库；经 `json:"-"` 不下发 API、不入日志；按 Server 注入上游请求头 |
| 按 Key×Tool 白名单授权（跨 Server 聚合） | ✅ 每个 key 只可见/可调被授权工具（支持精确工具名或 `*` 全量通配）；每工具可配调用参数与请求头 |
| Memory/Redis N 秒滑动窗口限流，支持按 Key 独立配额 | ✅ 默认关闭 |
| Request Logging + 基础 Metrics（P50/P95/P99） | ✅ 应用层聚合；`?scope=server` 按 Server 聚合 |
| **实例级流量归属（多实例观测下沉）** | ✅ 每次调用把 `instance_id` 落调用日志（traffic_log，可按 `server_id+instance_id` 筛选）并按实例记内存指标（`?scope=instance&server_id=`）；Server 详情「实例」表直接展示每个实例的 Requests / P95 / Errors |
| **长程分钟桶趋势（持久化）** | ✅ 已闭合分钟桶每 60s 幂等落库 `trend_minute`：跨重启可回溯、`?minutes` 可到保留天数（默认 7 天，`trend_retention_days`），支持 `?scope=instance&server_id=` 与 `?dim_key=` 单维聚焦；Console 观测页可切 30m~7d 窗口与维度 |
| Traffic 调用筛选 + Observability | ✅ 日志按 Server/实例/状态/关键词/时间分页筛选（page/page_size，上限 200）；指标按 Tool/Server/实例查看；趋势为真时序分钟桶（默认近 30 分钟折线，可拉长窗）。调用日志**保留清理**：默认保留最近 90 天（`observability.traffic_retention_days`，0 关闭），app 每小时分块删除超过保留期的日志，遏制流水无限膨胀 |
| **Traffic Replay（按捕获入参回放）** | ✅ `record_args=true` 时捕获 tools/call **入参**（响应永不落库）到调用日志；Traffic 行「回放」把该调用重发到其命中的上游实例复现（直连实例、诊断流量不写 metrics/调用日志） |
| **stdio 上游接入（本地子进程）** | ✅ 实例 `transport=stdio` 以子进程方式接入本地 MCP Server：`endpoint`=可执行命令 + `args`=启动参数（JSON 数组，不经过 shell）；按官方 stdio 规范换行 JSON-RPC over stdin/stdout，初始化生命周期（initialize→initialized）完整；spawn-per-request 每次操作新建并回收进程；stdio 无 HTTP 头部，Header 凭据注入仅适用 https/sse |
| Vue3 Console（Ant Design Vue + ECharts） | ✅ 构建后嵌入二进制 |
| MCP Manual Test（Console 内联） | ✅ |
| MCP 评测：Server 质量分（工具 Schema/描述/命名 + 协议 + 运行时指标）+ 回归用例集 | ✅ 无 LLM；即时计算不落库；现场拨测实例；quality/suite/meta 可 `?instance_id=` 定向某实例（缺省首个可拨测） |
| 评测报告导出（浏览器打印为 PDF） | ✅ |
| 控制面「以 API Key 身份试调用」（细粒度授权验证） | ✅ `POST /api/keys/{id}/invoke`：Operator 触发，后台用已存 Key 跑完整数据面（授权→grant 参数/头→路由→均衡→上游），**不写 metrics/调用日志**、不经 per-key 限流 |
| 工具详情页编辑 + 恢复源定义 | ✅ `/tools/:id` 支持改名/描述/入参 Schema 编辑，逐字段「恢复上游定义」 |
| Docker / Docker Compose / Makefile | ✅ |

**明确不属于 v1.0**：LLM Judge、Python Evaluation Worker、AI Optimization、复杂 ABAC、Kafka、ClickHouse、Kubernetes Operator、Service Mesh、微服务拆分。

## Architecture

```
MCP Client / AI Agent
        │  tools/list · tools/call（统一端点 /mcp）
        ▼
┌──────────────────────── MCP Conductor Gateway ────────────────────────┐
│  request_id → logging → auth → rate limit ─（HTTP 中间件）           │
│  tools/list：聚合 Tool Registry                                      │
│  tools/call：authorize → route → balance → upstream → record         │
└──────────────────────────────┬────────────────────────────────────────┘
                               ▼
                       Upstream MCP Servers
  +  Control Plane: /api/*（Registry / Tool / Route / AccessKey / Credential / 观测）
```

- **Monorepo + 模块化单体**：单一 Go Backend + 独立 Vue3 Console（`npm run build` 后经 `go:embed` 内嵌进二进制，`./mcp-conductor` 即可访问）。
- **领域模型优先**：`Server / Tool / Route / AccessKey / Credential / Traffic`（`Evaluation / Dataset / TestCase / Version` 预留边界）。
- **接口优先但克制**：核心模块面向 interface（`storage` / `ratelimit` / `balancer` / `router`）；无 Redis 也能以 memory 模式启动。

```
cmd/conductor        # 入口：装配 + 信号处理
internal/
  app               # 依赖装配与生命周期
  config            # config.yaml + CONDUCTOR_* 环境变量（分组）
  model             # 领域模型
  errs              # 统一错误模型 code/message/request_id
  storage           # 存储接口 + memory/mysql 5.7 双实现（生产 MySQL 5.7+）
  mcp               # MCP 协议最小层（手写 JSON-RPC，streamable HTTP 无状态）
  mcpclient         # 上游 Server 适配：发现 / 调用 / 健康 Probe
  registry          # Server CRUD + Tool 发现 / 命名空间聚合
  router            # 工具名 → (Server, 实例) 解析
  balancer          # 负载均衡（RoundRobin，健康感知）
  ratelimit         # memory/redis N 秒滑动窗口限流
  auth             # 认证（控制面 operator token / 数据面 API Key）
  health            # 周期健康巡检
  observability     # 指标聚合 + 调用日志（采样 / 不记敏感体）
  console           # go:embed 前端 dist
  gateway           # HTTP 组装 / 中间件 / 统一 MCP 端点 / 控制面
web/                # Vue3 + TS + Vite + Pinia + Ant Design Vue + ECharts
database/schema.sql # MySQL 5.7 兼容的空库初始化 DDL
examples/mock-mcp   # 演示用最小 MCP Server
```

底层设计详见 [docs/architecture/overview.md](docs/architecture/overview.md)，数据库兼容契约见 [docs/architecture/database.md](docs/architecture/database.md)。

## Quick Start

### 前置要求

- Go ≥ 1.25（后端）、Node ≥ 20（前端构建）、Docker（可选，用于 Compose）。
- 国内拉取 Go 依赖若直连 `proxy.golang.org` 不通，先设置镜像：`go env -w GOPROXY=https://goproxy.cn,direct`。

### 方式一：Docker Compose（一键启动应用 + MySQL + Redis）

Compose 默认构建并启动应用、MySQL 5.7 与 Redis；数据库 Schema 会在**首次创建 MySQL 数据卷**时自动初始化。MySQL 和 Redis 不映射宿主机端口，只能由 Compose 内的应用访问；数据分别保存在命名卷 `mysql_data` 与 `redis_data`。应用配置唯一来源是项目根 `config.yaml`（与本地 `go run` 同一份），Compose 会把它挂载进容器。

```bash
cp config.example.yaml config.yaml   # 首次：复制配置模板并按需编辑（含密钥）
docker compose up -d --build
# Console:  http://localhost:18110
# MCP 端点: http://localhost:18110/mcp
```

- 默认凭据为项目名派生的强密码，仅适合封闭的 Compose 网络；`config.yaml` 未跟踪、不应提交。使用 Compose 内置 MySQL/Redis 时，把 `config.yaml` 的 `database.dsn` / `redis` 指向 `mysql:3306` / `redis:6379`，密码与 `docker-compose.yml` 中 `MYSQL_*`、`CONDUCTOR_REDIS_PASSWORD` 默认一致（见 config.example.yaml 对应注释）。管理员密码、管理令牌、会话密钥与凭证加密密钥都写在 `config.yaml`；生产如需一次性覆盖，用 `export CONDUCTOR_*=xxx docker compose up`，不要引入 `.env`。
- 如需外部 MySQL/Redis，直接改 `config.yaml` 的 `database.dsn` 与 `redis` 区块；`database.driver: memory` 可脱离数据库运行。注意：`MYSQL_USER` 是 Compose 初始化的普通账号，不能设为 `root`。外部空 MySQL 可用 `CONDUCTOR_DATABASE_DSN='user:password@tcp(host:3306)/conductor?parseTime=true&loc=UTC&charset=utf8mb4' make db-migrate` 初始化。
- 清空本机所有 Compose 数据并重新初始化：`docker compose down -v`（会删除数据库和 Redis 数据）。

### 方式二：一键构建（带真实 Console 的单二进制，推荐）

Console 前端是经 `go:embed` 在编译期打包进二进制的，**只改代码不重建前端/二进制是看不到界面变化的**。

```bash
make web-install        # npm --prefix web install（首次）
make build              # 构建前端并嵌入 → bin/mcp-conductor
./bin/mcp-conductor     # 访问 :18110 即用真实 Console
```

> **鉴权默认开启**：首次启动会在控制台打印一次「引导管理员账号」（用户名 + 密码），Console 用它登录；建议先固化：
> `export CONDUCTOR_AUTH_ADMIN_USERNAME=admin CONDUCTOR_AUTH_ADMIN_PASSWORD=<你的密码> CONDUCTOR_AUTH_OPERATOR_TOKEN=<程序化管理令牌> CONDUCTOR_AUTH_TOKEN_SECRET=$(openssl rand -hex 32)` 后再启动。
> 未固化时重启会重新生成并打印新密码（旧密码随即失效）。/mcp 数据面鉴权与登录分离，使用 API Key。

> 仅调试后端、不需要 UI 时可 `make build-backend`（或 `go run ./cmd/conductor`）——此时 :18110 打开的是占位 Console（提示先构建前端），并非最新界面。

开发期前端热更新：另开终端 `make web-dev`（`:5173`，已代理 `/api` 与 `/mcp` 到后端），浏览器访问 `http://localhost:5173` 看最新界面。

### 最小闭环演示（5 分钟）

```bash
# 0. 控制面令牌：默认开启鉴权，用你固化的程序化管理令牌（未固化则取启动日志打印的管理员密码先登录换取）
export OPERATOR=<程序化管理令牌 CONDUCTOR_AUTH_OPERATOR_TOKEN>

# 1. 启动演示上游 MCP Server（提供 search/detail 两个回显工具）
go run ./examples/mock-mcp        # :9000/mcp

# 2. 注册该 Server（名称决定对外命名空间）
curl -s -X POST http://localhost:18110/api/servers \
  -H 'Content-Type: application/json' -H "X-Api-Key: $OPERATOR" \
  -d '{"name":"Mock","endpoint":"http://localhost:9000/mcp","transport":"https"}'

# 3. 聚合后的工具（gateway_name = search / detail）
curl -s http://localhost:18110/api/tools -H "X-Api-Key: $OPERATOR"

# 4. 通过统一端点调用，自动路由回上游
curl -s -X POST http://localhost:18110/mcp -H 'Content-Type: application/json' -H "X-Api-Key: $OPERATOR" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"q":"policy"}}}'
# → {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"[search] 收到参数: map[q:policy]"}]}}
```

同样的上游也可以 **stdio（子进程）** 方式接入：

```bash
# 先把 mock 构建成可执行文件（或直接用 go run，但 -stdio 模式需独立命令）
go build -o /tmp/mock-mcp ./examples/mock-mcp

# 注册 stdio 实例：endpoint = 启动命令，args = 启动参数
curl -s -X POST http://localhost:18110/api/servers \
  -H 'Content-Type: application/json' -H "X-Api-Key: $OPERATOR" \
  -d '{"name":"MockStdio","endpoint":"/tmp/mock-mcp","transport":"stdio","args":["-stdio"]}'
# 查询工具目录、tools/call 的调用方式与 HTTP 一致（gateway_name = mockstdio.search / mockstdio.detail）。
```
stdio 上游同样自动发现 Tools、健康巡检（initialize 握手）与 `/mcp` 调用路由，无需额外配置。

浏览器访问 `http://localhost:18110` 会先进入 `/login`，用管理员账号（默认 `admin` + `admin_password`）登录（`POST /api/auth/login` 换会话）；也可在 Settings 录入程序化管理令牌。

## API Reference

### 统一 MCP 端点 `POST /mcp`

streamable HTTP 无状态模式（`GET` 返回 405 以引导客户端走纯 POST）。支持方法：

| 方法 | 说明 |
| --- | --- |
| `initialize` | MCP 握手（默认协商 `2025-11-24`） |
| `tools/list` | 返回全部 Server 聚合后的 Tool 列表（保留上游原始工具名） |
| `tools/call` | 按 `gateway_name` 调用，自动授权 → 路由 → 均衡 → 转发上游 |

鉴权默认开启，会话/凭据载体为 `Authorization: Bearer <token>` 或 `X-Api-Key: <token>`：
- Console（浏览器）在 `/login` 用管理员账号（默认 `admin`，密码为 `admin_password`）登录（`POST /api/auth/login` → `mc1.` 会话令牌，默认 12h）；
- `/api/*`（控制面）：接受**管理令牌** `auth.operator_token` 或登录会话令牌；
- `/mcp`（数据面）：接受**数据面 API Key**（仅白名单内工具），与登录分离；管理/会话令牌调用 `/mcp` 视为 Operator 全量可见。

调用失败统一以 `isError: true` 结果返回，不暴露内部细节。

### 控制面 REST `GET /api/*`

统一信封：`{ "code", "message", "request_id", "data" }`。**鉴权开启时（默认）以下端点均要求管理令牌 `operator_token` 或登录会话**；数据面 API Key 访问返回 403。端点一览：

| 方法 / 路径 | 说明 |
| --- | --- |
| `GET/POST /api/servers` | 列表 / 注册（注册建逻辑 Server + seed 实例并发现 Tools；返回含 `instances`） |
| `GET /api/servers/{id}` | 详情（含 `instances`） |
| `PATCH /api/servers/{id}` | 更新：`description` 更新逻辑 Server，`endpoint/transport/args` 更新主实例 `instances[0]`（name 不可改；其余实例走实例接口） |
| `PATCH /api/servers/{id}/toggle` | 启用 / 禁用（禁用即摘除整个 Server） |
| `DELETE /api/servers/{id}` | 删除（级联删实例/Tools/凭证） |
| `POST /api/servers/test-connection` | **保存前测试连接**（无副作用）：用草稿 `{server_id?,name,endpoint,transport,args,headers}` 执行一次 MCP initialize 握手；不创建 Server/实例、不刷新 Tools、不更新健康、不保存临时 Header。带 `server_id` 时自动加载该 Server 已保存凭据，`headers` 中的同名临时头覆盖已保存头 |
| `POST /api/servers/{id}/test` | 测试连接（拨主实例）并重新发现 Tools |
| `POST /api/servers/{id}/rediscover/plan` | 预演重新发现：只读比对并返回将新增/变更/删除的工具清单（不落库） |
| `GET/POST /api/servers/{id}/instances` | 实例列表 / 新增实例（endpoint/transport；stdio 加 `args` 启动参数） |
| `PATCH .../instances/{iid}` · `.../toggle` · `.../test` · `DELETE .../instances/{iid}` | 实例编辑 · 启停 · 单实例测试 · 删除（Server 至少保留一个实例） |
| `GET /api/servers/{id}/tools` | Server 下的工具目录 |
| `POST /api/servers/{id}/credentials` · `GET .../credentials` · `PATCH .../credentials/{credId}` · `DELETE .../credentials/{credId}` | Gateway→上游凭据 CRUD（值加密落库、不下发） |
| `GET /api/tools` · `GET /api/tools/{id}` · `PATCH /api/tools/{id}/toggle` | 聚合工具分页/详情 / 工具启停 |
| `PATCH /api/tools/{id}` | 更新工具对外名称、描述、入参 Schema；支持 `reset_name/reset_description/reset_input_schema` 恢复上游定义 |
| `GET/POST /api/routes` · `PATCH /api/routes/{id}` · `PATCH /api/routes/{id}/toggle` · `DELETE /api/routes/{id}` | 路由 CRUD / 启停 / 删除 |
| `GET /api/keys` · `POST /api/keys` | API Key 分页 / 创建（明文 Secret 仅本响应返回一次） |
| `GET/PATCH/DELETE /api/keys/{id}` · `POST /api/keys/{id}/rotate` | Key 详情 · 更新（白名单/配额/启停）· 删除 · 密钥轮换（新明文仅本响应一次） |
| `POST /api/keys/{id}/invoke` | **按该 Key 身份试调用**（`{gateway_tool,arguments}`）：后台用已存 Key 跑完整数据面验证白名单授权；不写 metrics/调用日志、不经 per-key 限流。未授权/禁用 → 403 |
| `GET /api/metrics` | 指标快照（p50/p95/p99、成功率）；`?scope=server` 按 Server、`?scope=instance&server_id=` 按某 Server 实例聚合 |
| `GET /api/metrics/window` | **窗口聚合（看板统计）**：`?minutes=`（默认 30，上限 43200；控制台提供 30 分钟 ~ 7 天档位）、`?server_id=`、`?top_tools=`、`?top_ips=`、`?bucket_minutes=`。服务端一次范围扫描返回计数/成功率/平均延迟（精确）+ 分位（固定桶直方图近似）+ `by_server/by_tool/by_client_ip/by_status/by_minute`；长窗口自动降采样（3 天=10 分钟/桶、7 天=30 分钟/桶，点数 ≤ 500）。成本与窗口内行数成正比、与表总量无关 |
| `GET /api/metrics/trend` | 真时序趋势（分钟桶，已持久化、跨重启可回溯）：`?scope=tool\|server\|instance`（instance 须带 `server_id`）；`?minutes=` 默认 30、上限保留天数；`?dim_key=` 单维聚焦；长窗口自动降采样（`bucket_minutes` 返回实际桶宽） |
| `GET /api/logs` | 调用日志分页（`page/page_size`，上限 200；`?server_id=&instance_id=&q=&status=&from=&to=` 筛选；行带 `has_args` 标记是否可回放） |
| `GET /api/logs/{id}` | 调用日志详情（含已捕获入参 `request_args`，供回放弹窗） |
| `POST /api/logs/{id}/replay` | **Traffic Replay**（`{timeout_ms?}`）：把该调用捕获入参重发到其命中的上游实例（直连、诊断不写 metrics/调用日志）；is_error 以 200 返回，结构失败 4xx/5xx |
| `GET /api/evaluations/servers/{id}` | 评测概况（拨测实例/工具数/是否有流量/平台覆盖数；`?instance_id=` 可选） |
| `POST /api/evaluations/servers/{id}/quality` | 运行 Server 质量评测（`?instance_id=` 可选定向实例；现场拨测 initialize+tools/list，叠加运行时指标，即时返回分维度 MCP 分与建议） |
| `POST /api/evaluations/servers/{id}/suite` | 运行回归用例集（`{"cases":[{name,gateway_tool,arguments,expected_substring,timeout_ms}]}`，`?instance_id=` 可选；直连上游判定，即时返回汇总 + 按工具分组） |
| `POST /api/auth/login` | 管理员账号登录（`admin` + 密码）换 `mc1.` 会话 |
| `GET /api/auth/status` | 鉴权是否开启（`auth_required`） |
| `GET /healthz` · `/readyz` | 存活 / 就绪探针 |

> **破坏性变更（Server 多实例化）**：Server 顶层不再返回 `endpoint/transport`——端点与传输
> 已下沉到 `instances`（每实例独立 `endpoint/transport/enabled/health_status`），
> `GET /api/servers` 列表与 `GET /api/servers/{id}` 详情的每行都附带 `instances`
> 数组（`instances[0]` 为主实例，用于列表展示与默认发现拨测）。注册入参仍为
> `{name, description, endpoint, transport}`，其中 endpoint/transport 创建该
> Server 的 seed 实例。
>
> **stdio 上游**：实例 `transport=stdio` 时以子进程方式接入本地 MCP Server——
> `endpoint` 字段承载可执行命令、`args` 承载启动参数（字符串 JSON 数组，不经过
> shell，例如 `{"transport":"stdio","endpoint":"./bin/mock-mcp","args":["-stdio"]}`）。
> 采用 spawn-per-request：每次探测/发现/调用由网关新建并回收进程（有启动开销，
> Node 类服务器 ~1-2s）。stdio 无 HTTP 头部，Gateway→上游 Header 凭据注入仅适用
> https/sse；stdio 上游的身份凭据由进程自身环境自持。

### 错误码

`protocol_error` / `authentication_error` / `authorization_error` / `rate_limit_error` / `route_error` / `upstream_error` / `timeout_error` / `invalid_argument` / `not_found` / `internal_error`。控制面返回 HTTP 状态码 + 信封；`/mcp` 返回 JSON-RPC 错误对象。

## Configuration

配置 = **config.yaml + 环境变量**（环境变量优先级更高，命名 `CONDUCTOR_<GROUP>_<FIELD>`）：

```bash
cp config.example.yaml config.yaml
CONDUCTOR_AUTH_ENABLED=true \
CONDUCTOR_AUTH_OPERATOR_TOKEN="change-me-operator-token" \
CONDUCTOR_AUTH_TOKEN_SECRET="<32 位 hex>" \
CONDUCTOR_AUTH_API_KEYS="client-a:dev-key" \
go run ./cmd/conductor
```

| 分组 | 关键项 | 环境变量示例 |
| --- | --- | --- |
| `server` | host / port | `CONDUCTOR_SERVER_PORT=18110` |
| `database` | driver（memory/mysql）/ dsn | `CONDUCTOR_DATABASE_DSN=user:pwd@tcp(host:3306)/conductor?parseTime=true&charset=utf8mb4` |
| `redis` | enabled / addr / password / db | `CONDUCTOR_REDIS_ENABLED=false` · `CONDUCTOR_REDIS_DB=0` |
| `gateway` | upstream_timeout / max_concurrency | `CONDUCTOR_GATEWAY_UPSTREAM_TIMEOUT_MS=10000` |
| `ratelimit` | enabled / qps / burst | `CONDUCTOR_RATELIMIT_QPS=100`（代码默认 qps=0/burst=1，example 为启用示例） |
| `auth` | enabled / operator_token / token_secret / api_keys | `CONDUCTOR_AUTH_OPERATOR_TOKEN=<程序化令牌> CONDUCTOR_AUTH_TOKEN_SECRET=<32 位 hex>` |
| `auth` | admin_username / admin_password（Console 登录） | `CONDUCTOR_AUTH_ADMIN_USERNAME=admin CONDUCTOR_AUTH_ADMIN_PASSWORD=<管理员密码>` |
| `auth` | session_ttl（登录会话有效期，默认 12h） | `CONDUCTOR_AUTH_SESSION_TTL_MINUTES=720` |
| `credentials` | encryption_key（AES-256，64 位 hex） | `CONDUCTOR_CREDENTIALS_ENCRYPTION_KEY=<hex>` |
| `logging` | level / format | `CONDUCTOR_LOGGING_LEVEL=info` |
| `observability` | record_args（捕获入参供回放，默认关）/ sample_rate / trend_retention_days（默认 7）/ traffic_retention_days（默认 90，0=关闭清理） | `CONDUCTOR_OBSERVABILITY_RECORD_ARGS=false` · `CONDUCTOR_OBSERVABILITY_SAMPLE_RATE=1.0` · `CONDUCTOR_OBSERVABILITY_TREND_RETENTION_DAYS=7` · `CONDUCTOR_OBSERVABILITY_TRAFFIC_RETENTION_DAYS=90` |

完整说明见 [config.example.yaml](config.example.yaml)。docker compose 部署读取同一份项目根 `config.yaml`（见「方式一」），因此只需维护这一个配置文件。

### 数据库兼容目标：MySQL 5.7+

生产环境为 **MySQL 5.7**，因此 DDL / SQL 必须 5.7 可运行；**禁止 MySQL 8.0 专属特性**（Window Functions、CTE、Function Index、CHECK 兜底、MySQL 8 JSON 函数/索引等），需要时走 MySQL 5.7 兼容或应用层方案。详见 [docs/architecture/database.md](docs/architecture/database.md)。`database/schema.sql` 与 `internal/storage/mysql` 驱动已在真实 MySQL 5.7 上实测（含重启持久化）。

```bash
# 用 MySQL 驱动启动（先确保数据库建好并应用迁移）
CONDUCTOR_DATABASE_DRIVER=mysql \
CONDUCTOR_DATABASE_DSN='conductor:conductor@tcp(localhost:3306)/conductor?parseTime=true&loc=UTC&charset=utf8mb4' \
  go run ./cmd/conductor
```

## Development

```bash
make web-install   # 安装前端依赖
make build         # 构建前端并产出带真实 Console 的单二进制（默认构建）
make run           # 一键构建并运行（:18110，带真实 Console）
make build-backend # 仅编译后端（用当前 internal/console/dist，调试后端用）
make web-dev       # 前端热更新 :5173（代理 /api、/mcp）
make test          # 后端单元测试
make vet           # go vet 静态检查
make check         # 本地质量门禁：vet + test + 前端构建/类型检查
make docker-up     # 一键启动应用、MySQL 5.7、Redis（首次自动初始化 Schema）
make db-migrate    # 初始化外部 MySQL（需先设置 CONDUCTOR_DATABASE_DSN）
```

- **后端约定**：统一错误模型与结构化日志（不打印 Credential、Token 与完整敏感 MCP payload）；核心模块测试优先（Router / Balancer / Rate Limiter / Tool Namespace）。
- **前端约定**：基础设施管理平台风格（现代、克制、高信息密度），Ant Design Vue + ECharts + Pinia，Payload 不可达时保持空态。
- **测试**：`make check` 是提交前的本地质量门禁；真实 MySQL 5.7 集成测试需先应用 `database/schema.sql`，再显式设置 `MYSQL_TEST_DSN` 运行。启用 CGO 的环境可额外运行 `go test -race ./...`。

## Examples

- [`examples/mock-mcp`](examples/mock-mcp) — 演示用最小 MCP Server（search/detail 两个工具，监听 `:9000/mcp`，可用 `MOCK_PORT` 覆盖），用于本地闭环与手工测试。

## Roadmap

- **v1.0（当前）**：首个公开稳定版本，包含 Registry、聚合、路由、运行时治理、可观测性、基础评测、Console、MySQL 5.7+ 持久化和 Docker 交付。
- **v1.1 候选**：Route 管理体验完善、stdio 长驻会话缓存（降低子进程启动开销）、发布自动化与更多部署示例。
- **后续版本**：Evaluation 深化（Dataset/TestCase 落库、对比与 LLM Judge）、协议兼容性、Schema 与性能测试。
- **远期**：独立 Python Evaluation Worker、AI 优化建议、Route 灰度/分组转发。

## Contributing

欢迎贡献。请阅读 [CONTRIBUTING.md](CONTRIBUTING.md) 与 [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)；发现安全漏洞请按 [SECURITY.md](SECURITY.md) 私有渠道报告。

## License

[Apache-2.0](LICENSE) © mcp-conductor contributors
