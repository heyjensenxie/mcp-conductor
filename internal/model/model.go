// Package model 定义 MCP Conductor 的核心领域模型。
//
// 遵循"领域模型优先"原则（见 PRD 第五节），MVP 覆盖 Server、Tool、Route、
// AccessKey、Credential、Traffic；Evaluation、Dataset、TestCase、Version 仅预留边界。
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

// Server 代表一个逻辑 MCP Server（一组共享同一命名空间、聚合工具与上游凭证
// 的具体实例集合）。
//
// Server 不等于 Server Instance：一个逻辑 Server 下可挂多个实例，负载均衡与
// 健康探测作用于实例维度（见 Instance），Endpoint/Transport 只属于实例。
// HealthStatus 是由实例健康集合推导出的聚合值（见 AggregateServerHealth），
// 供列表筛选与展示；对外 API 响应还在 Server 上附带 instances 列表（由控制面
// 水合，不落库）。
type Server struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Description  string       `json:"description,omitempty"`
	Enabled      bool         `json:"enabled"`
	HealthStatus ServerStatus `json:"health_status"`
	// Instances 是对外 API 响应水合出来的实例列表（endpoint/transport/健康展示
	// 与实例管理入口）。存储层不读写本字段（不落库），仅控制面在序列化前填充。
	Instances []Instance `json:"instances,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Instance 代表一个逻辑 Server 下的具体上游实例（endpoint + transport 的承载单位）。
//
// Enabled=false 表示实例被摘除/停用（不进探测、不进负载均衡），与 Server 级
// Enabled 相互独立。HealthStatus 取值 unknown/healthy/unhealthy；disabled 只
// 出现在 Server 聚合层（见 AggregateServerHealth）。
type Instance struct {
	ID           string       `json:"id"`
	ServerID     string       `json:"server_id"`
	Endpoint     string       `json:"endpoint"`
	Transport    Transport    `json:"transport"`
	Enabled      bool         `json:"enabled"`
	HealthStatus ServerStatus `json:"health_status"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// IsCallable 报告该实例是否可作为调用目标：启用且未被探测为 unhealthy。
// unknown 乐观可调（尚未探活时允许尝试，避免新注册实例一上来就不可用）。
func (i Instance) IsCallable() bool {
	return i.Enabled && i.HealthStatus != ServerStatusUnhealthy
}

