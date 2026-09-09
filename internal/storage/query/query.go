// Package query 定义管理面列表查询（分页 + 关键词/字段筛选）的类型。
//
// 放在 storage 下的叶子包，避免 storage 接口、memory/mysql 实现与 gateway
// HTTP 参数解析之间循环依赖；只依赖标准库，不依赖 model/存储实现。
//
// 分页语义：Paging.PageSize <= 0 表示"返回全部匹配行，不分页"（供控制台
// picker/下拉等需要完整目录的消费）；Page 为 1 起，仅在 PageSize > 0 时生效。
// 关键词按资源含义模糊匹配（对名称/标识字段），其余字段为精确等值/三态筛选，
// 筛选在分页与全量两种模式下都生效。排序由各存储实现固定（见文档，不开放列排序）。
package query

import "time"

// Paging 承载分页意图。
type Paging struct {
	// Page 为 1 起的页码，仅 PageSize > 0 时有意义。
	Page int
	// PageSize <= 0 表示不分页（返回全部匹配行）。
	PageSize int
}

// ServerQuery 是 Server 列表的查询条件。
type ServerQuery struct {
	Paging
	// SkipTotal 表示调用方只需要当前返回行，不需要匹配总数。
	SkipTotal bool
	// Q 对 name 做模糊匹配（LIKE）。
	Q string
	// Enabled 三态筛选（nil 表示不过滤）。
	Enabled *bool
	// HealthStatus 为空表示不过滤；否则取 model.ServerStatus 合法值。
	HealthStatus string
}

// ToolQuery 是 Tool 列表的查询条件；ServerID 非空时等价于
// 原 ListToolsByServer 的按 Server 过滤语义（/api/servers/{id}/tools 复用）。
type ToolQuery struct {
	Paging
	// SkipTotal 表示调用方只需要当前返回行，不需要匹配总数。
	SkipTotal bool
	// Q 对 gateway_name 或 original_name 做模糊匹配。
	Q string
	// ServerID 为空表示不过滤。
	ServerID string
	Enabled  *bool
}

// RouteQuery 是路由列表的查询条件。
type RouteQuery struct {
	Paging
	Q        string // 对 name 模糊匹配
	ServerID string // 为空表示不过滤
	Enabled  *bool
}

// AccessKeyQuery 是 API Key 列表的查询条件。
type AccessKeyQuery struct {
	Paging
	Q       string // 对 name 或 subject 模糊匹配
	Enabled *bool
}

// CredentialQuery 是单 Server 凭证列表的查询条件。
type CredentialQuery struct {
	Paging
	// ServerID 必填：凭证为 /api/servers/{id}/credentials 下的子资源。
	ServerID string
	Q        string // 对 name 模糊匹配
	// Kind 为空表示不过滤；否则取 model.CredentialKind 合法值。
	Kind string
	// HasValue 三态筛选（是否已配置密文/值）。
	HasValue *bool
}

// TrafficQuery 是调用日志列表的查询条件。
type TrafficQuery struct {
	Paging
	// SkipTotal 表示调用方只消费当前页，不需要匹配总数。日志流水表增长较快，
	// 此时存储实现应跳过额外的 COUNT(*) 查询并把 total 返回为 0。
	SkipTotal bool
	// Q 对 tool / client / request_id / client_ip 做模糊匹配。
	Q string
	// ServerID 为空表示不过滤。注意流量行 server_id 可空，等值过滤不命中空行。
	ServerID string
	// InstanceID 为空表示不过滤；非空等值过滤命中的实例（配合 ServerID，
	// 定位多实例 Server 中的单个实例流量）。
	InstanceID string
	// Status 为空表示不过滤；否则取 "success" 或 errs.Code 字符串。
	Status string
	// ClientIP 为空表示不过滤；非空对来源 IP 精确等值（列存归一后的地址，
	// 空行不命中）。配合 Q 可对 IP 片段做模糊匹配。
	ClientIP string
	// From / To 为闭区间时间过滤（对写入时间 ts），零值表示对应端不设界。
	// 统一按 UTC 语义处理（库内 DATETIME(3) 存 UTC）。
	From time.Time
	To   time.Time
}

// TrendQuery 是分钟桶趋势的读取条件（长程序列窗口）。
// Scope ∈ tool|server|instance；ServerID 用于收敛 server/instance 维的归属 Server
// （tool 维忽略）；DimKey 非空时只读该单个维度；From/To 为分钟闭区间（UTC Unix 秒）。
type TrendQuery struct {
	Scope    string
	ServerID string
	DimKey   string
	From     int64
	To       int64
}

// TrafficWindowQuery 是调用日志的**窗口聚合**条件（不返回行，只返回统计）。
// 供看板/观测按时间窗口读取服务端聚合结果：计数与均值精确、分位延迟由固定桶
// 直方图近似、分组按调用量降序截断（TopTools/TopIPs），成本与窗口行数成正比、
// 与表总量无关（走 ts 索引范围扫描）。
type TrafficWindowQuery struct {
	// From / To 为闭区间时间过滤（对写入时间 ts，UTC 语义）；零值表示该端不设界。
	From time.Time
	To   time.Time
	// ServerID 非空时只聚合该 Server 的调用（Server 详情/看板过滤）。
	ServerID string
	// TopTools / TopIPs > 0 时分别限制 by_tool / by_client_ip 的返回条数
	// （按调用量降序）；<=0 表示不截断（仍受存储实现的硬上限约束）。
	TopTools int
	TopIPs   int
	// TopMinutes > 0 时只返回窗口内最近的 TopMinutes 个分钟桶（长窗口图表降采样用）；
	// <=0 表示返回窗口内全部分钟桶。
	TopMinutes int
}
