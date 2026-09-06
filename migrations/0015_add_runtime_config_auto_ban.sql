-- 0015: runtime_config 增加"自动封禁"参数列（enabled/window/max_violations/ban）。
--
-- 背景：来源（IP / API Key）在检测窗口内被限流 429 达到次数即自动临时封禁
-- （TTL 到期自动解封，进程内状态；此处仅持久化阈值参数，供运行期可调）。
-- 兼容目标：MySQL 5.7。可重复执行（幂等）：information_schema 守卫
-- （5.7 无 ADD COLUMN IF NOT EXISTS）。
SET NAMES utf8mb4;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='runtime_config' AND column_name='auto_ban_enabled');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE runtime_config ADD COLUMN auto_ban_enabled TINYINT(1) NOT NULL DEFAULT 0 COMMENT ''自动封禁开关''',
  'SELECT ''runtime_config.auto_ban_enabled ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='runtime_config' AND column_name='auto_ban_window_seconds');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE runtime_config ADD COLUMN auto_ban_window_seconds INT NOT NULL DEFAULT 60 COMMENT ''违规检测窗口(秒)''',
  'SELECT ''runtime_config.auto_ban_window_seconds ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='runtime_config' AND column_name='auto_ban_max_violations');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE runtime_config ADD COLUMN auto_ban_max_violations INT NOT NULL DEFAULT 5 COMMENT ''窗口内触发次数阈值''',
  'SELECT ''runtime_config.auto_ban_max_violations ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='runtime_config' AND column_name='auto_ban_ban_seconds');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE runtime_config ADD COLUMN auto_ban_ban_seconds INT NOT NULL DEFAULT 300 COMMENT ''临时封禁时长(秒, TTL 自动解封)''',
  'SELECT ''runtime_config.auto_ban_ban_seconds ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