// AggregateServerHealth 由实例集合推导 Server 级聚合健康（写回 servers 行供列表
// 筛选与展示）：
//   - 禁用 → disabled；
//   - 任一启用实例 healthy → healthy；
//   - 存在启用实例但都还没探出 healthy（含 unknown）→ unknown；
//   - 其余（启用实例全 unhealthy，或没有启用实例）→ unhealthy。
func AggregateServerHealth(enabled bool, instances []Instance) ServerStatus {
	if !enabled {
		return ServerStatusDisabled
	}
	hasUnknown := false
	for i := range instances {
		inst := &instances[i]
		if !inst.Enabled {
			continue
		}
		switch inst.HealthStatus {
		case ServerStatusHealthy:
			return ServerStatusHealthy
		case ServerStatusUnknown:
			hasUnknown = true
		}
	}
	if hasUnknown {
		return ServerStatusUnknown
	}
	return ServerStatusUnhealthy
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
	// SourceDescription / SourceInputSchema 保存最近一次上游发现结果，供控制台
	// 对比与恢复默认值；人工覆盖后的对外字段不会被重新发现覆盖。
	SourceDescription     string         `json:"source_description,omitempty"`
	SourceInputSchema     map[string]any `json:"source_input_schema,omitempty"`
	NameOverridden        bool           `json:"name_overridden"`
	DescriptionOverridden bool           `json:"description_overridden"`
	InputSchemaOverridden bool           `json:"input_schema_overridden"`
	RiskLevel             string         `json:"risk_level,omitempty"`
	Enabled               bool           `json:"enabled"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
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
// QPS 为该 key 专属限流速率（与安全防护「维度默认」同构）：>0 覆盖全局默认，
// 0 表示跟随全局（安全防护）默认，-1 表示不设 key 级上限（仅受单 IP/全局闸）；
// WindowSeconds 为该 key 专属滑动窗口（0=跟随全局）。Burst 已废弃（保留兼容）。
// KeyHash 存储 HMAC-SHA256(key)，Secret 为原始密钥明文，两者均 json:"-"
// 不随任何 API 序列化下发；明文只在创建成功时经专用响应体返回一次。
type AccessKey struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Subject string `json:"subject"` // 唯一主体标识（日志/授权/限流维度）
	Enabled bool   `json:"enabled"`
	// QPS 该 key 的每秒速率上限：>0 = 专属覆盖；0 = 跟随安全防护维度默认；-1 = 不设 key 上限。
	QPS int `json:"qps"`
	// Burst 已废弃（不再参与判定，保留兼容）。
	Burst int `json:"burst"`
	// WindowSeconds 该 key 自己的滑动窗口（秒，0..3600）：0 = 跟随安全防护全局窗口；
	// >0 = 覆盖（与 QPS 一并决定该 key 每窗口容量 = QPS×WindowSeconds）。
	WindowSeconds int         `json:"window_seconds"`
	Grants        []ToolGrant `json:"grants"`
	KeyHash       string      `json:"-"` // HMAC-SHA256 哈希，落库不下发
	Secret        string      `json:"-"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
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
	// ID 是流水主键（MySQL 自增 / memory 自增分配），供详情与回放按 id 寻址。
	ID int64 `json:"id"`
	// HasArgs 标记该行是否捕获了入参（列表轻量标记，供前端启用「回放」；
	// 入参本体见 RequestArgs，永不随列表下发）。
	HasArgs bool `json:"has_args,omitempty"`
	// RequestArgs 记录本次实际发出的 tools/call 入参（record_args 开启时捕获）。
	// json:"-"：只在 GET /api/logs/{id} 详情显式带出，列表/常规序列化不泄露请求体。
	RequestArgs map[string]any `json:"-"`
	RequestID   string         `json:"request_id"`
	TraceID     string         `json:"trace_id,omitempty"`
	ServerID    string         `json:"server_id"`
	// InstanceID 记录本次调用实际命中的上游实例；实例未定（如路由阶段失败）
	// 或 Server 单实例未落实例归属时为 ""。
	InstanceID string `json:"instance_id,omitempty"`
	Tool       string `json:"tool"`
	Client     string `json:"client,omitempty"`
	// ClientIP 记录调用方来源 IP（最外层中间件按可信代理规则解析，非空才落库/下发）。
	ClientIP  string    `json:"client_ip,omitempty"`
	Status    string    `json:"status"`
	LatencyMS int64     `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// TrendMinute 是单个维度在某个已闭合分钟桶的计数（进程内分钟桶落库的持久形态）。
// Scope 与 DimensionKey 的关系：tool→gateway 工具名；server→server id（ServerID 同值）；
// instance→instance id（ServerID 记录所属 Server）。Minute 为该分钟起点（UTC Unix 秒）。
type TrendMinute struct {
	Scope    string `json:"scope"`
	ServerID string `json:"server_id,omitempty"`
	DimKey   string `json:"dim_key"`
	Minute   int64  `json:"minute"`
	Totals   int64  `json:"totals"`
	Errors   int64  `json:"errors"`
}

// RuntimeRateLimit 是数据面 /mcp 三级限流的运行期配额（语义与 cfg.ratelimit 一致）：
// 各 QPS<=0 表示该级不启用（global）；ip 为 0 时沿用 QPS；key 为 0 时沿用维度默认。
// WindowSeconds 为滑动窗口长度（秒，0 归一为 1）。Burst 已废弃（不再参与判定）。
type RuntimeRateLimit struct {
	QPS           int `json:"qps"`
	Burst         int `json:"burst"`
	WindowSeconds int `json:"window_seconds"`
	IPQPS         int `json:"ip_qps"`
	IPBurst       int `json:"ip_burst"`
	GlobalQPS     int `json:"global_qps"`
	GlobalBurst   int `json:"global_burst"`
}

// RuntimeAutoBan 是自动封禁参数（来源在检测窗口内被限流 429 达次数即临时封禁，
// TTL 到期自动解封）。0 值语义由控制面归一为默认（60s/5 次/300s）。
type RuntimeAutoBan struct {
	Enabled       bool `json:"enabled"`
	WindowSeconds int  `json:"window_seconds"`
	MaxViolations int  `json:"max_violations"`
	BanSeconds    int  `json:"ban_seconds"`
}

// RuntimeConfig 是网关运行期治理配置的完整快照（单行持久化，Console 维护）：
// 后台首次保存后即权威，静态 config.yaml 仅作为无存值时的种子。
type RuntimeConfig struct {
	RateLimit RuntimeRateLimit `json:"ratelimit"`
	AutoBan   RuntimeAutoBan   `json:"auto_ban"`
	// IPBlocklist 来源 IP/CIDR 封禁名单（仅 /mcp 生效）。
	IPBlocklist []string `json:"ip_blocklist"`
	// IPWhitelist 可信豁免名单：命中来源不受 IP 黑名单 / 自动封禁(IP) / 单 IP 级限流影响。
	IPWhitelist []string  `json:"ip_whitelist"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}
