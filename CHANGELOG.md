# Changelog

本项目的重要变更记录在此文件中，版本遵循 [Semantic Versioning](https://semver.org/)。

## [Unreleased]

### Added

- **保存前测试连接**：新增无副作用接口 `POST /api/servers/test-connection`（草稿
  `{server_id?,name,endpoint,transport,args,headers}`）：执行 MCP initialize 握手并校验
  endpoint/transport/stdio args；不创建 Server/实例、不刷新 Tools、不更新健康状态、不保存
  临时 Header。编辑已有 Server 时自动加载已保存凭据，请求体临时 Header 覆盖同名已保存 Header。
- 后端抽象 `registry.DraftInstanceProber`（`mcpclient.Adapter` 实现 `CheckDraft`），复用既有
  dial 逻辑完成握手。

### Changed

- 新增/编辑 Server 使用同一套表单：name/description（逻辑 Server）+ endpoint/transport/args
  （主实例）+ 多条请求头。请求头异步回显已保存凭据（密钥值不从后端返回，仅显示"已配置；
  留空保留原值"）。
- Server 名称改为可修改（展示名）：对外稳定标识是 Server ID，改名不触碰实例/工具，
  工具对外名来自上游原名而非 Server 命名空间。
- `PATCH /api/servers/{id}` 现在可同时更新 `name`/`description` 与主实例
  `endpoint/transport/args`（主实例配置变更后健康复位并即时探活）；其余实例仍通过实例接口维护。
- 请求头保存按"已有凭证/新增行/删除行"分别处理：已有凭证留空密钥表示保留原值、填值表示替换，
  删除行按 Credential ID 删除，不再重复创建已有凭证。

### Performance

- **看板统计改为服务端聚合**：新增 `GET /api/metrics/window`（计数/成功率/平均延迟精确、分位由
  固定桶直方图近似、分组按调用量截断）。控制台不再把窗口内日志样本（截断到 200 条）拉到浏览器
  聚合——此前窗口内超过 200 次调用时总量/成功率/分位/工具榜单都只是样本值，且成本随流量线性增长。
- **趋势查询走索引**：`trend_minute` 的窗口读取此前只按 `scope` 前缀过滤，会扫描该 scope 在保留期
  内的全部行再 filesort（tool 维可达数百万行）；现在显式带上 `server_id` 命中
  `(scope, server_id, minute)`，并新增 `(scope, minute)` 索引覆盖"全部 Server"场景。`make db-migrate`
  会幂等补齐新索引。
- **内存存储分页不再全量物化**：`QueryTraffic` 只物化请求页的行（此前把全部匹配行复制一遍再切页，
  50k 行规模下单次请求产生 ~47MB 垃圾、耗时 13.8ms → 现在 34µs / 18KB）。
- **指标快照分位缓存**：`/api/metrics` 不再每次请求都复制并排序最多 4096 个延迟样本，改为按需重建
  排序缓存（200 维度下 1.44ms/0.9MB → 0.13ms/70KB）。
- **看板支持 3 天 / 7 天窗口**：窗口档位扩展为 30 分钟 / 1 小时 / 6 小时 / 1 天 / 3 天 / 7 天，
  图表与统计同源（`/api/metrics/window`）。长窗口在服务端按整齐桶宽降采样
  （3 天=10 分钟/桶、7 天=30 分钟/桶，点数 ≤ 500），避免 4320/10080 个点的响应体与渲染开销；
  `/api/metrics/trend` 同步支持降采样（`bucket_minutes` 返回实际桶宽）。
- 访问控制：Access Key 名称支持修改（列表行内「修改名称」与详情页标题内联编辑；name 是展示名，
  稳定标识仍是 id/subject，改名不影响授权与密钥），空名称被拒绝。
- 访问控制详情：平台工具目录按 MCP Server 分组可折叠（分组头点击折叠/展开、全部展开/折叠，
  分组头显示该 Server 已授权/总数，搜索时自动展开命中分组）。

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
