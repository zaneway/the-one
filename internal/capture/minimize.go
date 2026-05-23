package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zaneway/the-one/internal/config"
)

var forbiddenRawFields = []string{"full_text", "full_output", "full_diff"}

// CheckMinimizedObserve 执行 P2 observe 内容最小化硬边界检查。
//
// P2 不做复杂脱敏和自动摘要；Adapter 必须先发送摘要、关键片段、hash 和 source ref。
// 服务端发现完整全文字段、超长摘要或超长引用时直接拒绝，避免 raw_event 退化为隐藏日志库。
func CheckMinimizedObserve(cfg config.CaptureConfig, req ObserveRequest) error {
	if len([]rune(req.InputSummary)) > cfg.MaxInputSummaryChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: input_summary exceeds max_input_summary_chars=%d", cfg.MaxInputSummaryChars)
	}
	if len([]rune(req.OutputSummary)) > cfg.MaxOutputSummaryChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: output_summary exceeds max_output_summary_chars=%d", cfg.MaxOutputSummaryChars)
	}
	if len([]rune(req.ContentSummary)) > cfg.MaxContentSummaryChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: content_summary exceeds max_content_summary_chars=%d", cfg.MaxContentSummaryChars)
	}
	if len(req.Keywords) > cfg.MaxKeywordCount {
		return fmt.Errorf("CONTENT_TOO_LARGE: keywords exceeds max_keyword_count=%d", cfg.MaxKeywordCount)
	}
	if len(req.SalientSpans) > cfg.MaxSalientSpanCount {
		return fmt.Errorf("CONTENT_TOO_LARGE: salient_spans exceeds max_salient_span_count=%d", cfg.MaxSalientSpanCount)
	}
	for _, span := range req.SalientSpans {
		if len([]rune(span)) > cfg.MaxSalientSpanChars {
			return fmt.Errorf("CONTENT_TOO_LARGE: salient_span exceeds max_salient_span_chars=%d", cfg.MaxSalientSpanChars)
		}
	}
	sourceRefsJSON, err := json.Marshal(req.SourceRefs)
	if err != nil {
		return fmt.Errorf("VALIDATION_FAILED: source_refs is not json serializable: %w", err)
	}
	if len([]rune(string(sourceRefsJSON))) > cfg.MaxSourceRefsChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: source_refs exceeds max_source_refs_chars=%d", cfg.MaxSourceRefsChars)
	}
	if containsForbiddenRawField(string(sourceRefsJSON)) {
		return fmt.Errorf("CONTENT_TOO_LARGE: source_refs must not contain full_text/full_output/full_diff")
	}
	return nil
}

// ComputeContentHash 根据最小化后的事件字段生成稳定 hash，不要求 Adapter 回传完整原文。
func ComputeContentHash(req ObserveRequest) (string, error) {
	payload := struct {
		EventType      string      `json:"event_type"`
		AgentType      string      `json:"agent_type"`
		WorkspaceID    string      `json:"workspace_id"`
		ProjectID      string      `json:"project_id"`
		RepoID         string      `json:"repo_id"`
		Actor          string      `json:"actor"`
		ToolName       string      `json:"tool_name"`
		InputSummary   string      `json:"input_summary"`
		OutputSummary  string      `json:"output_summary"`
		ContentSummary string      `json:"content_summary"`
		Keywords       []string    `json:"keywords"`
		SalientSpans   []string    `json:"salient_spans"`
		SourceRefs     []SourceRef `json:"source_refs"`
	}{
		EventType:      req.EventType,
		AgentType:      req.AgentType,
		WorkspaceID:    req.WorkspaceID,
		ProjectID:      req.ProjectID,
		RepoID:         req.RepoID,
		Actor:          req.Actor,
		ToolName:       req.ToolName,
		InputSummary:   req.InputSummary,
		OutputSummary:  req.OutputSummary,
		ContentSummary: req.ContentSummary,
		Keywords:       req.Keywords,
		SalientSpans:   req.SalientSpans,
		SourceRefs:     req.SourceRefs,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("VALIDATION_FAILED: compute content hash: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// DedupKey 返回 P2 raw_event 幂等键。调用方应先执行 NormalizeObserve 和 CheckMinimizedObserve。
func DedupKey(req ObserveRequest) (string, error) {
	contentHash := strings.TrimSpace(req.ContentHash)
	if contentHash == "" {
		computed, err := ComputeContentHash(req)
		if err != nil {
			return "", err
		}
		contentHash = computed
	}
	if req.SessionID != "" {
		return strings.Join([]string{contentHash, req.SessionID, req.EventType}, "|"), nil
	}
	return strings.Join([]string{contentHash, req.SourceChannel, req.WorkspaceID, req.ProjectID, req.RepoID, req.EventType}, "|"), nil
}

func containsForbiddenRawField(value string) bool {
	lower := strings.ToLower(value)
	for _, field := range forbiddenRawFields {
		if strings.Contains(lower, field) {
			return true
		}
	}
	return false
}
