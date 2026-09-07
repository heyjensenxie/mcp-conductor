# MCP Conductor 架构概览

> 本文档描述 v0.1.0 的实现结构，随代码演进同步更新。

## 1. 总体形态

- **Monorepo + 模块化单体 + 单一 Go Backend**：一个二进制同时承载 Gateway Runtime、Control Plane API、Registry、Auth、Rate Limit、Observability 与基础测试能力。
- **前端独立 Vue3 Console**：构建产物内嵌进 Go 二进制（`internal/console` 的 `go:embed`），`/mcp`、`/api` 之外的路径由 SPA 兜底。
- **接口驱动、允许无外部依赖启动**：存储/限流等抽象为 interface，`memory` 实现为默认；MySQL 5.7+ / Redis 为可选扩展。生产数据库为 **MySQL 5.7+**，所有 SQL 必须保持 5.7 兼容（见 [database.md](database.md)）。

```
├── cmd/conductor        # 入口（信号处理、装配启动）
├── internal/
│   ├── app              # 组件装配（依赖注入）与生命周期
│   ├── config           # config.yaml + 环境变量（CONDUCTOR_*）
│   ├── model            # 领域模型：Server/Tool/Route/AccessKey/Credential/Traffic
│   ├── errs             # 统一错误模型（code/message/request_id；错误类别枚举）
│   ├── storage          # 存储接口；memory 与 mysql 5.7 双实现（mysql 见 database.md）
│   ├── mcp              # MCP 协议最小层：类型、JSON-RPC 传输、端点 Handler、上游 Client
│   ├── mcpclient        # 上游 Server 适配（Discover/Call/健康 Probe）
│   ├── registry         # Server CRUD、Tool 发现/聚合、命名空间
│   ├── router           # 工具名→(Server,实例) 解析
│   ├── balancer         # 负载均衡接口 + RoundRobin（健康感知）
│   ├── ratelimit        # Limiter 接口 + memory/redis(N秒滑动窗口)
│   ├── auth             # 认证：控制面 operator_token/登录会话、数据面 API Key
│   ├── access           # 按 key×tool 白名单授权 + 调用配置（managed key 模式；Operator 放行）
│   ├── health           # 周期健康巡检 + 注册/启用即时探活
│   ├── observability    # 指标聚合(P50/P95/P99) + 调用日志(采样/不记敏感体)
│   ├── console          # 内嵌前端 dist
│   └── gateway          # HTTP 组装、中间件链、统一 MCP 端点、控制面 REST
├── web/                 # Vue3 + TS + Vite + Pinia + Ant Design Vue + ECharts
├── examples/mock-mcp    # 演示用最小 MCP Server
├── deploy/  scripts/    # 容器/脚本（结构预留）
```

## 2. Gateway 请求链路

统一端点 `/mcp` 走中间件链后再进入 MCP Handler：

```
Request
  → requestID 中间件（生成/透传 request_id、trace_id）
  → logging 中间件（访问日志）
  → auth 中间件（校验 X-Api-Key / Bearer，写入 Identity）
  → rateLimit 中间件（按主体或来源 IP）
  → MCP Handler（initialize / tools/list / tools/call）
```

`tools/call` 在 Gateway 服务层完成：

```
授权（key×tool 白名单 / Operator 放行）→ 路由解析(Router) → 负载均衡(Balancer) → 并发上限
→ 上游调用(Adapter, 带超时) → 指标+调用日志(无论成败)
```

错误分类：`protocol / authentication / authorization / rate_limit / route / upstream / timeout / internal`，对外统一为 `code + message(脱敏) + request_id`。

**失败反馈回路（Balancer）**：`tools/call` 的一次真实拨测失败若属**传输/实例级故障**（连接失败/超时，上游 isError 的纯业务失败被 `registry.ToolFailedError` 标记排除），网关会回报 `RoundRobin.ReportFailure` 让该实例进入约 5s 短期冷却（不再被后续调用选中），并触发该 Server 的快速健康探活（`health.Monitor.TriggerCheck`）；探活确认健康或冷却期满后放回。这样坏实例的暴露时间从“等下一轮 30s 巡检”压到秒级，避免把每个请求都打向疑似故障的上游。

