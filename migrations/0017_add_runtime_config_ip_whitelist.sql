-- 0017: runtime_config 增加"IP 白名单（可信豁免）"列 ip_whitelist。
--
-- 背景：安全防护新增可信 IP 白名单（动态维护、持久化）。命中白名单的来源在
-- 数据面 /mcp 上不受 IP 黑名单 / 自动封禁(IP) / 单 IP 级限流影响。
-- 兼容目标：MySQL 5.7。可重复执行（幂等）：information_schema 守卫。
SET NAMES utf8mb4;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='runtime_config' AND column_name='ip_whitelist');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE runtime_config ADD COLUMN ip_whitelist TEXT NULL COMMENT ''可信 IP/CIDR 白名单 JSON（豁免黑名单/自动封禁/IP级限流）'' AFTER ip_blocklist',
  'SELECT ''runtime_config.ip_whitelist ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
