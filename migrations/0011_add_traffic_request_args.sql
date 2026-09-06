-- 0011: traffic_log 增加“调用入参捕获”列 request_args（Traffic Replay 前提）。
--
-- 背景：traffic_log 只存观测元数据，tools/call 的入参在上游调用后即被丢弃，
-- 无法把历史真实请求回放到上游复现。0011 在流水上追加可空 request_args 列：
-- observability.record_args=true（默认关，隐私）时由应用写入本次实际发出的
-- tools/call 入参 JSON。响应**永不落库**（工具均为查询语义，回放只需入参）。
--
-- 读取：列表/分页 SELECT 不读本列（避免大列拖慢流水扫描），仅
-- GET /api/logs/{id} 详情与 POST /api/logs/{id}/replay 读取；列表以
-- request_args IS NOT NULL 布尔派生 has_args，供前端启用「回放」操作。
--
-- 兼容目标：MySQL 5.7。可重复执行（幂等）：information_schema 守卫
-- （5.7 无 ADD COLUMN IF NOT EXISTS）。
SET NAMES utf8mb4;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='traffic_log' AND column_name='request_args');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE traffic_log ADD COLUMN request_args MEDIUMTEXT NULL COMMENT ''tools/call 入参 JSON（record_args 开启时写入；列表不读，仅详情/回放）'' AFTER error',
  'SELECT ''traffic_log.request_args ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
