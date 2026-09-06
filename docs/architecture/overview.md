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
│   ├── ratelimit        # Limiter 接口 + memory(令牌桶)/redis(固定窗口)
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

认证（默认开启）：Console 用管理员账号（`admin`/`admin_password`，未配置首启生成并打印）登录换会话；`/api` 控制面接受会话或 `operator_token`（程序化）；`/mcp` 数据面用 API Key，与登录分离。

## 3. 数据模型与命名空间

- **Server ≠ Instance（已落地）**：一个逻辑 Server 对应多个上游实例（`server_instances` 表，各自携带 `endpoint/transport/enabled/health`）。逻辑 Server 只承载 name/description/enabled 与聚合 `health_status`（由实例集合推导），对外 API 顶层不再返回 endpoint/transport，而在每条 Server 上附带 `instances[]`（控制面水合）。工具注册表与凭证仍归属逻辑 Server；工具发现与健康探测作用于具体实例，`tools/call` 在实例间做**健康感知 Round-Robin**（`instances[0]` 即最早创建的主实例，用于列表展示与默认发现拨测）。
- **Tool 对外名**：`gateway_name = <server_namespace>.<original_name>`（如 `university.search_policy`），从根本上规避多 Server 聚合的 Tool Name Collision。
- **Route 覆盖转发**：启用的 Route 其 `tool_names` 命中某 gateway 工具时，解析器把该工具调用目标 Server 覆盖为 `route.server_id`（恒等即原样；目标不可调用返回 `route_error`，不回退）；`tools/list` 聚合与授权语义不变。
- **Credential 安全**：区分 Client→Gateway 与 Gateway→Upstream；敏感值经 AES-256-GCM 加密落库、不返回前端、不入日志，调用时按 Server 解密注入上游请求头（见 database.md）。

## 4. 观测

- 每次 `tools/call`：`request_id / trace_id / server / tool / client / status / latency / timestamp` 落库并输出结构化日志。
- 默认**不记录**完整参数与返回值（可能含隐私）；`observability.record_body` 与 `sample_rate` 为后续精细化预留。
- 指标维度：请求量、成功率、错误率、P50/P95/P99、Server Health、Tool Call Count（经 `/api/metrics` 读取）。

## 5. 明确不在 v0.1

LLM Judge、Python Evaluation Worker、AI Optimization、Traffic Replay、复杂 ABAC、审批流、Kafka、ClickHouse、Kubernetes Operator、Service Mesh、微服务拆分。评估（Evaluation）已有「Server 质量分 + 回归用例集」的即时实现（见 `internal/eval`，无 LLM/不落库）；LLM Judge、Dataset/TestCase 落库与历史对比另行列版。