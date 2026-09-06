package gateway

import (
	"net/http"
	"strings"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/eval"
)

// evalEnabled 校验评测服务已装配，否则返回 CodeInternal 并 false。
func (c *Control) evalEnabled(w http.ResponseWriter, r *http.Request) bool {
	if c.eval != nil {
		return true
	}
	writeGatewayError(w, r, http.StatusInternalServerError,
		errs.New(errs.CodeInternal, "评测服务未装配"))
	return false
}

// instanceParam 读取可选的 ?instance_id= 定向评测参数（空 = 首个可拨测实例）。
func instanceParam(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("instance_id"))
}

// handleEvalMeta 返回指定 Server 的评测概况（拨测实例/工具数/流量/覆盖）。
func (c *Control) handleEvalMeta(w http.ResponseWriter, r *http.Request) {
	if !c.evalEnabled(w, r) {
		return
	}
	meta, err := c.eval.Describe(r.Context(), r.PathValue("id"), instanceParam(r))
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), meta)
}

// handleEvalQuality 对指定 Server 现场拨测并输出 MCP 质量报告。
func (c *Control) handleEvalQuality(w http.ResponseWriter, r *http.Request) {
	if !c.evalEnabled(w, r) {
		return
	}
	report, err := c.eval.RunQuality(r.Context(), r.PathValue("id"), instanceParam(r))
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), report)
}

// handleEvalSuite 执行一组回归用例并返回汇总。
func (c *Control) handleEvalSuite(w http.ResponseWriter, r *http.Request) {
	if !c.evalEnabled(w, r) {
		return
	}
	var body struct {
		Cases []eval.SuiteCase `json:"cases"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	if len(body.Cases) == 0 {
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "cases 不能为空"))
		return
	}
	out, err := c.eval.RunSuite(r.Context(), r.PathValue("id"), instanceParam(r), body.Cases)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), out)
}
