-- 004：工具门面元数据覆盖。源字段保存最近一次上游发现值，override 标记
-- 决定重新发现时是否保留平台侧名称、描述与入参 Schema。
-- MySQL 5.7 没有 ADD COLUMN IF NOT EXISTS，逐列通过 information_schema 守卫。

SET NAMES utf8mb4;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE table_schema=DATABASE() AND table_name='tools' AND column_name='source_description');
SET @sql = IF(@has_col = 0, 'ALTER TABLE tools ADD COLUMN source_description TEXT NULL AFTER input_schema', 'SELECT ''source_description exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE table_schema=DATABASE() AND table_name='tools' AND column_name='source_input_schema');
SET @sql = IF(@has_col = 0, 'ALTER TABLE tools ADD COLUMN source_input_schema TEXT NULL AFTER source_description', 'SELECT ''source_input_schema exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE table_schema=DATABASE() AND table_name='tools' AND column_name='name_overridden');
SET @sql = IF(@has_col = 0, 'ALTER TABLE tools ADD COLUMN name_overridden TINYINT(1) NOT NULL DEFAULT 0 AFTER source_input_schema', 'SELECT ''name_overridden exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE table_schema=DATABASE() AND table_name='tools' AND column_name='description_overridden');
SET @sql = IF(@has_col = 0, 'ALTER TABLE tools ADD COLUMN description_overridden TINYINT(1) NOT NULL DEFAULT 0 AFTER name_overridden', 'SELECT ''description_overridden exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE table_schema=DATABASE() AND table_name='tools' AND column_name='input_schema_overridden');
SET @sql = IF(@has_col = 0, 'ALTER TABLE tools ADD COLUMN input_schema_overridden TINYINT(1) NOT NULL DEFAULT 0 AFTER description_overridden', 'SELECT ''input_schema_overridden exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

UPDATE tools SET source_description = description WHERE source_description IS NULL;
UPDATE tools SET source_input_schema = input_schema WHERE source_input_schema IS NULL;
