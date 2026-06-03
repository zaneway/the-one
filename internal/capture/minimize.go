package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zaneway/theone/internal/config"
)

// forbiddenRawFields 禁止出现在 source_refs 中的原始内容字段列表
// 这些字段可能包含完整的工具输出、代码差异等内容，违反内容最小化原则
var forbiddenRawFields = []string{"full_text", "full_output", "full_diff"}

// CheckMinimizedObserve 执行 observe 内容最小化硬边界检查
// 校验规则：
// 1. input_summary 字符数不超过 max_input_summary_chars（默认1200）
// 2. output_summary 字符数不超过 max_output_summary_chars（默认2000）
// 3. content_summary 字符数不超过 max_content_summary_chars（默认6000）
// 4. keywords 数组长度不超过 max_keyword_count（默认30）
// 5. salient_spans 数组长度不超过 max_salient_span_count（默认10）
// 6. 单个 salient_span 字符数不超过 max_salient_span_chars（默认500）
// 7. source_refs JSON 序列化后长度不超过 max_source_refs_chars（默认4000）
// 8. source_refs 中禁止包含 full_text/full_output/full_diff 字段
// 设计说明：
// - 不做复杂脱敏和自动摘要，Adapter 必须先发送摘要、关键片段、hash 和 source ref
// - 服务端发现完整全文字段、超长摘要或超长引用时直接拒绝
// - 目的是避免 raw_event 退化为隐藏日志库，确保存储的都是最小化后的内容
// - 使用 rune 长度而非字节长度，正确处理中文等多字节字符
func CheckMinimizedObserve(cfg config.CaptureConfig, req ObserveRequest) error {
	// 校验输入摘要长度
	if len([]rune(req.InputSummary)) > cfg.MaxInputSummaryChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: input_summary exceeds max_input_summary_chars=%d", cfg.MaxInputSummaryChars)
	}
	// 校验输出摘要长度
	if len([]rune(req.OutputSummary)) > cfg.MaxOutputSummaryChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: output_summary exceeds max_output_summary_chars=%d", cfg.MaxOutputSummaryChars)
	}
	// 校验内容摘要长度
	if len([]rune(req.ContentSummary)) > cfg.MaxContentSummaryChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: content_summary exceeds max_content_summary_chars=%d", cfg.MaxContentSummaryChars)
	}
	// 校验关键词数量
	if len(req.Keywords) > cfg.MaxKeywordCount {
		return fmt.Errorf("CONTENT_TOO_LARGE: keywords exceeds max_keyword_count=%d", cfg.MaxKeywordCount)
	}
	// 校验显著片段数量
	if len(req.SalientSpans) > cfg.MaxSalientSpanCount {
		return fmt.Errorf("CONTENT_TOO_LARGE: salient_spans exceeds max_salient_span_count=%d", cfg.MaxSalientSpanCount)
	}
	// 校验单个显著片段的长度
	for _, span := range req.SalientSpans {
		if len([]rune(span)) > cfg.MaxSalientSpanChars {
			return fmt.Errorf("CONTENT_TOO_LARGE: salient_span exceeds max_salient_span_chars=%d", cfg.MaxSalientSpanChars)
		}
	}
	// 序列化 source_refs 为 JSON 并校验长度
	sourceRefsJSON, err := json.Marshal(req.SourceRefs)
	if err != nil {
		return fmt.Errorf("VALIDATION_FAILED: source_refs is not json serializable: %w", err)
	}
	if len([]rune(string(sourceRefsJSON))) > cfg.MaxSourceRefsChars {
		return fmt.Errorf("CONTENT_TOO_LARGE: source_refs exceeds max_source_refs_chars=%d", cfg.MaxSourceRefsChars)
	}
	// 校验 source_refs 中是否包含禁止的原始内容字段
	if containsForbiddenRawField(string(sourceRefsJSON)) {
		return fmt.Errorf("CONTENT_TOO_LARGE: source_refs must not contain full_text/full_output/full_diff")
	}
	return nil
}

// ComputeContentHash 根据最小化后的事件字段生成稳定的 SHA256 哈希值
// 输入字段：event_type、agent_type、workspace_id、project_id、repo_id、actor、
//
//	tool_name、input_summary、output_summary、content_summary、
//	keywords、salient_spans、source_refs
//
// 输出格式：sha256:<hex_string>
// 设计说明：
// - 不要求 Adapter 回传完整原文，只使用已有的最小化字段计算哈希
// - 使用 JSON 序列化保证字段顺序一致，确保相同内容生成相同哈希
// - 用于事件去重（dedup）和幂等写入
// - 哈希算法选择 SHA256 保证碰撞概率极低
func ComputeContentHash(req ObserveRequest) (string, error) {
	// 定义用于哈希计算的匿名结构体，只包含影响内容身份的字段
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
	// 序列化为 JSON 保证字段顺序一致
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("VALIDATION_FAILED: compute content hash: %w", err)
	}
	// 计算 SHA256 哈希值
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// DedupKey 返回 raw_event 的幂等键（去重键）
// 键生成规则：
// - 有 session_id 时：content_hash|session_id|event_type
// - 无 session_id 时：content_hash|source_channel|workspace_id|project_id|repo_id|event_type
// 设计说明：
// - 调用方应先执行 NormalizeObserve 和 CheckMinimizedObserve
// - 如果请求未提供 content_hash，会自动调用 ComputeContentHash 计算
// - 使用 "|" 作为分隔符，确保各字段不会混淆
// - 有 session_id 时优先使用它作为上下文标识，因为 session 是更精确的隔离边界
// - 无 session_id 时使用 workspace+project+repo 作为上下文标识
func DedupKey(req ObserveRequest) (string, error) {
	// 如果请求未提供 content_hash，自动计算
	contentHash := strings.TrimSpace(req.ContentHash)
	if contentHash == "" {
		computed, err := ComputeContentHash(req)
		if err != nil {
			return "", err
		}
		contentHash = computed
	}
	// 有 session_id 时，使用更精确的上下文标识
	if req.SessionID != "" {
		return strings.Join([]string{contentHash, req.SessionID, req.EventType}, "|"), nil
	}
	// 无 session_id 时，使用 workspace+project+repo 作为上下文标识
	return strings.Join([]string{contentHash, req.SourceChannel, req.WorkspaceID, req.ProjectID, req.RepoID, req.EventType}, "|"), nil
}

// containsForbiddenRawField 检查 JSON 字符串中是否包含禁止的原始内容字段
// 禁止字段：full_text、full_output、full_diff
// 设计说明：
// - 使用小写比较避免大小写绕过
// - 这些字段可能包含完整的工具输出、代码差异等内容，违反内容最小化原则
// - 在 source_refs 中发现这些字段时，应拒绝写入并返回 CONTENT_TOO_LARGE 错误
func containsForbiddenRawField(value string) bool {
	lower := strings.ToLower(value)
	for _, field := range forbiddenRawFields {
		if strings.Contains(lower, field) {
			return true
		}
	}
	return false
}
