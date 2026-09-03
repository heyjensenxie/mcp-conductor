// Package console 把前端构建产物内嵌进 Go 二进制。
//
// `dist/` 目录在容器构建时由 web/dist 复制而来；本地未构建前端时使用
// 占位 index.html（提示先运行 npm build）。这样 /mcp 与 /api 之外的路径
// 由单个二进制直接服务，满足 `./mcp-conductor` 即可访问 Console 的目标。
package console

import "embed"

//go:embed all:dist
var FS embed.FS
