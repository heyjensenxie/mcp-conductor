-- 0018: server_instances 增加 stdio 启动参数列 args。
--
-- 背景：stdio 上游实例以子进程方式接入（MCP 规范 stdio 传输）。endpoint 列
-- 承载可执行命令，args 列承载提交给命令的参数（JSON 数组，TEXT 存储、应用层
-- 解析）。https/sse 实例不使用本列（为空）。
-- 兼容目标：MySQL 5.7。可重复执行（幂等）：information_schema 守卫
-- （MySQL 5.7 无 ADD COLUMN IF NOT EXISTS）。
SET NAMES utf8mb4;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='server_instances' AND column_name='args');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE server_instances ADD COLUMN args TEXT NULL COMMENT ''stdio 启动参数（JSON 数组；https/sse 为空）'' AFTER transport',
  'SELECT ''server_instances.args ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;