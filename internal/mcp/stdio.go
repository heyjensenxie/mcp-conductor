package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// stdio 传输（官方 2025-11-25 规范）：客户端以子进程方式启动 MCP Server，
// 换行分隔的 JSON-RPC 消息经 stdin/stdout 双向传递；stderr 仅作日志、不得
// 视为错误；生命周期须先完成 initialize → notifications/initialized 再发其他请求。
//
// StdioClient 采用"每次操作新建进程"（spawn-per-dial）的生命周期策略：pipeline
// 每次拨测/发现/调用都新建一个 StdioClient，用完由上层 defer Close() 回收。
// 进程启动到初始化有现实开销（Node 类服务器 ~1-2s），MVP 接受该成本，长驻会话
// 缓存留待后续版本。

// 单条 stdio 消息的长度上限（字节）：防超大行撑爆内存；超出视为协议错误。
const stdioMaxMessage = 16 << 20 // 16 MiB

// 进程优雅退出的等待时间；超时后强制 Kill，避免僵尸占用。
const stdioCloseGrace = 2 * time.Second

// StdioClient 是基于本地子进程的最小 MCP stdio 客户端。
//
// 同一实例内的 stdin/stdout 流是单工的，且 MCP 服务端通常按请求顺序应答，
// 因此全部请求/响应在互斥锁内串行执行；一个 StdioClient 只服务一次逻辑
// 操作（拨测/发现/调用）内的一小撮连续请求，不存在并发交错。
type StdioClient struct {
	command string
	args    []string
	env     []string // 子进程额外环境变量（追加到当前环境；测试注入用）

	mu        sync.Mutex // 串行化管道上的请求/应答
	cmd       *exec.Cmd  // 已启动的子进程句柄（nil = 未启动）
	stop      context.CancelFunc
	stdin     io.WriteCloser
	stdout    *bufio.Scanner
	started   bool // 进程是否已启动
	initialed bool // 完成 initialize 握手（含 initialized 通知）
	closeOnce sync.Once
	closed    bool
	stderr    *limitedBuffer // 捕获 stderr 尾段供失败诊断；不视为错误
	nextID    atomic.Int64
}

// NewStdioClient 创建 stdio 客户端；command 为可执行文件，args 为其参数
// （与 MCP 规范中 stdio 配置的 command/args 一致，不经过 shell）。
func NewStdioClient(command string, args []string) *StdioClient {
	return &StdioClient{command: command, args: args, stderr: newLimitedBuffer(4 << 10)}
}

// WithStdioEnv 追加子进程环境变量（追加在当前进程环境之上）。通常仅测试注入。
func (c *StdioClient) WithStdioEnv(kv ...string) *StdioClient {
	c.env = append(c.env, kv...)
	return c
}

// Initialize 触发握手并返回服务器信息（进程首次使用先启动子进程）。
func (c *StdioClient) Initialize(ctx context.Context) (*InitializeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initializeLocked(ctx)
}

