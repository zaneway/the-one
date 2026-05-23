package ingest

import (
	"fmt"
	"strings"

	"github.com/zaneway/the-one/internal/config"
)

// MinimizationInput 是 P1 内容边界检查输入。
type MinimizationInput struct {
	Content               string
	EvidenceStatement     string
	Keywords              []string
	SalientSpans          []string
	ReviewCheckpointJSONs []string
}

// CheckMinimizedContent 执行 P1 内容最小化硬边界检查。P1 不做复杂脱敏，超界直接拒绝写入。
func CheckMinimizedContent(cfg config.MemoryConfig, input MinimizationInput) error {
	if len([]rune(input.Content)) > cfg.MaxContentChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: content exceeds max_content_chars=%d", cfg.MaxContentChars)
	}
	if len([]rune(input.EvidenceStatement)) > cfg.MaxEvidenceChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: evidence.interpreted_statement exceeds max_evidence_chars=%d", cfg.MaxEvidenceChars)
	}
	if len(input.Keywords) > cfg.MaxKeywordCount {
		return fmt.Errorf("CONTENT_TOO_LARGE: keywords exceeds max_keyword_count=%d", cfg.MaxKeywordCount)
	}
	if len(input.SalientSpans) > cfg.MaxSalientSpanCount {
		return fmt.Errorf("CONTENT_TOO_LARGE: salient_spans exceeds max_salient_span_count=%d", cfg.MaxSalientSpanCount)
	}
	for _, raw := range input.ReviewCheckpointJSONs {
		if strings.Contains(strings.ToLower(raw), "full_text") {
			return fmt.Errorf("CONTENT_TOO_LARGE: review checkpoint must not contain full_text")
		}
	}
	return nil
}
