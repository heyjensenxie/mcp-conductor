-- 006：自由文本"描述"列扩容为 TEXT。
-- 上游 MCP 工具描述可能远超 VARCHAR(512)（MySQL Error 1406 Data too long for
-- column 'description'），导致单个工具保存失败并中断整次工具发现/连接测试。
-- 工具对外描述 (tools.description)、最近一次上游发现值 (tools.source_description)
-- 与逻辑 Server 描述 (servers.description) 一并改为 TEXT —— 与 input_schema
-- （JSON Schema 同样可能超长）容量一致；应用层按原样存取，不做截断。
-- 命名 / 门面名等有界标识列仍保留 VARCHAR。
-- MySQL 5.7 没有 MODIFY ... IF NOT EXISTS，逐列经 information_schema 守卫：
-- 仅当列仍为 varchar 时才执行 MODIFY，重复执行安全（列已为 TEXT 时跳过）。

SET NAMES utf8mb4;

-- tools.description
SET @is_vc = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='tools' AND column_name='description' AND data_type='varchar');
SET @sql = IF(@is_vc > 0, 'ALTER TABLE tools MODIFY COLUMN description TEXT NULL', 'SELECT ''tools.description ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- tools.source_description
SET @is_vc = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='tools' AND column_name='source_description' AND data_type='varchar');
SET @sql = IF(@is_vc > 0, 'ALTER TABLE tools MODIFY COLUMN source_description TEXT NULL', 'SELECT ''tools.source_description ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- servers.description
SET @is_vc = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='servers' AND column_name='description' AND data_type='varchar');
SET @sql = IF(@is_vc > 0, 'ALTER TABLE servers MODIFY COLUMN description TEXT NULL', 'SELECT ''servers.description ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
