-- 005：下线 legacy 权限策略（policies / policy_rules）。
-- 授权模型在 v0.1 收口到「Access Key × 工具白名单」，策略规则已无使用方
-- 与代码路径（域模型/存储/接口/Console 一并移除）。0001 已不再创建这两张表；
-- 本迁移只为兼容已跑过 0001 的存量库做一次幂等清理。

SET NAMES utf8mb4;

DROP TABLE IF EXISTS policy_rules;
DROP TABLE IF EXISTS policies;
