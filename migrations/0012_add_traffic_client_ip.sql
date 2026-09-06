-- 0012: traffic_log 增加"调用方来源 IP"列 client_ip。
--
-- 背景：调用观测/审计此前不记录客户端来源 IP，异常溯源与按来源定位缺最基础一维。
-- 0012 在流水上追加可空 client_ip 列：由最外层中间件按可信代理规则解析
-- （server.trusted_proxies；未配置只认直连 RemoteAddr，防伪造）后随行写入。
--
-- 读取：列表/详情 SELECT 与流量落库均已携带；为空（旧行/未解析到）时不回填。
--
-- 兼容目标：MySQL 5.7。可重复执行（幂等）：information_schema 守卫
-- （5.7 无 ADD COLUMN IF NOT EXISTS）。
SET NAMES utf8mb4;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='traffic_log' AND column_name='client_ip');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE traffic_log ADD COLUMN client_ip VARCHAR(64) NULL COMMENT ''调用方来源 IP（可信代理规则解析；空为未记录）'' AFTER client',
  'SELECT ''traffic_log.client_ip ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
