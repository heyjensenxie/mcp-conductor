package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/ratelimit"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// fakeAuditer 捕获拒绝审计事件，供断言中间件拒绝是否按状态/IP 落审计。
type fakeAuditer struct {
	got []observability.Rejection
}

func (f *fakeAuditer) RecordRejection(_ context.Context, rej observability.Rejection) {
	f.got = append(f.got, rej)
}

// okHandler 是最小下游处理器（不应被调用到的分支由中间件自身拒绝）。
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

// auditedMcpRequest 构造带拒绝审计 + 来源 IP 上下文的 /mcp 请求。
func auditedMcpRequest(aud RejectionAuditer, ip string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = ip + ":1234"
	ctx := req.Context()
	ctx = WithRejectionAudit(ctx, aud)
	ctx = WithClientIP(ctx, ip) // 模拟最外层 captureClientIPMiddleware
	return req.WithContext(ctx)
}

// TestRejectAudit_AuthFailureRecordsMcpOnly 验证 /mcp 认证失败（401）落一条拒绝
// 审计（authentication_error + 来源 IP + 空主体）；控制面 /api 的认证失败不入调用日志。
func TestRejectAudit_AuthFailureRecordsMcpOnly(t *testing.T) {
	h := chain(okHandler, authMiddleware(failAuth{}))

	aud := &fakeAuditer{}
	req := auditedMcpRequest(aud, "198.51.100.7")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("认证失败应 401，得到 %d", rec.Code)
	}
	if len(aud.got) != 1 {
		t.Fatalf("/mcp 认证失败应落 1 条审计，得到 %d", len(aud.got))
	}
	if aud.got[0].Status != "authentication_error" || aud.got[0].ClientIP != "198.51.100.7" ||
		aud.got[0].Client != "" {
		t.Fatalf("认证审计字段不符：%+v", aud.got[0])
	}

	// 控制面 /api：认证失败不写入调用日志（审计只针对数据面 /mcp）。
	audAPI := &fakeAuditer{}
	reqAPI := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	reqAPI.RemoteAddr = "198.51.100.7:1234"
	reqAPI = reqAPI.WithContext(WithRejectionAudit(reqAPI.Context(), audAPI))
	recAPI := httptest.NewRecorder()
	h.ServeHTTP(recAPI, reqAPI)
	if len(audAPI.got) != 0 {
		t.Fatalf("非 /mcp 拒绝不应审计，得到 %d", len(audAPI.got))
	}
}

// denyLimiter 恒拒绝，用于强制限流中间件走 429 路径。
type denyLimiter struct{}

func (denyLimiter) Allow(context.Context, string, ratelimit.Limit) bool { return false }

// TestRejectAudit_RateLimitRecordsMcp 验证 /mcp 限流拒绝（429）落 rate_limit_error 审计。
func TestRejectAudit_RateLimitRecordsMcp(t *testing.T) {
	// 全局闸 1 QPS + 恒拒绝限流器 → 必然 429。
	h := chain(okHandler, rateLimitMiddleware(denyLimiter{}, rateLimitPolicy{
		globalLimit: ratelimit.Limit{QPS: 1, WindowSec: 60},
	}))

	aud := &fakeAuditer{}
	req := auditedMcpRequest(aud, "198.51.100.7")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("限流拒绝应 429，得到 %d", rec.Code)
	}
	if len(aud.got) != 1 || aud.got[0].Status != "rate_limit_error" || aud.got[0].ClientIP != "198.51.100.7" {
		t.Fatalf("限流审计不符：%+v", aud.got)
	}
}

// TestRejectAudit_BlocklistRecordsMcp 验证 /mcp 命中 IP 封禁名单（403）落
// authorization_error 审计（对外文案保持通用）。另开一个白名单 source 豁免用例
// 隐含在 blocklist 未命中场景（不再单独断言）。
func TestRejectAudit_BlocklistRecordsMcp(t *testing.T) {
	rc := &model.RuntimeConfig{IPBlocklist: []string{"198.51.100.7/32"}}
	cache := newRuntimeConfigCache(memory.New(), rc)
	h := chain(okHandler, runtimeGuardMiddleware(cache))

	aud := &fakeAuditer{}
	req := auditedMcpRequest(aud, "198.51.100.7")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("封禁 IP 应 403，得到 %d", rec.Code)
	}
	if len(aud.got) != 1 || aud.got[0].Status != "authorization_error" || aud.got[0].ClientIP != "198.51.100.7" {
		t.Fatalf("封禁审计不符：%+v", aud.got)
	}
}

// TestRejectAudit_NoAuditerNoop 验证未注入拒绝审计器（既有测试/未装配路径）时，
// 拒绝点不 panic 且不产生审计——保证中间件签名零改动下的向后兼容。
func TestRejectAudit_NoAuditerNoop(t *testing.T) {
	h := chain(okHandler, authMiddleware(failAuth{}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil) // 未注入审计器
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无审计器仍应正常拒绝 401，得到 %d", rec.Code)
	}
}
