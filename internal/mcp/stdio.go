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

type stdioRequest struct {
	RequestID string          `json:"request_id"`
	Tool      string          `json:"tool"`
	Params    json.RawMessage `json:"params"`
}

type stdioResponse struct {
	RequestID string `json:"request_id,omitempty"`
	Result    any    `json:"result,omitempty"`
	Error     *Error `json:"error,omitempty"`
}

type StdioServer struct {
	registry *Registry
	logger   *slog.Logger
	in       io.Reader
	out      io.Writer
}

// NewStdioServer 创建 P0 stdio JSON 工具服务。后续可替换为正式 MCP SDK 适配层。
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

var errUnsupportedTransport = errors.New("unsupported transport")
