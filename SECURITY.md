# Security Policy

## Supported Versions

| Version | Supported |
| --- | --- |
| 1.0.x | ✅ |
| < 1.0 | ❌ |

## Reporting a Vulnerability

**请不要在公开 Issue 中提交安全漏洞。**

请通过私有渠道报告：

- 给维护者发送私信（GitHub）
- 或发送邮件至维护者在仓库公开资料中列出的邮箱

请在报告中包含：

- 影响面描述与复现步骤
- 是否已公开/已被利用
- 建议的修复方向

## Security Notes

MCP Conductor 的安全基线：

- **错误设计与日志**：凭据、Token、完整敏感 MCP payload 一律禁止输出到日志或返回前端；统一错误只暴露 `code/message/request_id`。
- **Credential 边界**：Client→Gateway 与 Gateway→Upstream 两类凭据分离，敏感值不落明文。
- **安全默认值**：`auth` 默认开启；`ratelimit` 与 `redis` 默认关闭，按容量与部署需求显式配置（见 `config.example.yaml`）。
- **依赖**：使用 Go 官方渠道与镜像托管的第三方库，发布前运行 `govulncheck` 与 `npm audit`。

我们会在确认漏洞后 72 小时内回复，并尽快在可复现时发布修复。
