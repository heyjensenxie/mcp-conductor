package registry

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage"
)

// connection.go —— "保存前测试连接"（草稿拨测）能力。
//
// 与既有 Rediscover/TestInstance 的本质区别：草稿拨测**没有任何副作用**——
// 不创建 Server、不创建实例、不刷新 Tools、不更新健康状态、不落临时请求头。
// 因此它不读取实例健康、也不写回存储，仅用提交中的（endpoint/transport/args
// + 临时 Header）做一次 MCP initialize 握手，供控制台"先测试、再保存"。

// CredentialHeaders 按 Server 已配置凭证组装上游注入 header：
// static_token → Authorization: Bearer <value>；api_key → <Header>: <value>。
// 值来自存储解密，不落日志；读取/解密失败返回错误，由调用方 fail-closed 中止
// （避免把本地解密失败掩盖成上游 401，也避免匿名盲发）。
func CredentialHeaders(ctx context.Context, store storage.CredentialStore, serverID string) (map[string]string, error) {
	if strings.TrimSpace(serverID) == "" {
		return nil, nil
	}
	creds, err := store.ListCredentialsByServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	headers := make(map[string]string)
	for _, cred := range creds {
		if cred.Value == "" {
			continue
		}
		switch cred.Kind {
		case model.CredentialStaticToken:
			headers["Authorization"] = "Bearer " + cred.Value
		case model.CredentialAPIKey:
			if cred.Header != "" {
				headers[cred.Header] = cred.Value
			}
		}
	}
	return headers, nil
}

// DraftInstanceProber 对"尚未落库的实例草稿"执行一次 initialize 握手，
// 校验 endpoint/transport/stdio args 与临时请求头是否可用。
// headers 是本次拨测使用的完整 header 集合（已由调用方合并保存凭据与临时头）。
type DraftInstanceProber interface {
	CheckDraft(
		ctx context.Context,
		server model.Server,
		instance model.Instance,
		headers map[string]string,
	) (model.ServerStatus, error)
}

// WithDraftProber 装配草稿探针（通常为同一个 mcpclient.Adapter）。
func (s *Service) WithDraftProber(prober DraftInstanceProber) *Service {
	s.draftProber = prober
	return s
}

// TestConnectionInput 是"保存前测试连接"的入参。
// ServerID 为空表示新增流程（没有已保存凭据可加载）；非空表示编辑已有 Server，
// 会先加载该 Server 已保存的凭据，再由 Headers 覆盖同名 header。
type TestConnectionInput struct {
	ServerID  string            `json:"server_id,omitempty"`
	Name      string            `json:"name,omitempty"`
	Endpoint  string            `json:"endpoint"`
	Transport model.Transport   `json:"transport,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// TestConnection 用提交中的草稿配置执行一次握手，返回健康结论；不产生任何落库
// 副作用（不建 Server/实例、不刷新 Tools、不改健康、不保存临时 Header）。
//
// 请求头合并顺序：已保存凭据 → 临时 Headers（同名以临时值为准）。密钥明文既不
// 从本接口返回，也不写日志；上游不可达/握手失败时返回带脱敏 endpoint 的错误。
func (s *Service) TestConnection(ctx context.Context, in TestConnectionInput) (model.ServerStatus, error) {
	if s.draftProber == nil {
		return model.ServerStatusUnknown, errs.New(errs.CodeInternal, "草稿探针未装配")
	}
	endpoint := strings.TrimSpace(in.Endpoint)
	if endpoint == "" {
		return model.ServerStatusUnknown, errs.New(errs.CodeInvalidArgument, "endpoint 不能为空")
	}
	transport, err := normalizeTransport(in.Transport)
	if err != nil {
		return model.ServerStatusUnknown, err
	}
	if err := validateArgsForTransport(transport, endpoint, in.Args); err != nil {
		return model.ServerStatusUnknown, err
	}
	if err := validateHTTPEndpoint(transport, endpoint); err != nil {
		return model.ServerStatusUnknown, err
	}

	// 编辑已有 Server：以存储中的名称/ID 拨测（错误文案更贴合上下文），并加载其
	// 已保存凭据；新增（ServerID 为空）只使用临时 Header。
	server := model.Server{
		ID:           strings.TrimSpace(in.ServerID),
		Name:         strings.TrimSpace(in.Name),
		Enabled:      true,
		HealthStatus: model.ServerStatusUnknown,
	}
	if server.ID != "" {
		existing, err := s.stores.GetServer(ctx, server.ID)
		if err != nil {
			return model.ServerStatusUnknown, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
		}
		server.Name = existing.Name
	}
	if server.Name == "" {
		server.Name = "draft"
	}

	headers, err := CredentialHeaders(ctx, s.stores, server.ID)
	if err != nil {
		return model.ServerStatusUnknown, errs.Wrap(errs.CodeInternal, err, "读取 Server %q 凭据失败", server.Name)
	}
	// 临时 Header 覆盖同名已保存 Header。
	for k, v := range in.Headers {
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		if headers == nil {
			headers = make(map[string]string, len(in.Headers))
		}
		headers[k] = v
	}

	instance := model.Instance{
		ServerID:     server.ID,
		Endpoint:     endpoint,
		Transport:    transport,
		Args:         in.Args,
		Enabled:      true,
		HealthStatus: model.ServerStatusUnknown,
	}
	status, probeErr := s.draftProber.CheckDraft(ctx, server, instance, headers)
	if probeErr != nil {
		return model.ServerStatusUnhealthy, draftFailError(probeErr, instance)
	}
	return status, nil
}

// draftFailError 构造"保存前测试连接失败"统一错误：超时归 CodeTimeout、本地内部
// 错误（如凭据解密失败）归 CodeInternal，其余 CodeUpstream；文案使用脱敏 endpoint
// 并附 errs.SafeCause 安全根因，不透出端点 query/userinfo 或上游响应正文。
func draftFailError(probeErr error, instance model.Instance) *errs.Error {
	code := errs.CodeUpstream
	switch {
	case errs.IsTimeout(probeErr):
		code = errs.CodeTimeout
	case errs.Is(probeErr, errs.CodeInternal):
		code = errs.CodeInternal
	}
	message := fmt.Sprintf("连接测试失败（%s）", errs.RedactEndpoint(instance.Endpoint))
	if cause := errs.SafeCause(probeErr); cause != "" {
		message += "：" + cause
	}
	return errs.Wrap(code, probeErr, "%s", message)
}

// validateHTTPEndpoint 校验 HTTP 传输族（https/sse）的端点是否为绝对 http(s) URL。
// stdio 的 endpoint 承载可执行命令，不做 URL 校验（由 validateArgsForTransport
// 保证非空）。
func validateHTTPEndpoint(transport model.Transport, endpoint string) error {
	if transport == model.TransportStdio {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errs.New(errs.CodeInvalidArgument, "endpoint 需为绝对 URL（如 http://localhost:9000/mcp）")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errs.New(errs.CodeInvalidArgument, "endpoint 仅支持 http / https 协议")
	}
	return nil
}
