package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
)

// stdioRequest 是自定义 stdio 协议的请求结构。
// 设计上每行一个 JSON 对象：包含 request_id、tool、params 三个字段。
// 该协议用于在没有官方 MCP SDK 适配层之前的过渡期和本地测试。
type stdioRequest struct {
	RequestID string          `json:"request_id"`
	Tool      string          `json:"tool"`
	Params    json.RawMessage `json:"params"`
}

// stdioResponse 是自定义 stdio 协议的响应结构。
// result 与 error 二选一，request_id 回传便于客户端做并发匹配。
type stdioResponse struct {
	RequestID string `json:"request_id,omitempty"`
	Result    any    `json:"result,omitempty"`
	Error     *Error `json:"error,omitempty"`
}

// StdioServer 提供基于 stdin/stdout 的自定义 JSON 协议实现，
// 用于在没有官方 MCP SDK 适配层时也能本地调通工具链路。
// 当前默认 Serve 路径由 SDKServer 承担，StdioServer 主要作为参考实现保留。
type StdioServer struct {
	registry *Registry
	logger   *slog.Logger
	in       io.Reader
	out      io.Writer
}

// NewStdioServer 创建 stdio JSON 工具服务。后续可替换为正式 MCP SDK 适配层。
func NewStdioServer(registry *Registry, logger *slog.Logger) *StdioServer {
	return &StdioServer{
		registry: registry,
		logger:   logger,
		in:       os.Stdin,
		out:      os.Stdout,
	}
}

// Serve 按行读取 JSON 请求并返回 JSON 响应。
// 协议格式：每行一个 JSON 对象，包含 request_id、tool、params 字段。
// 响应格式：每行一个 JSON 对象，包含 request_id、result、error 字段。
// 设计说明：日志写入 stderr，不污染 stdout 协议流；支持 context 取消实现优雅关闭。
func (s *StdioServer) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.in)
	encoder := json.NewEncoder(s.out)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var req stdioRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			_ = encoder.Encode(stdioResponse{Error: &Error{
				ErrorCode: "VALIDATION_FAILED",
				Message:   "invalid json request",
				Retryable: false,
			}})
			continue
		}
		result, toolErr := s.registry.Call(ctx, req.Tool, req.Params)
		_ = encoder.Encode(stdioResponse{
			RequestID: req.RequestID,
			Result:    result,
			Error:     toolErr,
		})
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	s.logger.Info("stdio server stopped")
	return nil
}

// errUnsupportedTransport 表示所选传输类型暂未实现。
// 当前仅 stdio 由 SDKServer 实现，Serve 之前的传输协商若不在白名单内会返回该错误。
var errUnsupportedTransport = errors.New("unsupported transport")
