-- =============================================================================
-- MCP Conductor 数据库全量建表语句（最终形态 = migrations/0001..0011 合并结果）
-- =============================================================================
-- 用途：
--   1) 结构参考：一眼看全各表/字段含义（字段与表均带 COMMENT）；
--   2) 全新数据库初始化：在空库直接执行即可得到与「migrations 全量跑完」一致的表。
--
-- 维护约定：
--   - 本文件为只读结构快照，**不是**增量迁移的替代。schema 变更一律先落
--     migrations/ 下的版本化迁移，再同步更新本文件，保证两者一致。
--   - 兼容契约（详见 docs/architecture/database.md）：目标 MySQL 5.7+，
--     引擎 InnoDB / utf8mb4_unicode_ci；时间 DATETIME(3) 存 UTC（应用层换时区）；
--     布尔 TINYINT(1)；JSON/列表存 TEXT（应用层解析）；禁止 CTE / Window
--     Function / Function Index / CHECK 兜底 / MySQL 8 JSON 函数与索引。
--   - 已有库禁止直接执行本文件做“升级”，请按序执行 migrations/。
-- =============================================================================

SET NAMES utf8mb4;

-- -----------------------------------------------------------------------------
-- 逻辑 MCP Server（Server != Instance）：具体上游端点与健康在 server_instances，
-- 本表是逻辑实体与其它子表（tools/credentials/routes）的外键宿主。
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS servers (
  id            VARCHAR(64)   NOT NULL COMMENT '对外稳定 id（与内存实现一致，前缀 srv-）',
  name          VARCHAR(128)  NOT NULL COMMENT '逻辑 Server 名称（唯一业务标识，不可改）',
  description   TEXT          NULL    COMMENT 'Server 描述（0006 起 TEXT，容纳超长说明）',
  endpoint      VARCHAR(2048) NULL    COMMENT '遗留列：端点已下沉 server_instances（自 0008 起应用不再读写，仅留档）',
  transport     VARCHAR(16)   NOT NULL DEFAULT 'https' COMMENT '遗留列：传输类型 https|sse|stdio，单一事实源在 server_instances',
  version       VARCHAR(64)   NULL    COMMENT '上游协议/实现版本（观测留档）',
  enabled       TINYINT(1)    NOT NULL DEFAULT 1 COMMENT '逻辑 Server 启停',
  health_status VARCHAR(16)   NOT NULL DEFAULT 'unknown' COMMENT '聚合健康态：unknown | healthy | unhealthy',
  created_at    DATETIME(3)   NOT NULL COMMENT '创建时间（UTC）',
  updated_at    DATETIME(3)   NOT NULL COMMENT '更新时间（UTC）',
  PRIMARY KEY (id),
  KEY idx_servers_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='逻辑 MCP Server（多实例端点见 server_instances）';

-- -----------------------------------------------------------------------------
-- 逻辑 Server 下的具体上游实例（0008 起承载 endpoint/transport/健康/启停）
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS server_instances (
  id            VARCHAR(64)   NOT NULL COMMENT '对外稳定 id（新建用 inst- 前缀）',
  server_id     VARCHAR(64)   NOT NULL COMMENT '所属逻辑 Server',
  endpoint      VARCHAR(2048) NOT NULL COMMENT '上游端点 URL',
  transport     VARCHAR(16)   NOT NULL DEFAULT 'https' COMMENT 'https | sse | stdio',
  enabled       TINYINT(1)    NOT NULL DEFAULT 1 COMMENT '实例级启停（摘除/排空）',
  health_status VARCHAR(16)   NOT NULL DEFAULT 'unknown' COMMENT 'unknown | healthy | unhealthy',
  created_at    DATETIME(3)   NOT NULL COMMENT '创建时间（UTC）',
  updated_at    DATETIME(3)   NOT NULL COMMENT '更新时间（UTC）',
  PRIMARY KEY (id),
  KEY idx_server_instances_server (server_id),
  CONSTRAINT fk_server_instances_server FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='逻辑 Server 的上游实例（列表稳定序 ORDER BY created_at, id，首条为主实例）';

-- -----------------------------------------------------------------------------
-- 聚合后的 Tool 注册表；gateway_name 为对外命名空间名，唯一。
-- source_* 记录最近一次上游发现值，*_overridden 决定重新发现时是否保留平台覆盖。
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tools (
  id                    VARCHAR(64)   NOT NULL COMMENT '对外稳定 id（前缀 tool-）',
  server_id             VARCHAR(64)   NOT NULL COMMENT '所属 Server',
  original_name         VARCHAR(128)  NOT NULL COMMENT '上游原始工具名',
  gateway_name          VARCHAR(255)  NOT NULL COMMENT '对外门面名 server_namespace.original_name，全局唯一',
  description           TEXT          NULL    COMMENT '对外描述（可被门面覆盖）',
  input_schema          TEXT          NULL    COMMENT '入参 JSON Schema 文本，应用层解析（可被门面覆盖）',
  source_description    TEXT          NULL    COMMENT '最近一次上游发现的原生描述（覆盖时保留作对比）',
  source_input_schema   TEXT          NULL    COMMENT '最近一次上游发现的原生 Schema（覆盖时保留作对比）',
  name_overridden       TINYINT(1)    NOT NULL DEFAULT 0 COMMENT '名称是否平台定制（重新发现时保留定制）',
  description_overridden TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '描述是否平台定制',
  input_schema_overridden TINYINT(1)  NOT NULL DEFAULT 0 COMMENT '入参 Schema 是否平台定制',
  risk_level            VARCHAR(32)   NULL    COMMENT '风险等级（预留，可为空）',
  enabled               TINYINT(1)    NOT NULL DEFAULT 1 COMMENT '工具对外启停',
  created_at            DATETIME(3)   NOT NULL COMMENT '创建时间（UTC）',
  updated_at            DATETIME(3)   NOT NULL COMMENT '更新时间（UTC）',
  PRIMARY KEY (id),
  UNIQUE KEY uq_tools_gateway_name (gateway_name),
  KEY idx_tools_server (server_id),
  CONSTRAINT fk_tools_server FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='聚合后的 Tool 目录（含门面覆盖与上游源值）';

-- -----------------------------------------------------------------------------
-- 路由定义：tool_names 序列化为 JSON 文本，应用层拆装
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS routes (
  id         VARCHAR(64)   NOT NULL COMMENT '对外稳定 id（前缀 route-）',
  name       VARCHAR(128)  NOT NULL COMMENT '路由名',
  server_id  VARCHAR(64)   NOT NULL COMMENT '路由归属的逻辑 Server',
  tool_names TEXT          NULL    COMMENT '对外工具名数组 JSON 文本',
  enabled    TINYINT(1)    NOT NULL DEFAULT 1 COMMENT '路由启停',
  created_at DATETIME(3)   NOT NULL COMMENT '创建时间（UTC）',
  updated_at DATETIME(3)   NOT NULL COMMENT '更新时间（UTC）',
  PRIMARY KEY (id),
  KEY idx_routes_server (server_id),
  CONSTRAINT fk_routes_server FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='工具路由定义（对外暴露子集）';

-- -----------------------------------------------------------------------------
-- Gateway→Upstream 凭证。敏感值以 AES-256-GCM 密文（hex）存 encrypted_value；
-- 明文不下落 SQL、不下发 API、不入日志。
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS credentials (
  id              VARCHAR(64)   NOT NULL COMMENT '对外稳定 id（前缀 cred-）',
  server_id       VARCHAR(64)   NOT NULL COMMENT '所属 Server',
  name            VARCHAR(128)  NOT NULL COMMENT '凭证名',
  kind            VARCHAR(16)   NOT NULL COMMENT 'static_token | api_key',
  header          VARCHAR(128)  NULL    COMMENT '注入上游的 header 名，如 X-Upstream-Key',
  encrypted_value MEDIUMTEXT    NULL    COMMENT 'AES-256-GCM 加密的凭证值（hex），应用层加解密',
  has_value       TINYINT(1)    NOT NULL DEFAULT 0 COMMENT '是否已配置敏感值（元数据只读不泄露明文）',
  created_at      DATETIME(3)   NOT NULL COMMENT '创建时间（UTC）',
  updated_at      DATETIME(3)   NOT NULL COMMENT '更新时间（UTC）',
  PRIMARY KEY (id),
  KEY idx_credentials_server (server_id),
  CONSTRAINT fk_credentials_server FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Gateway→Upstream 凭证（敏感值加密落库）';

-- -----------------------------------------------------------------------------
-- 调用日志（高增长流水，自增主键；默认只留观测元数据）。
-- 管理面按 id 倒序取最近分页；0007 起为 server 等值 + 倒序翻页建了窄复合索引；
-- 0009 记录命中实例 instance_id；0011 起可选捕获 tools/call 入参（request_args，
-- 默认不存：observability.record_args 开启才写，且响应永不落库）。
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS traffic_log (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '自增流水主键（倒序即最近优先）',
  request_id VARCHAR(64)     NOT NULL COMMENT '本次调用的请求/事务标识',
  trace_id   VARCHAR(64)     NULL    COMMENT '分布式追踪 id（可选）',
  server_id  VARCHAR(64)     NULL    COMMENT '命中的逻辑 Server（可为空）',
  instance_id VARCHAR(64)    NULL    COMMENT '命中的上游实例（可为空；多实例 Server 归属用）',
  tool       VARCHAR(255)    NOT NULL COMMENT '被调用工具（对外名）',
  client     VARCHAR(255)    NULL    COMMENT '调用方标识（如 API Key subject）',
  status     VARCHAR(32)     NOT NULL COMMENT 'success 或错误码',
  latency_ms INT             NOT NULL COMMENT '端到端延迟（毫秒）',
  error      TEXT            NULL    COMMENT '失败时的错误文本（成功为空）',
  request_args MEDIUMTEXT    NULL    COMMENT 'tools/call 入参 JSON（record_args 开启时写入；列表不读，仅详情/回放）',
  ts         DATETIME(3)     NOT NULL COMMENT '调用发生时间（UTC）',
  PRIMARY KEY (id),
  KEY idx_traffic_ts (ts),
  KEY idx_traffic_tool (tool),
  KEY idx_traffic_server_id_id (server_id, id),
  KEY idx_traffic_server_instance_id (server_id, instance_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='调用流水日志（默认只存观测元数据；0011 起可 opt-in 捕获入参供回放）';

-- -----------------------------------------------------------------------------
-- API Key 访问控制：key 是认证/授权/限流主体（subject 唯一），密钥只存哈希。
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS access_keys (
  id         VARCHAR(64)  NOT NULL COMMENT '对外稳定 id（前缀 key-）',
  name       VARCHAR(128) NOT NULL COMMENT 'Key 显示名',
  subject    VARCHAR(128) NOT NULL COMMENT '唯一主体标识',
  enabled    TINYINT(1)   NOT NULL DEFAULT 1 COMMENT 'Key 启停',
  key_hash   VARCHAR(64)  NOT NULL COMMENT 'HMAC-SHA256(token_secret, key) hex，明文仅创建时返回一次',
  qps        INT          NOT NULL DEFAULT 0 COMMENT '每 key 限流 QPS，0 表示沿用全局限流配置',
  burst      INT          NOT NULL DEFAULT 0 COMMENT '突发容量（令牌桶 burst）',
  created_at DATETIME(3)  NOT NULL COMMENT '创建时间（UTC）',
  updated_at DATETIME(3)  NOT NULL COMMENT '更新时间（UTC）',
  PRIMARY KEY (id),
  UNIQUE KEY uq_access_keys_subject (subject),
  KEY idx_access_keys_hash (key_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='API Key 调用方主体';

-- -----------------------------------------------------------------------------
-- 白名单 + 调用配置：key×gateway_name 维度授权与参数/请求头注入。
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS access_key_grants (
  id           VARCHAR(64)  NOT NULL COMMENT '对外稳定 id（前缀 grant-）',
  key_id       VARCHAR(64)  NOT NULL COMMENT '所属 API Key',
  gateway_name VARCHAR(255) NOT NULL COMMENT '被授权工具对外名 server.tool，支持 * 与 server.* 通配',
  headers      TEXT         NULL    COMMENT '调用该工具时附加的请求头 JSON 文本',
  default_args TEXT         NULL    COMMENT '调用时注入的固定参数 JSON 文本',
  PRIMARY KEY (id),
  UNIQUE KEY uq_grants_key_tool (key_id, gateway_name),
  CONSTRAINT fk_grants_key FOREIGN KEY (key_id) REFERENCES access_keys (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='API Key 工具白名单与调用配置';

-- -----------------------------------------------------------------------------
-- 已闭合分钟桶趋势（0010）。metrics 每 60s 把进程内“已闭合分钟”幂等 upsert 到此，
-- 读侧作为长程权威源（当前 open 分钟由进程内热桶补）。scope=server/instance 时
-- server_id 冗余所属 Server 便于过滤；tool 维为空串。保留天数见 trend_retention_days。
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS trend_minute (
  scope      VARCHAR(16)  NOT NULL COMMENT 'tool | server | instance',
  server_id  VARCHAR(64)  NOT NULL DEFAULT '' COMMENT 'server/instance 维的归属 Server（tool 维为空串）',
  dim_key    VARCHAR(255) NOT NULL COMMENT 'tool=gateway名 / server=server id / instance=instance id',
  minute     BIGINT       NOT NULL COMMENT '已闭合分钟桶起点（UTC Unix 秒）',
  totals     BIGINT       NOT NULL DEFAULT 0 COMMENT '该分钟调用量',
  errors     BIGINT       NOT NULL DEFAULT 0 COMMENT '该分钟失败数',
  PRIMARY KEY (scope, dim_key, minute),
  KEY idx_trend_scope_server_minute (scope, server_id, minute)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='已闭合分钟桶趋势（幂等 upsert，按保留天数清理）';
