-- 0016: access_keys 增加"该 key 独立滑动窗口"列 window_seconds。
--
-- 背景：每个 API Key 可单独配置限流参数（与安全防护「维度默认」同构）：
-- QPS 为该 key 专属速率；window_seconds 为该 key 专属滑动窗口（秒），
-- 任一为 0 表示该项跟随安全防护的全局默认。两者合起来决定该 key 每窗口容量。
-- 兼容目标：MySQL 5.7。可重复执行（幂等）：information_schema 守卫。
SET NAMES utf8mb4;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='access_keys' AND column_name='window_seconds');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE access_keys ADD COLUMN window_seconds INT NOT NULL DEFAULT 0 COMMENT ''该 key 专属滑动窗口(秒)，0=跟随安全防护全局窗口'' AFTER burst',
  'SELECT ''access_keys.window_seconds ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
