-- 0008: Server 多实例化 —— Server != Instance 落地。
--
-- 新增 server_instances 表承载逻辑 Server 下的具体上游实例（endpoint/transport/
-- 健康）。tools / credentials / routes 仍挂在逻辑 servers(id) 上不变。
--
-- servers 表遗留的 endpoint/transport 列从 0008 起不再由应用读写：
-- endpoint 改可空（新插入省略），transport 保留默认 'https'，仅作安全降级与
-- 观测留档，单一事实源是 server_instances。
--
-- 兼容目标：MySQL 5.7。可重复执行（幂等）。回填幂等由 LEFT JOIN … IS NULL 保证。

CREATE TABLE IF NOT EXISTS server_instances (
  id            VARCHAR(64)   NOT NULL COMMENT '对外稳定 id（内存/MySQL 前缀 inst-）',
  server_id     VARCHAR(64)   NOT NULL COMMENT '所属逻辑 Server',
  endpoint      VARCHAR(2048) NOT NULL COMMENT '上游端点 URL',
  transport     VARCHAR(16)   NOT NULL DEFAULT 'https' COMMENT 'https | sse | stdio',
  enabled       TINYINT(1)    NOT NULL DEFAULT 1 COMMENT '实例级启停（摘除/排空）',
  health_status VARCHAR(16)   NOT NULL DEFAULT 'unknown' COMMENT 'unknown | healthy | unhealthy',
  created_at    DATETIME(3)   NOT NULL,
  updated_at    DATETIME(3)   NOT NULL,
  PRIMARY KEY (id),
  KEY idx_server_instances_server (server_id),
  CONSTRAINT fk_server_instances_server FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='逻辑 Server 的上游实例';

-- 回填：既有 Server 各转一条实例。id 复用 server id（server id 全局唯一；
-- 新实例使用 'inst-' 前缀，与 'srv-' 前缀互斥，不冲突）。实例一律 enabled、
-- health_status=unknown，交由启动时的健康巡检立即接管。
INSERT INTO server_instances (id, server_id, endpoint, transport, enabled, health_status, created_at, updated_at)
SELECT s.id, s.id, s.endpoint, s.transport, 1, 'unknown', s.created_at, s.updated_at
FROM servers s
LEFT JOIN server_instances si ON si.server_id = s.id
WHERE si.server_id IS NULL;

-- servers.endpoint 改可空：应用层建 Server 不再写端点（省略该列）。
SET @endpoint_nullable = (SELECT IS_NULLABLE FROM information_schema.COLUMNS
  WHERE table_schema = DATABASE() AND table_name = 'servers' AND column_name = 'endpoint');
SET @sql = IF(@endpoint_nullable = 'NO',
  'ALTER TABLE servers MODIFY COLUMN endpoint VARCHAR(2048) NULL COMMENT ''遗留列：端点已下沉 server_instances（自 0008 起应用不再读写）''',
  'SELECT ''servers.endpoint already nullable''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
