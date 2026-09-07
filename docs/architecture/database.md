# 数据库兼容目标：MySQL 5.7+

## 结论

**现状生产环境为 MySQL 5.7**。因此，本项目所有 Migration、DDL、SQL 查询必须保证可在 **MySQL 5.7** 上运行。这是硬性契约，覆盖 PRD 早期「PostgreSQL」的技术选型——持久化驱动按 MySQL 5.7 实现。

## 禁止的 MySQL 8.0+ 专属特性（基础版本一律不许用）

- Window Functions（`ROW_NUMBER() OVER ...` 等）
- Common Table Expressions / `WITH`（含 Recursive CTE）
- Functional Index（函数索引）
- 将 CHECK Constraint 作为可靠的数据校验机制（MySQL 5.7 解析但不强制）
- MySQL 8 专属 JSON 函数 / 排序 / 索引能力（如 `JSON_TABLE`、`->>` 优化、降序索引、`utf8mb4_0900_ai_ci` 等）
- `DEFAULT` 表达式（表达式默认值）、`COLLATE utf8mb4_0900_*`、不可见索引等 8.0 加码

以上能力如有需要，**优先通过应用层或 MySQL 5.7 兼容方案实现**（见下）。

## 5.7 兼容约定（本地开发与发布前验证保持一致）

| 主题 | 约定 |
| --- | --- |
| 引擎/字符集 | 建表显式 `ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`；所有字符串列在此基础上取舍长度 |
| 主键 | 业务实体沿用字符串稳定 id（`VARCHAR(64)`，与内存实现一致）；仅流水类表（`traffic_log`）用 `BIGINT UNSIGNED AUTO_INCREMENT` |
| 布尔 | `TINYINT(1)`，取值 0/1 |
| 时间 | `DATETIME(3)`，统一存 UTC，应用层负责时区转换 |
| JSON/结构数据 | 存为 `TEXT`（JSON 文本），由应用层 Go 解析/校验，不依赖 MySQL JSON 函数（规避 5.7→8 差异） |
| 列表字段（如 route.tool_names） | 序列化为 JSON 文本列，应用层拆装 |
| 分页 | `OFFSET/LIMIT`（禁止 CTE 分页）；大表走游标/键集分页方案留待必要时 |
| 聚合指标（P50/P95/P99） | **应用层内存聚合**（本仓库 observability 已经这么实现），SQL 不做窗口函数 |
| 校验 | 统一在 Go 应用层校验（`server.name`/`endpoint` 必填等），数据库不做 CHECK 兜底 |
| 迁移 | 只提交可重复执行的 DDL（`IF NOT EXISTS`）；破坏性变更拆小步并写清回滚说明；发布前在真实 MySQL 5.7 上跑迁移和相关用例 |

## 目录

- 迁移文件：`migrations/`（`.sql`，5.7 可执行，按文件名序应用）
- 全量结构快照：`database/schema.sql`（= migrations/0001..0017 合并的最终建表，表与字段均带注释，供结构参考与全新库初始化）。**Schema 变更先落 `migrations/` 增量迁移，再同步该快照，两者必须一致；已有库升级禁止直接执行快照。**
- MySQL 驱动：`internal/storage/mysql`（已实现，满足 `storage.Store`；DSN 须含 `parseTime=true&loc=UTC&charset=utf8mb4`）
- 集成测试：`MYSQL_TEST_DSN='...' go test ./internal/storage/mysql/ -v`（未设 DSN 自动跳过；须在真实 MySQL 5.7 上执行迁移后运行）
- 应用迁移（开发环境，连宿主机 MySQL）：迁移文件为可重复执行的 DDL（幂等），执行器不做版本记录表：
  ```bash
  CONDUCTOR_DATABASE_DSN='user:pass@tcp(host:3306)/conductor?parseTime=true&loc=UTC&charset=utf8mb4' \
    go run ./cmd/migrate     # 等价 make db-migrate
  ```
  `cmd/migrate` 按文件名序执行 `migrations/*.sql` 并自动补 `multiStatements=true`。

## server_instances（0008 起）

Server 多实例化后，具体上游端点与健康由 `server_instances` 承载；`servers` 表降级为逻辑实体：

- `servers`：`id/name/description/enabled/health_status/created_at/updated_at`。`endpoint/transport/version` 列自 0008 起**应用不再读写**（0008 回填后留作安全降级与观测留档，单一事实源是 `server_instances`）。精度说明：`endpoint` 自 0008 起改可空；`transport` 仍为 `NOT NULL DEFAULT 'https'`；`version` 自 0001 起即可空。
- `server_instances`：`id/server_id/endpoint/transport/enabled/health_status/created_at/updated_at`；`server_id` 外键 `ON DELETE CASCADE` 关联 `servers(id)`。字符串 id（新建用 `inst-` 前缀；0008 回填复用 server id），`enabled` 为实例级启停，`health_status ∈ {unknown,healthy,unhealthy}`。
- 实例列表稳定序：`ORDER BY created_at, id`（首条即"主实例"，用于列表展示与默认发现拨测）。
- Server 删除级联删除实例（MySQL 外键）；`registry`/memory 侧显式 `DeleteInstancesByServer` 对齐。
- MySQL 集成测试运行前需已应用 0001..0017（`make db-migrate`）。

