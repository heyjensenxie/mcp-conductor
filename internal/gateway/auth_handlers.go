package gateway

import (
	"encoding/json"
	"net/http"

	"github.com/xmj128/mcp-conductor/internal/auth"
	"github.com/xmj128/mcp-conductor/internal/errs"
)

// handleLogin 校验用户名/密码并签发会话令牌。
// username 可留空（此时 password 视为静态 API Key）；正常 UI 登录走用户名+密码。
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
		session, err := svc.Login(body.Username, body.Password)
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