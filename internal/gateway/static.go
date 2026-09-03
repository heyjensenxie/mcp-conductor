package gateway

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/xmj128/mcp-conductor/internal/console"
)

// spaHandler 服务内嵌的前端静态资源，并对非文件路径回退到 index.html。
//
// 与 /api、/mcp 在同一 mux 下：更具体的路径由接口路由命中，其余走 SPA。
func spaHandler() http.Handler {
	sub, err := fs.Sub(console.FS, "dist")
	if err != nil {
		// 理论上 embed 保证目录存在；防御性兜底。
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "" || clean == "index.html" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := sub.Open(clean); err != nil {
			// 未命中静态文件（前端路由），回退 index.html 交给 SPA 路由。
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
