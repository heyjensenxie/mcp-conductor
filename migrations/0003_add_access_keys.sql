-- 003：新增 API Key 访问控制（AccessKey + 白名单 grants）。
-- Backed 需求：按 key 独立限流（QPS/Burst）、跨 Server 聚合授权工具（Grants）、
-- 每个 key×tool 可配置调用参数（default_args）与请求头（headers）。
-- 密钥只存 HMAC-SHA256 哈希（key_hash），明文仅创建时返回一次。
-- 幂等：全部 IF NOT EXISTS，可重复执行。

SET NAMES utf8mb4;

-- API Key 调用方（subject 唯一，作为认证/授权/限流主体）。
CREATE TABLE IF NOT EXISTS access_keys (
  id         VARCHAR(64)  NOT NULL,
  name       VARCHAR(128) NOT NULL,
  subject    VARCHAR(128) NOT NULL COMMENT '唯一主体标识',
  enabled    TINYINT(1)   NOT NULL DEFAULT 1,
  key_hash   VARCHAR(64)  NOT NULL COMMENT 'HMAC-SHA256(token_secret, key) hex',
  qps        INT          NOT NULL DEFAULT 0 COMMENT '0 表示沿用全局限流配置',
  burst      INT          NOT NULL DEFAULT 0,
  created_at DATETIME(3)  NOT NULL,
  updated_at DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_access_keys_subject (subject),
  KEY idx_access_keys_hash (key_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 白名单 + 调用配置（headers / default_args 以 JSON 文本保存）。
CREATE TABLE IF NOT EXISTS access_key_grants (
  id           VARCHAR(64)  NOT NULL,
  key_id       VARCHAR(64)  NOT NULL,
  gateway_name VARCHAR(255) NOT NULL COMMENT '对外名 server.tool，支持 * 与 server.* 通配',
  headers      TEXT         NULL COMMENT '调用该工具时附加的请求头 JSON 文本',
  default_args TEXT         NULL COMMENT '调用时注入的固定参数 JSON 文本',
  PRIMARY KEY (id),
  UNIQUE KEY uq_grants_key_tool (key_id, gateway_name),
  CONSTRAINT fk_grants_key FOREIGN KEY (key_id) REFERENCES access_keys (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;