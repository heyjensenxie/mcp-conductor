-- MCP Conductor 基础表结构（MySQL 5.7 兼容）
-- 兼容契约见 docs/architecture/database.md：
--   引擎 InnoDB / 字符集 utf8mb4_unicode_ci；时间 DATETIME(3) 存 UTC；
--   布尔 TINYINT(1)；JSON/列表存 TEXT（应用层解析，不用 MySQL 8 JSON 函数）；
--   无 CTE / Window Function / CHECK / Function Index。
-- 迁移幂等：全部使用 IF NOT EXISTS，可重复执行。

SET NAMES utf8mb4;

-- 逻辑 MCP Server（Server != Instance，未来多实例走独立表扩展）
CREATE TABLE IF NOT EXISTS servers (
  id            VARCHAR(64)   NOT NULL COMMENT '对外稳定 id（与内存实现一致的字符串）',
  name          VARCHAR(128)  NOT NULL,
  description   TEXT          NULL,
  endpoint      VARCHAR(2048) NOT NULL,
  transport     VARCHAR(16)   NOT NULL DEFAULT 'https',
  version       VARCHAR(64)   NULL,
  enabled       TINYINT(1)    NOT NULL DEFAULT 1,
  health_status VARCHAR(16)   NOT NULL DEFAULT 'unknown',
  created_at    DATETIME(3)   NOT NULL,
  updated_at    DATETIME(3)   NOT NULL,
  PRIMARY KEY (id),
  KEY idx_servers_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 聚合后的 Tool 注册表；gateway_name 为对外命名空间名，唯一
CREATE TABLE IF NOT EXISTS tools (
  id            VARCHAR(64)   NOT NULL,
  server_id     VARCHAR(64)   NOT NULL,
  original_name VARCHAR(128)  NOT NULL,
  gateway_name  VARCHAR(255)  NOT NULL COMMENT 'server_namespace.original_name',
  description   TEXT          NULL,
  input_schema  TEXT          NULL COMMENT '输入 Schema JSON 文本，应用层解析',
  risk_level    VARCHAR(32)   NULL,
  enabled       TINYINT(1)    NOT NULL DEFAULT 1,
  created_at    DATETIME(3)   NOT NULL,
  updated_at    DATETIME(3)   NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_tools_gateway_name (gateway_name),
  KEY idx_tools_server (server_id),
  CONSTRAINT fk_tools_server FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 路由定义（tool_names 序列化为 JSON 文本）
CREATE TABLE IF NOT EXISTS routes (
  id         VARCHAR(64)   NOT NULL,
  name       VARCHAR(128)  NOT NULL,
  server_id  VARCHAR(64)   NOT NULL,
  tool_names TEXT          NULL COMMENT '对外工具名数组 JSON 文本',
  enabled    TINYINT(1)    NOT NULL DEFAULT 1,
  created_at DATETIME(3)   NOT NULL,
  updated_at DATETIME(3)   NOT NULL,
  PRIMARY KEY (id),
  KEY idx_routes_server (server_id),
  CONSTRAINT fk_routes_server FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;


-- Gateway→Upstream 凭证元数据（敏感值不在库中存明文，只标记是否配置）
CREATE TABLE IF NOT EXISTS credentials (
  id         VARCHAR(64)  NOT NULL,
  server_id  VARCHAR(64)  NOT NULL,
  name       VARCHAR(128) NOT NULL,
  kind       VARCHAR(16)  NOT NULL COMMENT 'static_token | api_key',
  header     VARCHAR(128) NULL,
  has_value  TINYINT(1)   NOT NULL DEFAULT 0,
  created_at DATETIME(3)  NOT NULL,
  updated_at DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  KEY idx_credentials_server (server_id),
  CONSTRAINT fk_credentials_server FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 调用日志（流水，自增主键；只留必要观测字段，不存敏感参数）
CREATE TABLE IF NOT EXISTS traffic_log (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  request_id VARCHAR(64)     NOT NULL,
  trace_id   VARCHAR(64)     NULL,
  server_id  VARCHAR(64)     NULL,
  tool       VARCHAR(255)    NOT NULL,
  client     VARCHAR(255)    NULL,
  status     VARCHAR(32)     NOT NULL,
  latency_ms INT             NOT NULL,
  error      TEXT            NULL,
  ts         DATETIME(3)     NOT NULL,
  PRIMARY KEY (id),
  KEY idx_traffic_ts (ts),
  KEY idx_traffic_tool (tool)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;