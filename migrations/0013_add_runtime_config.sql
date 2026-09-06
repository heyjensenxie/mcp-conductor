-- 0013: 新增 runtime_config 表——"运行期治理配置"单行快照。
--
-- 背景：IP 黑名单与数据面三级限流阈值需支持 Console/API 后台动态配置并持久化。
-- 该表始终只保存一行（PK id 固定 1，无自增），由控制面整份覆盖写；无保存值时
-- 网关回退 config.yaml 种子（ratelimit.* 与 security.ip_blocklist）。
-- ip_blocklist 为 JSON 数组文本（IP 或 CIDR，仅数据面 /mcp 封禁）。
--
-- 兼容目标：MySQL 5.7；CREATE TABLE IF NOT EXISTS 幂等可重放。
SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS runtime_config (
  id           TINYINT UNSIGNED NOT NULL COMMENT '固定主键（恒为 1，单行快照）',
  qps          INT     NOT NULL DEFAULT 0 COMMENT '维度默认限流 QPS（key/IP 未单独配置时）',
  burst        INT     NOT NULL DEFAULT 0 COMMENT '维度默认突发容量',
  ip_qps       INT     NOT NULL DEFAULT 0 COMMENT '单来源 IP 限流 QPS（0=沿用 qps）',
  ip_burst     INT     NOT NULL DEFAULT 0 COMMENT '单来源 IP 突发容量（0=沿用 burst）',
  global_qps   INT     NOT NULL DEFAULT 0 COMMENT '全局总闸 QPS（0=不启用全局级）',
  global_burst INT     NOT NULL DEFAULT 0 COMMENT '全局总闸突发容量',
  ip_blocklist TEXT    NULL    COMMENT '来源 IP/CIDR 封禁名单 JSON（仅 /mcp）',
  updated_at   DATETIME(3) NOT NULL COMMENT '最近保存时间（UTC）',
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='运行期治理配置（IP 黑名单 + 三级限流阈值，Console 维护）';
