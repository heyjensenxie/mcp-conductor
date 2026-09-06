// command conductor 是 MCP Conductor 后端的统一入口。
//
// 单二进制承载 Gateway Runtime + Control Plane API + Registry，
// 匹配 PRD「./mcp-conductor 后访问 :8080」的开发体验目标。
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/heyjensenxie/mcp-conductor/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		log.Printf("MCP Conductor 退出: %v", err)
		os.Exit(1)
	}
}
