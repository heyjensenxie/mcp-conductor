-- 0010: 指标分钟桶趋势持久化（trend_minute）。
--
-- 背景：/api/metrics/trend 原只读进程内分钟桶（trendKeepMinutes=120，重启即清，
-- 窗口固定 2h）。0010 把“已闭合分钟桶”落库：趋势可跨重启、可回溯到保留上限，
-- 并支持 scope=instance / dim_key 维度聚焦（读侧合并见 internal/gateway/control_trend.go）。
--
-- 写入：app 每 60s 快照内存“已闭合分钟”（minute < now）后幂等 upsert（PK 防重）；
-- 保留天数由配置 observability.trend_retention_days（默认 7 天）控制，app 每小时
-- DELETE minute < cutoff 收敛。本表无外键（纯观测数据，不随 Server 删除级联清理）。
--
-- 兼容目标：MySQL 5.7。可重复执行（幂等）：CREATE TABLE IF NOT EXISTS。
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
