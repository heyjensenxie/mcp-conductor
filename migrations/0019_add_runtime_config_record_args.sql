-- 0019: runtime_config 增加"入参捕获"列 record_args（运行期动态开关）。
--
-- 背景：observability.record_args（Traffic 回放的前提）原本只能改 config.yaml
-- 重启生效；本迁移将其纳入运行期治理配置单行快照，后台 PUT /api/runtime-config
-- 即可动态切换。config.yaml 仅作无存值种子；列缺失时读取报错 → 缓存回退种子
-- （与 auto_ban 等既有列的降级行为一致）。
-- 兼容目标：MySQL 5.7。可重复执行（幂等）：information_schema 守卫
-- （5.7 无 ADD COLUMN IF NOT EXISTS）。
SET NAMES utf8mb4;

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE table_schema=DATABASE() AND table_name='runtime_config' AND column_name='record_args');
SET @sql = IF(@has_col = 0,
  'ALTER TABLE runtime_config ADD COLUMN record_args TINYINT(1) NOT NULL DEFAULT 0 COMMENT ''tools/call 入参捕获开关（0=关，默认；1=开）''',
  'SELECT ''runtime_config.record_args ok''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;