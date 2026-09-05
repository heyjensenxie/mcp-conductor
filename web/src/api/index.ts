import http, { unwrap } from './http'
import type { AccessKey, Credential, MCPServer, MetricSnapshot, Paged, Route, ServerStatus, Session, Tool, ToolGrant, TrafficSample, TrendPoint } from '@/types'

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

export const createServer = (payload: Partial<MCPServer>) =>
  unwrap<MCPServer>(http.post('/servers', payload))

export const getServer = (id: string) => unwrap<MCPServer>(http.get(`/servers/${id}`))

export const toggleServer = (id: string, enabled: boolean) =>
  unwrap<MCPServer>(http.patch(`/servers/${id}/toggle`, { enabled }))

// 更新 Server 可编辑字段（name 不可改，见后端 registry.UpdateServer）。
export const updateServer = (
  id: string,
  payload: Partial<Pick<MCPServer, 'description' | 'endpoint' | 'transport'>>,
) => unwrap<MCPServer>(http.patch(`/servers/${id}`, payload))

export const deleteServer = (id: string) => unwrap<void>(http.delete(`/servers/${id}`))

export const testServer = (id: string) => unwrap<{ status: string }>(http.post(`/servers/${id}/test`))

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

// ---- Metrics / Logs ----

// getMetrics 读取指标快照；scope='tool'（默认）排除 server: 前缀行，
// scope='server' 只返回按 Server 聚合的行。
export const getMetrics = (scope?: 'tool' | 'server') =>
  unwrap<MetricSnapshot[]>(http.get('/metrics', { params: scope ? { scope } : undefined }))

export const getServerMetrics = () => unwrap<MetricSnapshot[]>(http.get('/metrics', { params: { scope: 'server' } }))

// getMetricsTrend 读取真时序趋势（近 N 分钟，按分钟桶）。
export const getMetricsTrend = (scope: 'tool' | 'server' = 'tool', minutes = 30) =>
  unwrap<{ series: TrendPoint[] }>(http.get('/metrics/trend', { params: { scope, minutes } }))

// getLogs 分页读取调用日志，支持 server_id/q/status/from/to 筛选。
export const getLogs = (params?: LogListParams) =>
  unwrap<Paged<TrafficSample>>(http.get('/logs', { params }))

// ---- 全量辅助（page_size=0，供 picker/下拉/弹层等需要完整目录的消费）----

export const listAllServers = async () => (await listServers({ page_size: 0 })).items
export const listAllTools = async () => (await listTools({ page_size: 0 })).items
export const listServerToolsAll = async (id: string) => (await listServerTools(id, { page_size: 0 })).items
export const listServerCredentialsAll = async (id: string) => (await listServerCredentials(id, { page_size: 0 })).items
export const listAllRoutes = async () => (await listRoutes({ page_size: 0 })).items
export const listAllKeys = async () => (await listKeys({ page_size: 0 })).items
