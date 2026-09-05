-- 007：traffic_log 增加 (server_id, id) 复合索引。
-- 管理列表对调用日志最常见的查询是"按 server 等值过滤 + 按 id 倒序取页"
-- （ServerDetail 日志页、Traffic 页 server 筛选）。在 MySQL 5.7 下对升序
-- 复合索引 (server_id, id) 做等值 server + 反向扫 id 即可免 filesort；
-- 不引入降序索引（5.7 契约禁止）。全局（不带 server）按 id 倒序走主键，
-- from/to 时间范围由既有 idx_traffic_ts 承载，故仅新增这一个窄索引。
-- MySQL 5.7 没有 CREATE INDEX IF NOT EXISTS，经 information_schema.STATISTICS
-- 守卫，重复执行安全（追加表仅维护一个额外索引，写放大有限）。

SET NAMES utf8mb4;

SET @has_idx = (SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE table_schema=DATABASE() AND table_name='traffic_log' AND index_name='idx_traffic_server_id_id');
SET @sql = IF(@has_idx = 0,
  'ALTER TABLE traffic_log ADD KEY idx_traffic_server_id_id (server_id, id)',
  'SELECT ''traffic_log.idx_traffic_server_id_id ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
