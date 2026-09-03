// Package model 定义 MCP Conductor 的核心领域模型。
//
// 遵循"领域模型优先"原则（见 PRD 第五节），MVP 覆盖 Server、Tool、Route、
// Policy、Credential、Traffic；Evaluation、Dataset、TestCase、Version 仅预留边界。
package model

import "time"

// Transport 描述 MCP Server 使用的传输方式。
type Transport string

const (
	// TransportStdio 表示通过标准输入输出进程式调用（MVP 可先不接入）。
	TransportStdio Transport = "stdio"
	// TransportStreamableHTTP 表示 streamable HTTP 传输（MCP 现行主流）。
	TransportStreamableHTTP Transport = "https"
	// TransportSSE 表示 HTTP+SSE 传输（兼容旧客户端）。
	TransportSSE Transport = "sse"
)

// ServerStatus 表示 Server 健康/运行状态。
type ServerStatus string

const (
	// ServerStatusUnknown 尚未完成健康检查。
	ServerStatusUnknown ServerStatus = "unknown"
	// ServerStatusHealthy 健康检查通过。
	ServerStatusHealthy ServerStatus = "healthy"
	// ServerStatusUnhealthy 健康检查失败。
	ServerStatusUnhealthy ServerStatus = "unhealthy"
	// ServerStatusDisabled 被管理员禁用。
	ServerStatusDisabled ServerStatus = "disabled"
)

// Server 代表一个逻辑 MCP Server。
//
// 逻辑 Server 不等于 Server Instance：未来同一逻辑 Server 可能对应多个实例，
// 负载均衡将作用于实例维度，当前 MVP 使用 endpoint 单实例承载。
type Server struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Description  string       `json:"description,omitempty"`
	Endpoint     string       `json:"endpoint"`
	Transport    Transport    `json:"transport"`
	Version      string       `json:"version,omitempty"`
	Enabled      bool         `json:"enabled"`
	HealthStatus ServerStatus `json:"health_status"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// Tool 代表某个 Server 暴露的 MCP Tool。
//
// Gateway 聚合多个 Server 后必须解决 Tool Name Collision，因此对外暴露
// gateway_name（采用 Server namespace），original_name 保留上游原始名。
type Tool struct {
	ID           string `json:"id"`
	ServerID     string `json:"server_id"`
	OriginalName string `json:"original_name"`
	// GatewayName 是面向客户端暴露的名称，例如 university.search_policy。
	GatewayName string `json:"gateway_name"`
	Description string `json:"description,omitempty"`
	// InputSchema 是上游 tools/list 返回的 JSON Schema（序列化形式）。
	InputSchema map[string]any `json:"input_schema,omitempty"`
	RiskLevel   string         `json:"risk_level,omitempty"`
	Enabled     bool           `json:"enabled"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Route 描述"对外工具（或匹配规则）→ 目标 Server"的路由定义。
// MVP 由 Tool 唯一映射到 Server；未来支持多 Server、分组与灰度。
type Route struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ServerID  string    `json:"server_id"`
	ToolNames []string  `json:"tool_names,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PolicyEffect 表示策略对某类访问的最终裁定。
type PolicyEffect string

const (
	PolicyEffectAllow PolicyEffect = "allow"
	PolicyEffectDeny  PolicyEffect = "deny"
)

// PolicyRule 表示一条"主体/对象"权限规则。MVP 采用简单 RBAC：
// 规则作用于 Tool（GatewayName）或通配，主体是调用凭据身份。
type PolicyRule struct {
	Subject string       `json:"subject"` // 主体标识，如 api-key 名称
	Tool    string       `json:"tool"`    // 支持前缀通配，如 "university.*"
	Effect  PolicyEffect `json:"effect"`
}

// Policy 是一组权限规则，可按 for对工具调用进行 allow/deny 裁定。
type Policy struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Rules     []PolicyRule `json:"rules"`
	Enabled   bool         `json:"enabled"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// CredentialKind 表示 Gateway 调用上游 MCP Server 时使用的凭据类型。
type CredentialKind string

const (
	// CredentialStaticToken 静态 Bearer Token。
	CredentialStaticToken CredentialKind = "static_token"
	// CredentialAPIKey 自定义 header 的 API Key。
	CredentialAPIKey CredentialKind = "api_key"
)

// Credential 保存 Gateway→Upstream MCP 的凭据元数据。
// 敏感值禁止明文返回前端、禁止写入日志；value 仅在后端协商阶段使用。
type Credential struct {
	ID       string         `json:"id"`
	ServerID string         `json:"server_id"`
	Name     string         `json:"name"`
	Kind     CredentialKind `json:"kind"`
	// Header 是 api_key 类型时的注入 header 名。
	Header string `json:"header,omitempty"`
	// HasValue 仅标记 value 是否已配置，避免序列化暴露明文。
	HasValue  bool      `json:"has_value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TrafficSample 是一条工具调用观测记录（Request Logging / Audit 的落库结构）。
type TrafficSample struct {
	RequestID string    `json:"request_id"`
	TraceID   string    `json:"trace_id,omitempty"`
	ServerID  string    `json:"server_id"`
	Tool      string    `json:"tool"`
	Client    string    `json:"client,omitempty"`
	Status    string    `json:"status"`
	LatencyMS int64     `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
