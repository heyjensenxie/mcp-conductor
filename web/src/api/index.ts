import http, { unwrap } from './http'
import type { AccessKey, Credential, EvalMeta, EvalReport, EvalSuiteCase, EvalSuiteResult, KeyInvokeResult, MCPServer, MetricSnapshot, Paged, RediscoverPlan, Route, ServerInstance, ServerStatus, Session, Tool, ToolGrant, TrafficSample, Transport, TrendPoint } from '@/types'

// ---- 管理面列表查询（服务端分页 + 筛选）----
//
// 后端 /api 列表统一返回 { items, total, page, page_size }；
// page_size=0 表示"全量模式"（返回全部匹配行），供下拉/弹层等需要完整目录
// 的消费。以下 ListParams 及其子类仅传明确给定的筛选，未设置的字段不发送
// （axios 会忽略 undefined）。

export interface ListParams {
  page?: number
  page_size?: number
}

export interface ServerListParams extends ListParams {
  q?: string
  enabled?: boolean
  health_status?: ServerStatus
}

// 注册 Server 入参：endpoint/transport 是 seed（首个）实例字段。
export interface CreateServerPayload {
  name: string
  description?: string
  endpoint: string
  transport?: Transport
}

export interface ToolListParams extends ListParams {
  q?: string
  server_id?: string
  enabled?: boolean
}

export interface RouteListParams extends ListParams {
  q?: string
  server_id?: string
  enabled?: boolean
}

export interface CredentialListParams extends ListParams {
  q?: string
  kind?: Credential['kind']
  has_value?: boolean
}

export interface KeyListParams extends ListParams {
  q?: string
  enabled?: boolean
}

export interface LogListParams extends ListParams {
  q?: string
  server_id?: string
  instance_id?: string
  status?: string
  from?: string
  to?: string
}

// ---- Auth（免认证端点）----

export const getAuthStatus = () => unwrap<{ auth_required: boolean }>(http.get('/auth/status'))

// 登录换取会话令牌（仅 auth.enabled 且填 operator_token 时可用）。
export const login = (payload: { username: string; password: string }) =>
  unwrap<Session>(http.post('/auth/login', payload))

// ---- Servers ----

export const listServers = (params?: ServerListParams) =>
  unwrap<Paged<MCPServer>>(http.get('/servers', { params }))

export const createServer = (payload: CreateServerPayload) =>
  unwrap<MCPServer>(http.post('/servers', payload))

export const getServer = (id: string) => unwrap<MCPServer>(http.get(`/servers/${id}`))

export const toggleServer = (id: string, enabled: boolean) =>
  unwrap<MCPServer>(http.patch(`/servers/${id}/toggle`, { enabled }))

// 更新 Server 逻辑字段（name/endpoint/transport 不可改；端点属于实例）。
export const updateServer = (id: string, payload: Partial<Pick<MCPServer, 'description'>>) =>
  unwrap<MCPServer>(http.patch(`/servers/${id}`, payload))

export const deleteServer = (id: string) => unwrap<void>(http.delete(`/servers/${id}`))

export const testServer = (id: string) => unwrap<{ status: string }>(http.post(`/servers/${id}/test`))

// previewRediscover 只读预演：重新发现但不落库，返回相对当前登记的变更清单，
// 供前端弹窗对比；人工确认后再 POST /servers/:id/test 真正应用。
export const previewRediscover = (id: string) =>
  unwrap<RediscoverPlan>(http.post(`/servers/${id}/rediscover/plan`))

// 主实例（最早创建）端点，用于列表/详情展示。
export const primaryEndpoint = (s: MCPServer): string => s.instances?.[0]?.endpoint ?? ''
export const primaryTransport = (s: MCPServer): string => s.instances?.[0]?.transport ?? ''

// ---- Server 实例（多实例负载均衡）----

export const listServerInstances = (id: string) =>
  unwrap<ServerInstance[]>(http.get(`/servers/${id}/instances`))