// ListTools 发现上游工具定义（若尚未握手则先完成握手）。
func (c *StdioClient) ListTools(ctx context.Context) ([]Tool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureSessionLocked(ctx); err != nil {
		return nil, err
	}
	var out ListToolsResult
	if err := c.requestLocked(ctx, "tools/list", ListToolsRequestParams{}, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// CallTool 调用上游工具并返回结果（若尚未握手则先完成握手）。
func (c *StdioClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*CallToolResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureSessionLocked(ctx); err != nil {
		return nil, err
	}
	var out CallToolResult
	if err := c.requestLocked(ctx, "tools/call", CallToolRequestParams{Name: name, Arguments: arguments}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Close 终止子进程并回收资源（幂等）：先关闭 stdin 通知正常退出，短暂等待后
// 未退出再强制 Kill。已通过 Context 取消的进程 Wait 会立即返回。
func (c *StdioClient) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		stdin := c.stdin
		cmd := c.cmd
		stop := c.stop
		c.closed = true
		c.mu.Unlock()
		if stdin != nil {
			_ = stdin.Close() // EOF：请求服务端优雅结束
		}
		if cmd != nil && cmd.Process != nil {
			done := make(chan struct{})
			go func() { _ = cmd.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(stdioCloseGrace):
				stop()
				<-done
			}
		}
	})
	return nil
}

// ensureSessionLocked 保证进程已启动且完成 initialize 握手（调用方持有 mu）。
func (c *StdioClient) ensureSessionLocked(ctx context.Context) error {
	if c.initialed {
		return nil
	}
	if _, err := c.initializeLocked(ctx); err != nil {
		return err
	}
	return nil
}

// initializeLocked 启动进程（若未启动）、发送 initialize、按规范发送
// notifications/initialized，并把 initialed 置位。调用方持有 mu。
func (c *StdioClient) initializeLocked(ctx context.Context) (*InitializeResult, error) {
	if err := c.startLocked(ctx); err != nil {
		return nil, err
	}
	params := InitializeRequestParams{
		ProtocolVersion: DefaultProtocolVersion,
		ClientInfo:      Implementation{Name: "mcp-conductor", Version: ServerVersion},
		Capabilities:    Capabilities{Tools: &ToolCapabilities{}},
	}
	var out InitializeResult
	if err := c.requestLocked(ctx, "initialize", params, &out); err != nil {
		return nil, err
	}
	// 规范必须：会话建立后发送 initialized 通知（点对点，无响应）。
	c.notifyLocked("notifications/initialized")
	c.initialed = true
	return &out, nil
}

// startLocked 惰性启动子进程并装配管道。调用方持有 mu。
func (c *StdioClient) startLocked(ctx context.Context) error {
	if c.started {
		return nil
	}
	if c.command == "" {
		return fmt.Errorf("注册了一个空命令的 stdio 实例")
	}
	// 子进程属于 StdioClient 会话，而不是触发首次启动的那个请求。请求 Context
	// 仍由 requestLocked 负责超时取消；Close 负责终止整个会话。
	processCtx, stop := context.WithCancel(context.Background())
	cmd := exec.CommandContext(processCtx, c.command, c.args...)
	if len(c.env) > 0 {
		cmd.Env = append(os.Environ(), c.env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		stop()
		return fmt.Errorf("创建 stdio 子进程 stdin 管道失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stop()
		return fmt.Errorf("创建 stdio 子进程 stdout 管道失败: %w", err)
	}
	// stderr 只作日志：捕获尾段供失败诊断，但不视为错误（规范明确）。
	cmd.Stderr = c.stderr
	if err := cmd.Start(); err != nil {
		stop()
		return fmt.Errorf("启动 stdio 子进程 %q 失败: %w", c.command, err)
	}
	// 放大 Scanner 缓冲，适配大文本工具结果；超出上限按协议错误返回。
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), stdioMaxMessage)
	c.cmd = cmd
	c.stop = stop
	c.stdin = stdin
	c.stdout = scanner
	c.started = true
	return nil
}

// notifyLocked 发送一个点对点 JSON-RPC 通知（无 id、期待无响应）。调用方持有 mu。
func (c *StdioClient) notifyLocked(method string) {
	// 通知写失败（进程提前退出等）不阻断主流程，交由后续请求暴露。
	_, _ = io.WriteString(c.stdin, fmt.Sprintf(`{"jsonrpc":"2.0","method":%q}%s`, method, "\n"))
}

// requestLocked 编码并发送一次 JSON-RPC 请求，读取应答并解码 result 到 out。
// 服务端期间推送的通知、未匹配的响应按换行协议跳过，直到找到匹配 id 的响应。
// 调用方持有 mu。
func (c *StdioClient) requestLocked(ctx context.Context, method string, params any, out any) error {
	requestDone := make(chan struct{})
	defer close(requestDone)
	go func() {
		select {
		case <-ctx.Done():
			// 管道读取在各平台未必支持 deadline；终止进程可可靠解除阻塞。
			if c.stop != nil {
				c.stop()
			}
		case <-requestDone:
		}
	}()

	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("序列化请求参数失败: %w", err)
	}
	id := json.RawMessage(fmt.Sprintf("%d", c.nextID.Add(1)))
	envelope := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}
	if err := c.writeLine(ctx, body); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("等待 stdio 应答超时: %w (%s)", err, c.stderr.snapshot())
		}
		var resp struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result,omitempty"`
			Error  *RPCError       `json:"error,omitempty"`
		}
		if err := c.readMessage(ctx, &resp); err != nil {
			return err
		}
		if resp.Method != "" {
			// 通知 / 服务端请求：当前不处理（MVP 不做 sampling 等应答），跳过。
			continue
		}
		if string(resp.ID) != string(id) {
			// 串行请求下一个不匹配的响应理论上不会出现；防御性跳过。
			continue
		}
		if resp.Error != nil {
			return &RPCErrorResponse{Code: resp.Error.Code, Message: resp.Error.Message}
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("解码 stdio 应答失败: %w", err)
		}
		return nil
	}
}

// writeLine 把一条请求写入 stdin。Context 过期或进程退出都会让写入及时失败。
func (c *StdioClient) writeLine(ctx context.Context, body []byte) error {
	// 断言底层管道文件以设置写超时，进程存活但不读取时也能及时返回。
	if f, ok := c.stdin.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = f.SetWriteDeadline(deadlineFrom(ctx))
	}
	if _, err := c.stdin.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("写入 stdio 子进程失败: %w (%s)", err, c.stderr.snapshot())
	}
	return nil
}

// readMessage 读取一条换行分隔的 JSON-RPC 消息并解码到 v。
func (c *StdioClient) readMessage(ctx context.Context, v any) error {
	if !c.stdout.Scan() {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("等待 stdio 应答超时: %w (%s)", err, c.stderr.snapshot())
		}
		if err := c.stdout.Err(); err != nil {
			return fmt.Errorf("读取 stdio 应答失败: %w (%s)", err, c.stderr.snapshot())
		}
		return fmt.Errorf("stdio 子进程提前退出，未收到应答 (%s)", c.stderr.snapshot())
	}
	if err := json.Unmarshal(c.stdout.Bytes(), v); err != nil {
		return fmt.Errorf("解析 stdio 应答失败: %w (%s)", err, c.stderr.snapshot())
	}
	return nil
}

// deadlineFrom 由 Context 推导读写超时；无 deadline 时给出一个长兜底（15s），
// 避免子进程存活但沉默时无限期阻塞当前调用（调用方通常自带更短的超时）。
func deadlineFrom(ctx context.Context) time.Time {
	if dl, ok := ctx.Deadline(); ok {
		return dl
	}
	return time.Now().Add(15 * time.Second)
}

// limitedBuffer 保留最近写入内容尾段大小的字节，防止恶意/冗长 stderr 撑爆内存。
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func newLimitedBuffer(max int) *limitedBuffer { return &limitedBuffer{max: max} }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Write(p)
	// 保留尾部 max 字节：前面溢出部分直接丢弃。
	if n := b.buf.Len(); n > b.max {
		drop := n - b.max
		rest := b.buf.Bytes()[n-drop:]
		b.buf.Reset()
		b.buf.Write(rest)
	}
	return len(p), nil
}

func (b *limitedBuffer) snapshot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.buf.String()
	if len(s) == 0 {
		return ""
	}
	return "stderr: " + s
}
