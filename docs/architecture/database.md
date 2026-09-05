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

## 5.7 兼容约定（本地开发与 CI 保持一致）

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
| 迁移 | 只提交可重复执行的 DDL（`IF NOT EXISTS`）；破坏性变更拆小步并写清回滚说明；CI 需在真实 MySQL 5.7 上跑迁移 + 相关用例 |

## 目录

- 迁移文件：`migrations/`（`.sql`，5.7 可执行）
- MySQL 驱动：`internal/storage/mysql`（已实现，满足 `storage.Store`；DSN 须含 `parseTime=true&loc=UTC&charset=utf8mb4`）
- 集成测试：`MYSQL_TEST_DSN='...' go test ./internal/storage/mysql/ -v`（未设 DSN 自动跳过；须在真实 MySQL 5.7 上执行迁移后运行）
- 应用迁移（开发环境，连宿主机 MySQL）：迁移文件为可重复执行的 DDL（幂等），执行器不做版本记录表：
  ```bash
  CONDUCTOR_DATABASE_DSN='user:pass@tcp(host:3306)/conductor?parseTime=true&loc=UTC&charset=utf8mb4' \
    go run ./cmd/migrate     # 等价 make db-migrate
  ```
  `cmd/migrate` 按文件名序执行 `migrations/*.sql` 并自动补 `multiStatements=true`。

## 上线检查清单（涉及 SQL 的改动合入前）

1. `mysql:5.7` 容器实际执行迁移通过；
2. 全量查询无 8.0 专属语法（可用 5.7 版 `mysqld --sql-mode=...` / EXPLAIN 复核）；
3. 索引长度 < InnoDB 限制（utf8mb4 下 VARCHAR(255) = 1020 字节，注意除外键/联合索引长度）；
4. 时间列、字符集、布尔列按上表约定；
5. Credential 值以 AES-256-GCM 密文入库（`credentials.encryption_key`，密文 hex 存 `encrypted_value` 列）；不落 SQL 明文、不下发 API、不入日志。