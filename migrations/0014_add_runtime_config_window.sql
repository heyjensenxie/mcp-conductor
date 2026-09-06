-- 0014: runtime_config 增加"滑动窗口长度"列 window_seconds。
--
-- 背景：数据面三级限流由令牌桶/固定窗口改为可配置 N 秒滑动窗口，任意连续
-- N 秒内最多放行 该级QPS×N 次。窗口长度为全局单一值（1..3600 秒，默认 60 = 1 分钟），
-- 纳入运行期治理快照以便后台调整；Burst 语义已废弃（列保留兼容，不再参与判定）。
--
-- 兼容目标：MySQL 5.7。可重复执行（幂等）：information_schema 守卫
-- （5.7 无 ADD COLUMN IF NOT EXISTS）。
SET NAMES utf8mb4;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='runtime_config' AND column_name='window_seconds');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE runtime_config ADD COLUMN window_seconds INT NOT NULL DEFAULT 60 COMMENT ''滑动窗口长度(秒,1..3600)，任意 N 秒内≤QPS×N；默认 1 分钟'' AFTER burst',
  'SELECT ''runtime_config.window_seconds ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