认证（默认开启）：Console 用管理员账号（`admin`/`admin_password`，未配置首启生成并打印）登录换会话；`/api` 控制面接受会话或 `operator_token`（程序化）；`/mcp` 数据面用 API Key，与登录分离。

## 3. 数据模型与命名空间

- **Server ≠ Instance（已落地）**：一个逻辑 Server 对应多个上游实例（`server_instances` 表，各自携带 `endpoint/transport/enabled/health`）。逻辑 Server 只承载 name/description/enabled 与聚合 `health_status`（由实例集合推导），对外 API 顶层不再返回 endpoint/transport，而在每条 Server 上附带 `instances[]`（控制面水合）。工具注册表与凭证仍归属逻辑 Server；工具发现与健康探测作用于具体实例，`tools/call` 在实例间做**健康感知 Round-Robin**（`instances[0]` 即最早创建的主实例，用于列表展示与默认发现拨测）。
- **Tool 对外名**：`gateway_name = <server_namespace>.<original_name>`（如 `university.search_policy`），从根本上规避多 Server 聚合的 Tool Name Collision。
- **Route 覆盖转发**：启用的 Route 其 `tool_names` 命中某 gateway 工具时，解析器把该工具调用目标 Server 覆盖为 `route.server_id`（恒等即原样；目标须已发现与源工具相同的 `original_name`；同一 gateway 工具只允许一条启用覆盖规则；目标不可调用返回 `route_error`，不回退）；`tools/list` 聚合与授权语义不变。
- **Credential 安全**：区分 Client→Gateway 与 Gateway→Upstream；敏感值经 AES-256-GCM 加密落库、不返回前端、不入日志，调用时按 Server 解密注入上游请求头（见 database.md）。

## 4. 观测

- 每次 `tools/call`：`request_id / trace_id / server / instance / tool / client / status / latency / timestamp` 落调用日志并输出结构化日志。多实例 Server 会记录**命中实例**（`instance_id`，可配合 `server_id` 精确筛选，见 `traffic_log` 0009）。
- 默认**不记录**完整参数与返回值（可能含隐私）。`observability.sample_rate` 已生效（采样即丢弃，降低高并发写放大，被丢弃的行无法回放）；`observability.record_args`（默认关）开启时只在调用日志捕获 **tools/call 入参**（供回放），响应**永不**落库。
- 趋势为**分钟桶真时序**：进程内按维度聚合（`internal/observability/metrics.go`），已闭合分钟每 60s 由 app 幂等落库 `trend_minute`（迁移 0010），跨重启可回溯、按保留天数清理；`/api/metrics/trend` 读侧以存储为闭合分钟权威源、进程内热桶补当前 open 分钟（不双计）。支持 `?scope=tool|server|instance`（instance 须 `server_id`）与 `?dim_key=` 单维聚焦（读合并见 `internal/gateway/control_trend.go`）。
- 控制面另有「以 API Key 身份试调用」`POST /api/keys/{id}/invoke` 与「调用回放」`POST /api/logs/{id}/replay`：前者以已存 Key 身份跑完整数据面验证授权；后者把捕获行直连其命中的上游实例复现（不写调用日志/metrics，诊断流量不污染真实观测）。
- 指标维度：请求量、成功率、错误率、P50/P95/P99、Server Health、Tool Call Count（经 `/api/metrics` 读取）。维度三档：工具维（默认）、`?scope=server` 的 `server:<id>` 聚合、`?scope=instance&server_id=` 的 `instance:<sid>:<iid>` 实例维（`internal/gateway/mcp.go` 同一次调用三档各记一次，实例维仅进程内）。

## 5. 明确不在 v0.1

LLM Judge、Python Evaluation Worker、AI Optimization、复杂 ABAC、审批流、Kafka、ClickHouse、Kubernetes Operator、Service Mesh、微服务拆分。评估（Evaluation）已有「Server 质量分 + 回归用例集」的即时实现（见 `internal/eval`，无 LLM/不落库）；LLM Judge、Dataset/TestCase 落库与历史对比另行列版。Traffic Replay 已落地雏形（`record_args` 捕获入参 + `POST /api/logs/{id}/replay` 直连复现，见上节）。
