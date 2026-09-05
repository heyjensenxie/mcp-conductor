package gateway

import (
	"encoding/json"
	"net/http"

	"github.com/xmj128/mcp-conductor/internal/auth"
	"github.com/xmj128/mcp-conductor/internal/errs"
)

// handleLogin 校验管理令牌并签发会话令牌。
// password 必须是 auth.operator_token（AccessKey 明文不能用于登录）；
// username 仅作 UI 友好显示，由认证服务忽略。登录端点属于 /api/auth/* 免认证路径。
func handleLogin(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
			writeGatewayError(w, r, http.StatusBadRequest,
				errs.New(errs.CodeInvalidArgument, "请求体须包含 password"))
			return
		}
		session, err := svc.Login(r.Context(), body.Username, body.Password)
		if err != nil {
			writeGatewayError(w, r, http.StatusUnauthorized, err)
			return
		}
		// Token 仅下发一次；后续请求以 Authorization: Bearer <token> 或 X-Api-Key 携带。
		writeOK(w, RequestIDFrom(r.Context()), session)
	}
}

// handleAuthStatus 返回认证是否开启，供前端决定是否要求登录。
func handleAuthStatus(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeOK(w, RequestIDFrom(r.Context()), map[string]bool{"auth_required": svc.Enabled()})
	}
}
