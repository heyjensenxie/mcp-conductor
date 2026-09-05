package gateway

import (
	"net/url"
	"strconv"
	"time"

	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/model"
)

// 管理面列表分页/筛选的共享参数解析与统一分页信封。
//
// 契约：列表接口 data 固定为 { items, total, page, page_size }；
// page 默认 1（>=1）；page_size 默认 defaultPageSize、上限 maxPageSize，
// <=0 表示"全量模式"（返回全部匹配行，供控制台 picker/下拉全量目录）；
// 非法但非空的值一律返回 CodeInvalidArgument（HTTP 400）。

const (
	defaultPageSize = 20
	maxPageSize     = 500
)

// pageData 是分页列表的统一 data 载荷。
type pageData[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// parsePageParams 解析 page / page_size。
// 返回 (page, pageSize)：pageSize<=0 表示调用方需要全量（不分页）。
func parsePageParams(v url.Values) (page, pageSize int, err error) {
	page = 1
	if raw := v.Get("page"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 1 {
			return 0, 0, errs.New(errs.CodeInvalidArgument, "page 需为 >=1 的整数")
		}
		page = n
	}
	pageSize = defaultPageSize
	if raw := v.Get("page_size"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n > maxPageSize {
			return 0, 0, errs.New(errs.CodeInvalidArgument, "page_size 需为 0~%d 的整数", maxPageSize)
		}
		pageSize = n // <=0 表示全量
	}
	return page, pageSize, nil
}

// parseTriStateBool 解析三态布尔筛选参数：缺失返回 nil；"true"/"false" 返回对应指针；
// 其他值返回 400 参数错误。
func parseTriStateBool(v url.Values, key string) (*bool, error) {
	raw := v.Get(key)
	if raw == "" {
		return nil, nil
	}
	switch raw {
	case "true":
		b := true
		return &b, nil
	case "false":
		b := false
		return &b, nil
	default:
		return nil, errs.New(errs.CodeInvalidArgument, "%s 仅支持 true / false", key)
	}
}

// validHealthStatus 校验 Server 健康状态枚举。
func validHealthStatus(s string) error {
	switch model.ServerStatus(s) {
	case model.ServerStatusUnknown, model.ServerStatusHealthy,
		model.ServerStatusUnhealthy, model.ServerStatusDisabled:
		return nil
	}
	return errs.New(errs.CodeInvalidArgument, "health_status 仅支持 unknown / healthy / unhealthy / disabled")
}

// validCredentialKind 校验凭证类型枚举。
func validCredentialKind(s string) error {
	switch model.CredentialKind(s) {
	case model.CredentialStaticToken, model.CredentialAPIKey:
		return nil
	}
	return errs.New(errs.CodeInvalidArgument, "kind 仅支持 static_token / api_key")
}

// validTrafficStatus 校验调用日志状态：success 或标准错误码。
func validTrafficStatus(s string) error {
	switch s {
	case "success",
		string(errs.CodeProtocol),
		string(errs.CodeAuthentication),
		string(errs.CodeAuthorization),
		string(errs.CodeRateLimit),
		string(errs.CodeRoute),
		string(errs.CodeUpstream),
		string(errs.CodeTimeout),
		string(errs.CodeInvalidArgument),
		string(errs.CodeNotFound),
		string(errs.CodeInternal):
		return nil
	}
	return errs.New(errs.CodeInvalidArgument, "status 仅支持 success 或标准错误码")
}

// validRFC3339 解析闭区间时间筛选参数（RFC3339，UTC 语义）。
func validRFC3339(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, errs.New(errs.CodeInvalidArgument, "%s 需为 RFC3339 时间（如 2026-09-05T00:00:00Z）", s)
	}
	return t, nil
}
