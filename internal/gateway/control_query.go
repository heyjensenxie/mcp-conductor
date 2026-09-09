package gateway

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

// control_query.go —— 各列表资源的 HTTP 查询参数绑定。
//
// 每个 binder 读取 r.URL.Query() 的公共参数（page/page_size）与该资源筛选
// 参数，返回 storage/query 的查询结构；非法参数返回 CodeInvalidArgument。
// server_id 类子资源（tools/credentials 按 Server 过滤）由调用方在 handler
// 中填入（路径 id 或参数），不在 binder 内处理。

// keyword 读取 q 参数并去首尾空白。
func keyword(v url.Values) string { return strings.TrimSpace(v.Get("q")) }

// skipTotal 解析 include_total=false。列表默认保留 total，只有明确声明不需要
// 总数的轻量消费方（如仪表盘）才跳过 COUNT(*)。
func skipTotal(v url.Values) (bool, error) {
	if v.Get("include_total") == "" {
		return false, nil
	}
	includeTotal, err := parseTriStateBool(v, "include_total")
	if err != nil {
		return false, err
	}
	return includeTotal != nil && !*includeTotal, nil
}

// bindServerQuery 绑定 Server 列表查询。
func bindServerQuery(r *http.Request) (query.ServerQuery, error) {
	v := r.URL.Query()
	page, pageSize, err := parsePageParams(v)
	if err != nil {
		return query.ServerQuery{}, err
	}
	enabled, err := parseTriStateBool(v, "enabled")
	if err != nil {
		return query.ServerQuery{}, err
	}
	skipTotal, err := skipTotal(v)
	if err != nil {
		return query.ServerQuery{}, err
	}
	out := query.ServerQuery{
		Paging:    query.Paging{Page: page, PageSize: pageSize},
		SkipTotal: skipTotal,
		Q:         keyword(v),
		Enabled:   enabled,
	}
	if hs := strings.TrimSpace(v.Get("health_status")); hs != "" {
		if err := validHealthStatus(hs); err != nil {
			return query.ServerQuery{}, err
		}
		out.HealthStatus = hs
	}
	return out, nil
}

// bindToolQuery 绑定 Tool 列表查询（server_id 可为空 = 全部 Server）。
func bindToolQuery(r *http.Request) (query.ToolQuery, error) {
	v := r.URL.Query()
	page, pageSize, err := parsePageParams(v)
	if err != nil {
		return query.ToolQuery{}, err
	}
	enabled, err := parseTriStateBool(v, "enabled")
	if err != nil {
		return query.ToolQuery{}, err
	}
	skipTotal, err := skipTotal(v)
	if err != nil {
		return query.ToolQuery{}, err
	}
	return query.ToolQuery{
		Paging:    query.Paging{Page: page, PageSize: pageSize},
		SkipTotal: skipTotal,
		Q:         keyword(v),
		ServerID:  strings.TrimSpace(v.Get("server_id")),
		Enabled:   enabled,
	}, nil
}

// bindRouteQuery 绑定路由列表查询。
func bindRouteQuery(r *http.Request) (query.RouteQuery, error) {
	v := r.URL.Query()
	page, pageSize, err := parsePageParams(v)
	if err != nil {
		return query.RouteQuery{}, err
	}
	enabled, err := parseTriStateBool(v, "enabled")
	if err != nil {
		return query.RouteQuery{}, err
	}
	return query.RouteQuery{
		Paging:   query.Paging{Page: page, PageSize: pageSize},
		Q:        keyword(v),
		ServerID: strings.TrimSpace(v.Get("server_id")),
		Enabled:  enabled,
	}, nil
}

// bindAccessKeyQuery 绑定 API Key 列表查询。
func bindAccessKeyQuery(r *http.Request) (query.AccessKeyQuery, error) {
	v := r.URL.Query()
	page, pageSize, err := parsePageParams(v)
	if err != nil {
		return query.AccessKeyQuery{}, err
	}
	enabled, err := parseTriStateBool(v, "enabled")
	if err != nil {
		return query.AccessKeyQuery{}, err
	}
	return query.AccessKeyQuery{
		Paging:  query.Paging{Page: page, PageSize: pageSize},
		Q:       keyword(v),
		Enabled: enabled,
	}, nil
}

// bindCredentialQuery 绑定单 Server 凭证列表查询（ServerID 由调用方从路径填入）。
func bindCredentialQuery(r *http.Request) (query.CredentialQuery, error) {
	v := r.URL.Query()
	page, pageSize, err := parsePageParams(v)
	if err != nil {
		return query.CredentialQuery{}, err
	}
	enabled, err := parseTriStateBool(v, "has_value")
	if err != nil {
		return query.CredentialQuery{}, err
	}
	out := query.CredentialQuery{
		Paging:   query.Paging{Page: page, PageSize: pageSize},
		Q:        keyword(v),
		HasValue: enabled,
	}
	if kind := strings.TrimSpace(v.Get("kind")); kind != "" {
		if err := validCredentialKind(kind); err != nil {
			return query.CredentialQuery{}, err
		}
		out.Kind = kind
	}
	return out, nil
}

// trafficLogMaxPage 是调用日志单次可取的最大条数。traffic_log 属高增长流水表，
// 禁止 page_size<=0 的"全量模式"（避免高并发下整表返回拖垮管理面）；
// 缺省/超限一律收敛为该上限，按最近记录返回。
const trafficLogMaxPage = 200

// bindTrafficQuery 绑定调用日志列表查询。
func bindTrafficQuery(r *http.Request) (query.TrafficQuery, error) {
	v := r.URL.Query()
	page, pageSize, err := parsePageParams(v)
	if err != nil {
		return query.TrafficQuery{}, err
	}
	if pageSize <= 0 || pageSize > trafficLogMaxPage {
		pageSize = trafficLogMaxPage
	}
	out := query.TrafficQuery{
		Paging:     query.Paging{Page: page, PageSize: pageSize},
		Q:          keyword(v),
		ServerID:   strings.TrimSpace(v.Get("server_id")),
		InstanceID: strings.TrimSpace(v.Get("instance_id")),
		ClientIP:   strings.TrimSpace(v.Get("client_ip")),
	}
	skipTotal, err := skipTotal(v)
	if err != nil {
		return query.TrafficQuery{}, err
	}
	out.SkipTotal = skipTotal
	if status := strings.TrimSpace(v.Get("status")); status != "" {
		if err := validTrafficStatus(status); err != nil {
			return query.TrafficQuery{}, err
		}
		out.Status = status
	}
	if raw := v.Get("from"); raw != "" {
		t, err := validRFC3339(raw)
		if err != nil {
			return query.TrafficQuery{}, err
		}
		out.From = t
	}
	if raw := v.Get("to"); raw != "" {
		t, err := validRFC3339(raw)
		if err != nil {
			return query.TrafficQuery{}, err
		}
		out.To = t
	}
	return out, nil
}