export const createServerInstance = (id: string, payload: { endpoint: string; transport?: Transport }) =>
  unwrap<ServerInstance>(http.post(`/servers/${id}/instances`, payload))

export const updateServerInstance = (
  id: string,
  iid: string,
  payload: Partial<Pick<ServerInstance, 'endpoint' | 'transport'>>,
) => unwrap<ServerInstance>(http.patch(`/servers/${id}/instances/${iid}`, payload))

export const toggleServerInstance = (id: string, iid: string, enabled: boolean) =>
  unwrap<ServerInstance>(http.patch(`/servers/${id}/instances/${iid}/toggle`, { enabled }))

export const testServerInstance = (id: string, iid: string) =>
  unwrap<ServerInstance>(http.post(`/servers/${id}/instances/${iid}/test`))

export const deleteServerInstance = (id: string, iid: string) =>
  unwrap<void>(http.delete(`/servers/${id}/instances/${iid}`))

// ---- Server Tools / Credentials ----

export const listServerTools = (id: string, params?: ToolListParams) =>
  unwrap<Paged<Tool>>(http.get(`/servers/${id}/tools`, { params }))

export const listServerCredentials = (id: string, params?: CredentialListParams) =>
  unwrap<Paged<Credential>>(http.get(`/servers/${id}/credentials`, { params }))

export const createCredential = (
  id: string,
  payload: { name: string; kind: 'api_key' | 'static_token'; header: string; value: string },
) => unwrap<Credential>(http.post(`/servers/${id}/credentials`, payload))

export const updateCredential = (
  id: string,
  credId: string,
  payload: Partial<{ name: string; kind: 'api_key' | 'static_token'; header: string; value: string }>,
) => unwrap<Credential>(http.patch(`/servers/${id}/credentials/${credId}`, payload))

export const deleteCredential = (id: string, credId: string) =>
  unwrap<void>(http.delete(`/servers/${id}/credentials/${credId}`))

// ---- Tools / Routes ----

export const listTools = (params?: ToolListParams) =>
  unwrap<Paged<Tool>>(http.get('/tools', { params }))

export const getTool = (id: string) => unwrap<Tool>(http.get(`/tools/${id}`))

export const toggleTool = (id: string, enabled: boolean) =>
  unwrap<Tool>(http.patch(`/tools/${id}/toggle`, { enabled }))

export const updateTool = (id: string, payload: Partial<Pick<Tool, 'gateway_name' | 'description' | 'input_schema'>> & {
  reset_name?: boolean
  reset_description?: boolean
  reset_input_schema?: boolean
}) => unwrap<Tool>(http.patch(`/tools/${id}`, payload))

export const listRoutes = (params?: RouteListParams) =>
  unwrap<Paged<Route>>(http.get('/routes', { params }))

export const createRoute = (payload: Partial<Route>) => unwrap<Route>(http.post('/routes', payload))

export const updateRoute = (id: string, payload: Partial<Pick<Route, 'name' | 'server_id' | 'tool_names' | 'enabled'>>) =>
  unwrap<Route>(http.patch(`/routes/${id}`, payload))

export const toggleRoute = (id: string, enabled: boolean) =>
  unwrap<Route>(http.patch(`/routes/${id}/toggle`, { enabled }))

export const deleteRoute = (id: string) => unwrap<void>(http.delete(`/routes/${id}`))

// ---- API Keys（访问控制）----

export const listKeys = (params?: KeyListParams) =>
  unwrap<Paged<AccessKey>>(http.get('/keys', { params }))

export const createKey = (payload: { name: string; subject: string; qps?: number; burst?: number; grants?: ToolGrant[] }) =>
  unwrap<AccessKey>(http.post('/keys', payload))

export const getKey = (id: string) => unwrap<AccessKey>(http.get(`/keys/${id}`))

export const updateKey = (
  id: string,
  payload: Partial<Pick<AccessKey, 'name' | 'enabled' | 'qps' | 'burst' | 'grants'>>,
) => unwrap<AccessKey>(http.patch(`/keys/${id}`, payload))

