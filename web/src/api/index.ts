import http, { unwrap } from './http'
import type { AccessKey, Credential, MCPServer, MetricSnapshot, Policy, Route, Session, Tool, ToolGrant, TrafficSample } from '@/types'

// ---- Auth（免认证端点）----

export const getAuthStatus = () => unwrap<{ auth_required: boolean }>(http.get('/auth/status'))

// 登录换取会话令牌（仅 auth.enabled 且填 operator_token 时可用）。
export const login = (payload: { username: string; password: string }) =>
  unwrap<Session>(http.post('/auth/login', payload))

// ---- Servers ----

export const listServers = () => unwrap<MCPServer[]>(http.get('/servers'))

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

export const listServerTools = (id: string) => unwrap<Tool[]>(http.get(`/servers/${id}/tools`))

export const listServerCredentials = (id: string) =>
  unwrap<Credential[]>(http.get(`/servers/${id}/credentials`))

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

// ---- Tools / Routes / Policies ----

export const listTools = () => unwrap<Tool[]>(http.get('/tools'))

export const listRoutes = () => unwrap<Route[]>(http.get('/routes'))

export const createRoute = (payload: Partial<Route>) => unwrap<Route>(http.post('/routes', payload))

export const listPolicies = () => unwrap<Policy[]>(http.get('/policies'))

export const createPolicy = (payload: Partial<Policy>) =>
  unwrap<Policy>(http.post('/policies', payload))

// ---- API Keys（访问控制）----

export const listKeys = () => unwrap<AccessKey[]>(http.get('/keys'))

export const createKey = (payload: { name: string; subject: string; qps?: number; burst?: number; grants?: ToolGrant[] }) =>
  unwrap<AccessKey>(http.post('/keys', payload))

export const getKey = (id: string) => unwrap<AccessKey>(http.get(`/keys/${id}`))

export const updateKey = (
  id: string,
  payload: Partial<Pick<AccessKey, 'name' | 'enabled' | 'qps' | 'burst' | 'grants'>>,
) => unwrap<AccessKey>(http.patch(`/keys/${id}`, payload))

export const deleteKey = (id: string) => unwrap<void>(http.delete(`/keys/${id}`))

// ---- Metrics / Logs ----

// getMetrics 读取指标快照；scope='tool'（默认）排除 server: 前缀行，
// scope='server' 只返回按 Server 聚合的行。
export const getMetrics = (scope?: 'tool' | 'server') =>
  unwrap<MetricSnapshot[]>(http.get('/metrics', { params: scope ? { scope } : undefined }))

export const getServerMetrics = () => unwrap<MetricSnapshot[]>(http.get('/metrics', { params: { scope: 'server' } }))

// getLogs 读取最近调用日志；可选按 server 过滤与条数限制。
export const getLogs = (opts?: { server_id?: string; limit?: number }) =>
  unwrap<TrafficSample[]>(http.get('/logs', { params: opts }))