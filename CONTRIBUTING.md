# Contributing to MCP Conductor

感谢你对 MCP Conductor 的贡献！请先阅读 [README](README.md) 与 [架构概览](docs/architecture/overview.md)。

## 开发流程

1. Fork 并创建功能分支（如 `feat/redis-rate-limit`）。
2. 本地启动最小闭环（见 README Quick Start）。
3. 编码约定：
   - 后端 Go：idiomatic，核心模块面向 interface 但避免 interface 滥用；新增依赖前先判断必要性。
   - 领域模型优先，不从 Controller+CRUD 开始堆代码。
   - 统一错误模型与结构化日志；**日志禁止输出 Credential 与完整敏感 MCP payload**。
   - 修改后运行 `go vet ./...` 与 `go test ./...`。
   - 前端：遵循现有 Vue3 + TS + Pinia + Ant Design Vue 结构，保持基础设施管理平台风格。
4. 提交信息清晰，避免无关改动混入。

## 测试要求

- 新增/变更核心逻辑（Router、Balancer、Rate Limiter、Policy、Tool Namespace 等）须带单元测试。
- 聚合相关改动尤其要覆盖 **Tool Name Collision** 回归用例。

## IDE/工作台

项目上下文（长期约束、决策、Agent 规则）维护在中央 AI Workspace 仓库 `projects/mcp-conductor/`；本仓库保持纯净源码，不落地工作台副本。

## Code of Conduct

参与即视为同意 [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)。