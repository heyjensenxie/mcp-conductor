import http, { unwrap } from './http'
import type { Credential, MCPServer, MetricSnapshot, Policy, Route, Tool, TrafficSample } from '@/types'

// ---- Servers ----

export const listServers = () => unwrap<MCPServer[]>(http.get('/servers'))

export const createServer = (payload: Partial<MCPServer>) =>
  unwrap<MCPServer>(http.post('/servers', payload))

export const getServer = (id: string) => unwrap<MCPServer>(http.get(`/servers/${id}`))

export const toggleServer = (id: string, enabled: boolean) =>
  unwrap<MCPServer>(http.patch(`/servers/${id}/toggle`, { enabled }))

export const deleteServer = (id: string) => unwrap<void>(http.delete(`/servers/${id}`))

export const testServer = (id: string) => unwrap<{ status: string }>(http.post(`/servers/${id}/test`))

export const listServerTools = (id: string) => unwrap<Tool[]>(http.get(`/servers/${id}/tools`))

export const listServerCredentials = (id: string) =>
  unwrap<Credential[]>(http.get(`/servers/${id}/credentials`))

// ---- Tools / Routes / Policies ----

export const listTools = () => unwrap<Tool[]>(http.get('/tools'))

export const listRoutes = () => unwrap<Route[]>(http.get('/routes'))

export const createRoute = (payload: Partial<Route>) => unwrap<Route>(http.post('/routes', payload))

export const listPolicies = () => unwrap<Policy[]>(http.get('/policies'))

export const createPolicy = (payload: Partial<Policy>) =>
  unwrap<Policy>(http.post('/policies', payload))

// ---- Metrics / Logs ----

export const getMetrics = () => unwrap<MetricSnapshot[]>(http.get('/metrics'))

export const getLogs = () => unwrap<TrafficSample[]>(http.get('/logs'))