package gateway

import (
	"net/http"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
)

// handleGetTool 读取单个工具的完整元数据（含 source_* 与 *_overridden），
// 供 Console 工具详情页展示与手动调用前取参。
func (c *Control) handleGetTool(w http.ResponseWriter, r *http.Request) {
	tool, err := c.store.GetTool(r.Context(), r.PathValue("id"))
	if err != nil {
		writeGatewayError(w, r, http.StatusNotFound,
			errs.Wrap(errs.CodeNotFound, err, "读取 Tool 失败"))
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), tool)
}
