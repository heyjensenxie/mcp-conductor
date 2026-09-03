-- 002：credentials 增加加密值列。
-- 凭证值以 AES-256-GCM 密文（hex 文本）保存，应用层用 credentials.encryption_key
-- 加解密；未配置密钥时 MySQL 存储拒绝落库明文。迁移仅在已执行过 0001 的库上运行。
-- 幂等：先检查列是否存在，避免重复执行报错。

SET @has_col = (SELECT COUNT(*) FROM information_schema.COLUMNS
                WHERE table_schema=DATABASE() AND table_name='credentials' AND column_name='encrypted_value');

SET @sql = IF(@has_col = 0,
    'ALTER TABLE credentials ADD COLUMN encrypted_value MEDIUMTEXT NULL COMMENT ''AES-256-GCM 加密的凭证值（hex），应用层加解密'' AFTER header',
    'SELECT ''column already exists''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;