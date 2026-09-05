// 与后端 internal/model 保持一致的领域类型。

export type ServerStatus = 'unknown' | 'healthy' | 'unhealthy' | 'disabled'
export type Transport = 'stdio' | 'https' | 'sse'
export type PolicyEffect = 'allow' | 'deny'

export interface MCPServer {
  id: string
  name: string
  description?: string
  endpoint: string
  transport: Transport
  version?: string
  enabled: boolean
  health_status: ServerStatus
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

export interface PolicyRule {
  subject: string
  tool: string
  effect: PolicyEffect
}

export interface Policy {
  id: string
  name: string
  rules: PolicyRule[]
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
  grants: ToolGrant[]
  secret?: string // 仅创建响应返回一次
  created_at: string
  updated_at: string
}

export interface TrafficSample {
  request_id: string
  trace_id?: string
  server_id: string
  tool: string
  client?: string
  status: string
  latency_ms: number
  error?: string
  timestamp: string
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