// rotateKey 重置 API Key 明文密钥：新密钥仅在本响应返回一次（与创建同契约）。
export const rotateKey = (id: string) =>
  unwrap<AccessKey & { secret?: string }>(http.post(`/keys/${id}/rotate`))

export const deleteKey = (id: string) => unwrap<void>(http.delete(`/keys/${id}`))

// invokeKey 以某 API Key 身份端到端试调用一次（Operator 触发；授权拒绝由 HTTP
// 403 信封给出，未授权/禁用 key 不返回数据体）。
export const invokeKey = (id: string, payload: { gateway_tool: string; arguments?: Record<string, unknown> }) =>
  unwrap<KeyInvokeResult>(http.post(`/keys/${id}/invoke`, payload))

// ---- Metrics / Logs ----

// getMetrics 读取指标快照；默认（tool）排除 server:/instance: 前缀行，
// scope='server' 只返回按 Server 聚合的行，scope='instance' 只返回实例维行。
export const getMetrics = (scope?: 'tool' | 'server' | 'instance') =>
  unwrap<MetricSnapshot[]>(http.get('/metrics', { params: scope ? { scope } : undefined }))

export const getServerMetrics = () => unwrap<MetricSnapshot[]>(http.get('/metrics', { params: { scope: 'server' } }))

// getInstanceMetrics 返回某 Server 各实例的指标行（供 Server 详情实例表展示）。
export const getInstanceMetrics = (serverId: string) =>
  unwrap<MetricSnapshot[]>(http.get('/metrics', { params: { scope: 'instance', server_id: serverId } }))

// getMetricsTrend 读取真时序趋势（近 N 分钟，按分钟桶）。
export const getMetricsTrend = (scope: 'tool' | 'server' = 'tool', minutes = 30) =>
  unwrap<{ series: TrendPoint[] }>(http.get('/metrics/trend', { params: { scope, minutes } }))

// getLogs 分页读取调用日志，支持 server_id/instance_id/q/status/from/to 筛选。
// 注意：traffic_log 为流水大表，服务端对 page_size 设上限（200）且不支持 0=全量，
// Dashboard/Traffic 均按最近分页拉取。
export const getLogs = (params?: LogListParams) =>
  unwrap<Paged<TrafficSample>>(http.get('/logs', { params }))

// ---- MCP Evaluation（评测）----

// instanceId 可选：定向某实例评测（缺省为空=首个可拨测实例）。

// getEvalMeta 读取某 Server 的评测概况（拨测实例/工具数/是否有流量/覆盖数）。
export const getEvalMeta = (id: string, instanceId?: string) =>
  unwrap<EvalMeta>(http.get(`/evaluations/servers/${id}`, { params: instanceId ? { instance_id: instanceId } : undefined }))

// runEvalQuality 对某 Server 现场拨测并返回 MCP 质量报告（即时，不落库）。
export const runEvalQuality = (id: string, instanceId?: string) =>
  unwrap<EvalReport>(http.post(`/evaluations/servers/${id}/quality`, {}, { params: instanceId ? { instance_id: instanceId } : undefined }))

// runEvalSuite 执行一组回归用例并返回汇总（即时，不落库）。
export const runEvalSuite = (id: string, cases: EvalSuiteCase[], instanceId?: string) =>
  unwrap<EvalSuiteResult>(http.post(`/evaluations/servers/${id}/suite`, { cases }, { params: instanceId ? { instance_id: instanceId } : undefined }))

// ---- 全量辅助（page_size=0，供 picker/下拉/弹层等需要完整目录的消费）----

export const listAllServers = async () => (await listServers({ page_size: 0 })).items
export const listAllTools = async () => (await listTools({ page_size: 0 })).items
export const listServerToolsAll = async (id: string) => (await listServerTools(id, { page_size: 0 })).items
export const listServerCredentialsAll = async (id: string) => (await listServerCredentials(id, { page_size: 0 })).items
export const listAllRoutes = async () => (await listRoutes({ page_size: 0 })).items
export const listAllKeys = async () => (await listKeys({ page_size: 0 })).items
