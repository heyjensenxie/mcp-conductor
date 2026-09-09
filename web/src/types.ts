// 与后端 internal/model 保持一致的领域类型。

// 管理面列表统一分页信封（后端 /api 列表 data 形状）。
export interface Paged<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export type ServerStatus = 'unknown' | 'healthy' | 'unhealthy' | 'disabled'
export type Transport = 'stdio' | 'https' | 'sse'

// 逻辑 Server 下的具体上游实例（endpoint/transport/健康承载单位）。
// endpoint 随 transport 而异：https/sse 为端点 URL，stdio 为可执行命令；
// args 仅 stdio 使用（启动参数数组，不经过 shell）。
export interface ServerInstance {
  id: string
  server_id: string
  endpoint: string
  transport: Transport
  args?: string[]
  enabled: boolean
  health_status: Exclude<ServerStatus, 'disabled'>
  created_at: string
  updated_at: string
}

// 逻辑 Server：不含端点，只承载 name/description/enabled + 聚合健康；
// 端点与传输属于 instances（由后端控制面水合，instances[0] 为主实例）。
export interface MCPServer {
  id: string
  name: string
  description?: string
  enabled: boolean
  health_status: ServerStatus
  instances?: ServerInstance[]
  created_at: string
  updated_at: string
}

export interface Tool {
  id: string
  server_id: string
  original_name: string
  gateway_name: string
  description?: string
  input_schema?: Record<string, unknown>
  source_description?: string
  source_input_schema?: Record<string, unknown>
  name_overridden: boolean
  description_overridden: boolean
  input_schema_overridden: boolean
  risk_level?: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface Route {
  id: string
  name: string
  server_id: string
  tool_names?: string[]
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface Credential {
  id: string
  server_id: string
  name: string
  kind: 'static_token' | 'api_key'
  header?: string
  has_value: boolean
  created_at: string
  updated_at: string
}

// API Key 访问控制：白名单授权 + 按 key 限流 + 调用配置。
export interface ToolGrant {
  gateway_name: string
  headers?: Record<string, string>
  default_args?: Record<string, unknown>
}

export interface AccessKey {
  id: string
  name: string
  subject: string
  enabled: boolean
  qps: number
  burst: number
  window_seconds?: number
  grants: ToolGrant[]
  secret?: string // 仅创建响应返回一次
  created_at: string
  updated_at: string
}

// KeyInvokeResult 是控制面「以某 Key 身份试调用」的返回（POST /api/keys/{id}/invoke）。
// Allowed=false 时后端以 403 信封返回，不产生本数据体；此处覆盖放行后的成功/失败。
export interface KeyInvokeResult {
  allowed: boolean
  is_error?: boolean
  server_id?: string
  instance_id?: string
  latency_ms: number
  error_code?: string
  message?: string
  content?: string
}

// TrafficReplayResult 是「回放捕获调用」的诊断返回（POST /api/logs/{id}/replay）。
export interface TrafficReplayResult {
  server_id?: string
  instance_id?: string
  latency_ms: number
  is_error?: boolean
  error_code?: string
  message?: string
  content?: string
}

export interface TrafficSample {
  id?: number
  request_id: string
  trace_id?: string
  server_id: string
  instance_id?: string
  tool: string
  client?: string
  // client_ip 记录调用方来源 IP（可信代理规则解析；空表示未记录）。
  client_ip?: string
  status: string
  latency_ms: number
  error?: string
  timestamp: string
  // has_args 标记该行是否捕获了入参（启用「回放」的前置条件）。
  has_args?: boolean
}

// TrafficDetail 是单条调用日志详情（含已捕获入参，供回放弹窗）。
export interface TrafficDetail extends TrafficSample {
  request_args?: Record<string, unknown>
}

export interface MetricSnapshot {
  key: string
  totals: number
  success: number
  errors: number
  success_rate: number
  p50: number
  p95: number
  p99: number
}

// 真时序趋势点（分钟级，来自 /api/metrics/trend）。
export interface TrendPoint {
  ts: number
  totals: number
  errors: number
}

// ---- 窗口聚合（GET /api/metrics/window）----
// 服务端在时间窗口内做的聚合：计数/成功率/均值精确，分位延迟由固定桶直方图近似。
// 看板/观测用它替代“拉取日志样本再前端聚合”，口径不受样本截断影响；成本与窗口内
// 行数成正比、与表总量无关。

// MetricsWindowGroup 是某个分组维度（server / tool / client_ip）的窗口统计。
export interface MetricsWindowGroup {
  key: string
  totals: number
  success: number
  errors: number
  success_rate: number
  avg: number
  p50: number
  p95: number
  p99: number
}

// MetricsWindowMinute 是窗口内单个分钟桶（供延迟/成功率趋势）。
export interface MetricsWindowMinute {
  minute: number
  totals: number
  success: number
  errors: number
  avg: number
  p95: number
}

// MetricsWindowStatusCount 是窗口内某个调用状态（success 或错误码）的计数。
export interface MetricsWindowStatusCount {
  status: string
  count: number
}

export interface MetricsWindow {
  from: string
  to: string
  minutes: number
  // by_minute 的桶宽（分钟）：长窗口服务端自动降采样（3 天=10、7 天=30），点数 ≤ 500。
  bucket_minutes: number
  totals: number
  success: number
  errors: number
  success_rate: number
  avg: number
  p50: number
  p95: number
  p99: number
  by_server?: MetricsWindowGroup[]
  by_tool?: MetricsWindowGroup[]
  by_client_ip?: MetricsWindowGroup[]
  by_status?: MetricsWindowStatusCount[]
  by_minute?: MetricsWindowMinute[]
}

// 重新发现预演的单个工具变更（advisory：只读报告，需人工确认后应用）。
export interface RediscoverChange {
  kind: 'add' | 'update'
  original_name: string
  gateway_name: string
  enabled: boolean
  desc_changed: boolean
  schema_changed: boolean
  source_changed: boolean
  desc_protected: boolean
  schema_protected: boolean
  name_protected: boolean
}

// 重新发现预演中因其他 Server 已占用对外名而跳过的工具。
export interface RediscoverConflict {
  gateway_name: string
  owner_server_id: string
  owner_server_name: string
}

// 重新发现预演结果：relative 当前登记的工具变更（POST .../rediscover/plan 返回）。
export interface RediscoverPlan {
  server_id: string
  added: number
  updated: number
  conflict_count: number
  changes: RediscoverChange[]
  conflicts: RediscoverConflict[]
}

// 管理登录会话（POST /api/auth/login 响应，明文 token 仅下发一次）。
export interface Session {
  token: string
  subject: string
  expires_at: string
}

// ---- MCP Evaluation（评测：质量分 + 回归用例，即时计算不落库）----

export type EvalSeverity = 'error' | 'warn' | 'info'

export interface EvalFinding {
  severity: EvalSeverity
  code: string
  message: string
  suggestion?: string
}

export interface EvalInstanceProbe {
  id: string
  endpoint: string
  transport: Transport
  health_status: ServerStatus
}

export interface EvalServerMeta {
  id: string
  name: string
  health_status: ServerStatus
  probed_instance?: EvalInstanceProbe
}

export interface EvalMeta {
  server: Pick<EvalServerMeta, 'id' | 'name' | 'health_status'>
  probe?: EvalInstanceProbe
  stored_tool_count: number
  runtime_metrics_available: boolean
  platform_override_count: number
  issues?: EvalFinding[]
}

export interface EvalToolScores {
  schema: number
  description: number
  naming: number
}

export interface EvalToolCheck {
  tool: string
  gateway_name?: string
  scores: EvalToolScores
  findings: EvalFinding[]
}

export interface EvalDimension {
  key: string
  score: number
  available: boolean
  weight: number
  weighted_contribution: number
  findings?: EvalFinding[]
}

export interface EvalRuntimeInfo {
  available: boolean
  totals: number
  success_rate: number
  p95: number
  errors: number
  note?: string
}

export interface EvalOverrideNote {
  count: number
  message: string
}

export interface EvalReport {
  server: EvalServerMeta
  overall_score: number
  dimensions: EvalDimension[]
  tool_checks: EvalToolCheck[]
  runtime?: EvalRuntimeInfo
  platform_overrides?: EvalOverrideNote
  generated_at: string
}

export interface EvalSuiteCase {
  name: string
  gateway_tool: string
  arguments: Record<string, unknown>
  expected_substring?: string
  timeout_ms?: number
}

export interface EvalSuiteCaseResult {
  name: string
  gateway_tool: string
  original_tool: string
  passed: boolean
  matched: boolean
  latency_ms: number
  error_code?: string
  error?: string
  output_snippet?: string
}

export interface EvalSuiteSummary {
  total: number
  passed: number
  failed: number
  pass_rate: number
  avg_latency_ms: number
  p95_latency_ms: number
}

export interface EvalToolGroup {
  gateway_tool: string
  total: number
  passed: number
  p95_latency_ms: number
}

export interface EvalSuiteResult {
  server_id: string
  server_name: string
  summary: EvalSuiteSummary
  cases: EvalSuiteCaseResult[]
  by_tool: EvalToolGroup[]
}

// ---- 运行期治理配置（防护页：IP 黑名单 + 数据面三级限流阈值）----

// 三级限流配额：0 语义见后端（ip_qps=0→沿用 qps；global_qps<=0→不启用全局级）。
// window_seconds 为滑动窗口长度（秒，默认 60）：任意 N 秒内 ≤ 该级 QPS×N。burst 已废弃。
export interface RuntimeRateLimit {
  qps: number
  burst: number
  window_seconds: number
  ip_qps: number
  ip_burst: number
  global_qps: number
  global_burst: number
}

// 自动封禁：来源在检测窗口内被限流 429 达次数即临时封禁（TTL 自动解封，进程内状态）。
export interface RuntimeAutoBan {
  enabled: boolean
  window_seconds: number
  max_violations: number
  ban_seconds: number
}

// 运行期观测设置（随 RuntimeConfig 单行持久化；observability 缺省=沿用当前有效值）。
export interface RuntimeObservability {
  record_args: boolean
}

// 生效的运行期配置（后台保存值优先，否则为 config.yaml 种子）。
export interface RuntimeConfig {
  ratelimit: RuntimeRateLimit
  auto_ban: RuntimeAutoBan
  ip_blocklist: string[]
  ip_whitelist: string[]
  observability?: RuntimeObservability
  updated_at?: string
}

// GET/PUT /api/runtime-config 的统一视图。
export interface RuntimeConfigView {
  config: RuntimeConfig
  persisted: boolean
  ratelimit_enabled: boolean
}
