-- 0009: traffic_log 增加实例归属列 instance_id。
--
-- Server 多实例化（0008）后，单次 tools/call 会被均衡到逻辑 Server 下的某个
-- 具体实例。0009 为调用日志补上"命中的上游实例"，配合内存 metrics 的 instance:
-- 维度，让观测能从 Server 粒度下沉到实例粒度（定位坏实例/单实例回归）。
--
-- 兼容目标：MySQL 5.7。可重复执行（幂等）：列与索引均经 information_schema
-- 守卫（5.7 无 ADD COLUMN / ADD KEY IF NOT EXISTS）。流水表仅追加一列 + 一个
-- 窄复合索引，写放大有限；实例过滤的常见查询形态是
-- WHERE server_id=? AND instance_id=? ORDER BY id DESC，由该索引承载。

SET NAMES utf8mb4;

-- 新增列：server_id 之后（与内存 TrafficSample 字段序/查询 Scan 对齐）。
SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='traffic_log' AND column_name='instance_id');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE traffic_log ADD COLUMN instance_id VARCHAR(64) NULL COMMENT ''命中的上游实例（可为空）'' AFTER server_id',
  'SELECT ''traffic_log.instance_id ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- 新增复合索引：等值 server + 等值 instance + 反向扫 id 免 filesort。
-- 0007 已有 (server_id, id)；此处新增 (server_id, instance_id, id)，两者并存：
-- server 级过滤仍命中 0007 更窄索引，server+instance 过滤命中本索引。
SET @has_idx = (SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE table_schema=DATABASE() AND table_name='traffic_log' AND index_name='idx_traffic_server_instance_id');
SET @sql = IF(@has_idx = 0,
  'ALTER TABLE traffic_log ADD KEY idx_traffic_server_instance_id (server_id, instance_id, id)',
  'SELECT ''traffic_log.idx_traffic_server_instance_id ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
