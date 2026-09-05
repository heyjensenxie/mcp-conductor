// Package model 定义 MCP Conductor 的核心领域模型。
//
// 遵循"领域模型优先"原则（见 PRD 第五节），MVP 覆盖 Server、Tool、Route、
// Policy、Credential、Traffic；Evaluation、Dataset、TestCase、Version 仅预留边界。
package model

import (
	"strings"
	"time"
)

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
// 敏感值（Value）仅应用内部使用：不通过 API 下发（json:"-"）、不写入日志；
// has_value 只标记是否已配置，避免序列化暴露明文。
type Credential struct {
	ID       string         `json:"id"`
	ServerID string         `json:"server_id"`
	Name     string         `json:"name"`
	Kind     CredentialKind `json:"kind"`
	// Header 是 api_key 类型时的注入 header 名。
	Header string `json:"header,omitempty"`
	// HasValue 仅标记 value 是否已配置。
	HasValue bool `json:"has_value"`
	// Value 是敏感凭证值（上游注入用）。json:"-" 保证不随 API 下发。
	Value     string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ToolGrant 是"某个 key 对某个工具"的白名单条目（含调用配置）。
//
// GatewayName 采用对外名（server.tool），支持 "*" 与 "server.*" 前缀通配，
// 以表达跨 Server 聚合后的授权集合；Headers 用于调用该工具时附加的请求头
// （部分工具需要独立的多于 Server 级凭证的鉴权头），DefaultArgs 用于在调用
// 时注入固定参数（客户端同名参数可覆盖）。
type ToolGrant struct {
	GatewayName string            `json:"gateway_name"`
	Headers     map[string]string `json:"headers,omitempty"`
	DefaultArgs map[string]any    `json:"default_args,omitempty"`
}

// AccessKey 是 API Key 调用方的完整配置，作为认证/授权/限流的主体载体。
//
// 语义：被管理的 key 采用白名单模式——只能看到/调用 Grants 内命中的工具；
// QPS/Burst 为该 key 独立限流配额，0 表示沿用全局 ratelimit 配置。
// KeyHash 存储 HMAC-SHA256(key)，Secret 为原始密钥明文，两者均 json:"-"
// 不随任何 API 序列化下发；明文只在创建成功时经专用响应体返回一次。
type AccessKey struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Subject   string      `json:"subject"` // 唯一主体标识（日志/授权/限流维度）
	Enabled   bool        `json:"enabled"`
	QPS       int         `json:"qps"`   // 0 → 全局默认
	Burst     int         `json:"burst"` // 0 → 全局默认
	Grants    []ToolGrant `json:"grants"`
	KeyHash   string      `json:"-"` // HMAC-SHA256 哈希，落库不下发
	Secret    string      `json:"-"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// bestGrant 返回与工具（GatewayName）命中的白名单条目中"最特异"的那条；
// 特异性 = 模式字面量匹配长度：精确工具名 > 更长前缀 > 更短前缀 > "*"。
// 匹配结果与 Grants 声明/存储顺序无关（修复 MySQL ORDER BY gateway_name
// 把通配排在精确项之前导致的配置遮蔽）；同特异并列时取先声明者
// （重复条目属冗余配置，不改变授权语义）。
func (k *AccessKey) bestGrant(tool string) *ToolGrant {
	var best *ToolGrant
	bestSpec := -1
	for i := range k.Grants {
		spec, ok := toolMatchSpec(k.Grants[i].GatewayName, tool)
		if ok && spec > bestSpec {
			best = &k.Grants[i]
			bestSpec = spec
		}
	}
	return best
}

// Granted 判定该 key 是否被授权调用工具（GatewayName），支持 "*" 与 "server.*" 通配。
func (k *AccessKey) Granted(tool string) bool {
	return k.bestGrant(tool) != nil
}

// GrantFor 返回与工具（GatewayName）命中的最特异白名单条目及调用配置；无匹配返回 nil。
func (k *AccessKey) GrantFor(tool string) *ToolGrant {
	return k.bestGrant(tool)
}

// toolMatchSpec 判断模式是否与工具名匹配并返回特异性分值：
// "*" 全匹配（0）；"server.*" 前缀通配命中同一 Server 全部工具（得分为
// 前缀字面量长度）；其余须精确相等（得分为全串长度）。ok=false 表示未命中。
func toolMatchSpec(pattern, tool string) (spec int, ok bool) {
	if pattern == "*" {
		return 0, true
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := pattern[:len(pattern)-1] // 去掉尾部 "*"，保留结尾 "."，如 "svc.prod."
		if strings.HasPrefix(tool, prefix) {
			return len(prefix), true
		}
		return 0, false
	}
	if pattern == tool {
		return len(pattern), true
	}
	return 0, false
}

// MergeArguments 把固定参数与本次调用参数合并为发给上游的参数表：
// 以 defaultArgs 为底，客户端传入的同名键以客户端为准（允许覆盖默认值）。
func MergeArguments(defaultArgs, clientArgs map[string]any) map[string]any {
	if len(defaultArgs) == 0 {
		return clientArgs
	}
	out := make(map[string]any, len(defaultArgs)+len(clientArgs))
	for k, v := range defaultArgs {
		out[k] = v
	}
	for k, v := range clientArgs {
		out[k] = v
	}
	return out
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
