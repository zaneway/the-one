package ingest

import (
	"fmt"
	"strings"

	"github.com/zaneway/theone/internal/config"
)

// MinimizationInput P1 内容边界检查的输入结构体
// 封装需要进行最小化校验的所有字段，用于 CheckMinimizedContent 函数
// 设计目的：将需要校验的字段集中管理，便于扩展和维护
type MinimizationInput struct {
	Content               string   // 记忆内容主体，即 memory.remember 的 content 字段
	EvidenceStatement     string   // 证据的 interpreted_statement 字段，用于记忆溯源
	Keywords              []string // 关键词列表，用于检索和分类
	SalientSpans          []string // 显著片段列表，标记内容中的关键信息
	ReviewCheckpointJSONs []string // 审查检查点的 JSON 序列化列表，用于 review_checkpoint 类型记忆
}

// CheckMinimizedContent 执行 P1 内容最小化硬边界检查
// 校验规则：
// 1. content 字符数不超过 max_content_chars（默认4000）
// 2. evidence.interpreted_statement 字符数不超过 max_evidence_chars（默认1200）
// 3. keywords 数组长度不超过 max_keyword_count（默认30）
// 4. salient_spans 数组长度不超过 max_salient_span_count（默认10）
// 5. review_checkpoint JSON 中禁止包含 full_text 字段
// 设计说明：
// - P1 不做复杂脱敏和自动摘要，超界直接拒绝写入
// - 使用 rune 长度而非字节长度，正确处理中文等多字节字符
// - review_checkpoint 禁止 full_text 是为了避免存储完整代码或文档内容
// - 错误码 CONTENT_TOO_LARGE 用于客户端识别并调整内容大小
func CheckMinimizedContent(cfg config.MemoryConfig, input MinimizationInput) error {
	// 校验 content 字段长度，使用 rune 计算正确处理中文字符
	if len([]rune(input.Content)) > cfg.MaxContentChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: content exceeds max_content_chars=%d", cfg.MaxContentChars)
	}
	// 校验 evidence.interpreted_statement 字段长度
	if len([]rune(input.EvidenceStatement)) > cfg.MaxEvidenceChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: evidence.interpreted_statement exceeds max_evidence_chars=%d", cfg.MaxEvidenceChars)
	}
	// 校验 keywords 数组长度
	if len(input.Keywords) > cfg.MaxKeywordCount {
		return fmt.Errorf("CONTENT_TOO_LARGE: keywords exceeds max_keyword_count=%d", cfg.MaxKeywordCount)
	}
	// 校验 salient_spans 数组长度
	if len(input.SalientSpans) > cfg.MaxSalientSpanCount {
		return fmt.Errorf("CONTENT_TOO_LARGE: salient_spans exceeds max_salient_span_count=%d", cfg.MaxSalientSpanCount)
	}
	// 校验 review_checkpoint JSON 中是否包含禁止的 full_text 字段
	// full_text 可能包含完整的代码或文档内容，违反内容最小化原则
	for _, raw := range input.ReviewCheckpointJSONs {
		if strings.Contains(strings.ToLower(raw), "full_text") {
			return fmt.Errorf("CONTENT_TOO_LARGE: review checkpoint must not contain full_text")
		}
	}
	return nil
}
