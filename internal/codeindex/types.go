package codeindex

import (
	"context"
	"errors"

	"github.com/zaneway/the-one/internal/memory"
)

// ErrUnsupported 表示 local_basic 不能可靠提供调用图、影响面等高级 Code Index 能力。
var ErrUnsupported = errors.New("CODE_INDEX_UNSUPPORTED")

// Capabilities 描述 Code Index Adapter 当前可提供的能力。
type Capabilities struct {
	// Provider Adapter 名称。
	Provider string

	// FilePathResolve 是否支持文件路径定位。
	FilePathResolve bool

	// SymbolResolve 是否支持轻量符号定位。
	SymbolResolve bool

	// CallGraph 是否支持精确调用图。
	CallGraph bool

	// Impact 是否支持影响面分析。
	Impact bool
}

// Adapter 定义 P4-C3 Code Index Adapter 的最小接口。
// local_basic 只实现 code_ref resolve；调用图、影响面和 LSP 能力必须明确返回 unsupported。
type Adapter interface {
	// Name 返回 Adapter 名称。
	Name() string

	// Capabilities 返回 Adapter 能力，供 diagnostics 和降级决策使用。
	Capabilities(ctx context.Context) (Capabilities, error)

	// ResolveCodeRefs 对已有 code_ref 做 best-effort 文件/符号解析，不返回源码正文。
	ResolveCodeRefs(ctx context.Context, refs []memory.CodeRef) ([]memory.CodeRef, error)
}
