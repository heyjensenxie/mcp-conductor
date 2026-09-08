# Changelog

本项目的重要变更记录在此文件中，版本遵循 [Semantic Versioning](https://semver.org/)。

## [1.0.0] - 2026-09-08

首个公开稳定版本。

### Added

- MCP Server 注册、工具自动发现、命名空间聚合与统一 `/mcp` 端点。
- 多实例健康感知 Round-Robin、Route 覆盖和 HTTP/stdio 上游传输。
- Console 登录、管理令牌、数据面 Access Key、工具级授权与运行期限流治理。
- MySQL 5.7+ 持久化、上游凭据加密、流量日志、分钟趋势、Traffic Replay。
- Server 质量评分、回归用例评测、Vue Console 和单二进制/Docker 交付。

### Fixed

- stdio 会话不再由首次请求的 Context 意外终止；阻塞请求仍会按调用 Context 超时并回收子进程。

### Security

- 默认启用认证；敏感凭据不返回列表接口、不写日志，上游凭据使用 AES-256-GCM 加密落库。
- 默认不记录工具参数，启用 Traffic Replay 入参捕获时受采样与保留策略约束。
- 升级 `vue-i18n` 至 9.14.5、ECharts 至 6.1.0，修复公开的 DOM XSS 风险。

[1.0.0]: https://github.com/heyjensenxie/mcp-conductor/releases/tag/v1.0.0