## traffic_log.instance_id（0009 起）

Server 多实例后，调用观测从 Server 粒度下沉到实例粒度：`traffic_log` 在 `server_id`
后新增 `instance_id VARCHAR(64) NULL`，记录该次调用实际命中的上游实例（路由阶段
失败/单实例未落实例归属时为空）。配套复合索引 `idx_traffic_server_instance_id
(server_id, instance_id, id)`（0007 的 `(server_id, id)` 仍保留，服务纯 Server 级
过滤）。应用写入见 `internal/gateway/mcp.go`（metrics 另按 `instance:<sid>:<iid>`
维度记录，供 `/api/metrics?scope=instance` 展示，纯进程内不落库）。

> 注意：`traffic_log` 是**高增长流水且无自动保留/清理策略**（应用不 DELETE 该表），
> 生产需自行规划归档/分区；管理面列表只读最近分页（`page_size` 上限 200）以限制扫描。

## trend_minute 分钟桶趋势（0010 起）

指标聚合器的“已闭合分钟桶”（进程内 `minute < now`）每 60s 由 app（`runTrendPersist`）
幂等 upsert 到 `trend_minute`，PK `(scope, dim_key, minute)` 防重、重复 flush 无害。
读侧 `/api/metrics/trend` 以本表为已闭合分钟权威源，当前 open 分钟由进程内热桶实时补
（见 `internal/gateway/control_trend.go`）。保留天数由 `observability.trend_retention_days`
（默认 7）控制，app 每小时 `DELETE minute < cutoff` 收敛。维度语义：tool→gateway 名
（`server_id=''`）；server→server id；instance→instance id（`server_id` 记所属 Server，
供 `?scope=instance&server_id=` 过滤）。本表无外键，不随 Server 删除级联清理。

## traffic_log.request_args（0011 起，Traffic Replay 前提）

`traffic_log` 追加可空 `request_args MEDIUMTEXT`：`observability.record_args=true`
（默认关，入参可能含隐私）时记录本次实际发出的 tools/call 入参 JSON；**响应永不落库**
（工具均为查询语义，回放只需入参）。列表/分页 SELECT 不读本列，仅 `GET /api/logs/{id}`
详情与 `POST /api/logs/{id}/replay` 读取；列表以 `request_args IS NOT NULL` 派生
`has_args` 供前端启用「回放」。受 `sample_rate` 影响：被采样丢弃的行连同入参丢弃、不可回放。

## traffic_log.client_ip（0012 起，调用观测来源 IP）

`traffic_log` 追加可空 `client_ip VARCHAR(64)`：最外层中间件按可信代理规则解析的调用方
来源 IP（`server.trusted_proxies`；未配置只认直连 `RemoteAddr`，防伪造），随行写入并
随列表/详情带出，供审计与按来源定位。为空表示未解析到（旧行/直连未配置）。

## runtime_config 运行期治理配置（0013 起，Console/API 动态维护）

单行快照表（PK `id` 恒为 1，无自增），保存数据面运行期治理配置：三级限流滑动窗口
（`qps/burst/window_seconds/ip_qps/ip_burst/global_qps/global_burst`；任意 N 秒窗口内
≤ 该级 QPS×N，0014 起支持 window_seconds；0015 起支持自动封禁参数 auto_ban_*；0017 起支持 ip_whitelist 可信豁免）与来源 IP/CIDR 封禁名单（`ip_blocklist`
JSON 文本，仅 /mcp 生效）。由 `/api/runtime-config`（Operator 门禁）整份覆盖写
（`INSERT ... ON DUPLICATE KEY UPDATE`）；无保存值时网关回退 `config.yaml` 种子
（`ratelimit.*` 与 `security.ip_blocklist`）。memory 模式仅进程内承载，重启回退种子。

## 上线检查清单（涉及 SQL 的改动合入前）

1. `mysql:5.7` 容器实际执行迁移通过；
2. 全量查询无 8.0 专属语法（可用 5.7 版 `mysqld --sql-mode=...` / EXPLAIN 复核）；
3. 索引长度 < InnoDB 限制（utf8mb4 下 VARCHAR(255) = 1020 字节，注意除外键/联合索引长度）；
4. 时间列、字符集、布尔列按上表约定；
5. Credential 值以 AES-256-GCM 密文入库（`credentials.encryption_key`，密文 hex 存 `encrypted_value` 列）；不落 SQL 明文、不下发 API、不入日志。
