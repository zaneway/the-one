package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/idgen"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/prompts"
	"github.com/zaneway/theone/internal/scoring"
)

const (
	OpenAIProviderName = "openai"
	// providerLogBodyMaxChars 限制单条日志字段长度，避免 prompt/事件正文撑爆日志文件。
	providerLogBodyMaxChars = 32000
	providerReasoningOpen   = "<" + "think" + ">"
	providerReasoningClose  = "</" + "think" + ">"
)

// OpenAIProviderConfig 是 OpenAI processor 的运行时配置。
// APIKey 由调用方从环境变量或测试替身注入，避免配置文件保存明文密钥。
type OpenAIProviderConfig struct {
	APIKey                   string
	BaseURL                  string
	Model                    string
	Timeout                  time.Duration
	MaxOutputTokens          int64
	HTTPClient               *http.Client
	ExtractEvidencePrompt    string
	GenerateCandidatesPrompt string
	SemanticEnhancePrompt    string
	Logger                   *slog.Logger
}

type OpenAIProvider struct {
	client                   openai.Client
	baseURL                  string
	model                    string
	timeout                  time.Duration
	maxOutputTokens          int64
	processRawEventPrompt    string
	extractEvidencePrompt    string
	generateCandidatesPrompt string
	semanticEnhancePrompt    string
	logger                   *slog.Logger
}

// callLogMeta 记录单次 provider 调用的业务关联字段，便于失败时定位具体请求。
type callLogMeta struct {
	TaskName   string
	RawEventID string
	EventType  string
	SessionID  string
	TaskID     string
	EvidenceID string
}

func NewOpenAIProvider(cfg OpenAIProviderConfig) (OpenAIProvider, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return OpenAIProvider{}, fmt.Errorf("CONFIG_INVALID: openai api key is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return OpenAIProvider{}, fmt.Errorf("CONFIG_INVALID: openai model is required")
	}
	if cfg.Timeout <= 0 {
		return OpenAIProvider{}, fmt.Errorf("CONFIG_INVALID: openai timeout must be positive")
	}
	if cfg.MaxOutputTokens <= 0 {
		return OpenAIProvider{}, fmt.Errorf("CONFIG_INVALID: openai max output tokens must be positive")
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL := strings.TrimSpace(cfg.BaseURL); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	return OpenAIProvider{
		client:                   openai.NewClient(opts...),
		baseURL:                  strings.TrimSpace(cfg.BaseURL),
		model:                    model,
		timeout:                  cfg.Timeout,
		maxOutputTokens:          cfg.MaxOutputTokens,
		processRawEventPrompt:    firstNonEmpty(cfg.ExtractEvidencePrompt, prompts.OpenAIProcessRawEventPrompt),
		extractEvidencePrompt:    firstNonEmpty(cfg.ExtractEvidencePrompt, prompts.OpenAIExtractEvidencePrompt),
		generateCandidatesPrompt: firstNonEmpty(cfg.GenerateCandidatesPrompt, prompts.OpenAIGenerateCandidatesPrompt),
		semanticEnhancePrompt:    firstNonEmpty(cfg.SemanticEnhancePrompt, prompts.OpenAISemanticEnhancePrompt),
		logger:                   cfg.Logger,
	}, nil
}

func (p OpenAIProvider) Name() string {
	return OpenAIProviderName
}

// CheckHealth 对 OpenAI 兼容 Chat Completions API 执行轻量结构化探测。
// 该方法只验证外部模型可达性、认证和模型配置，不抽取业务 evidence，也不写入任何存储。
func (p OpenAIProvider) CheckHealth(ctx context.Context) (HealthStatus, error) {
	startedAt := time.Now()
	raw, err := p.callStructured(ctx, "Return a JSON object with ok=true. Do not include any other text.", map[string]any{
		"task":     "health_check",
		"provider": OpenAIProviderName,
		"model":    p.model,
	}, "theone_provider_health", healthSchema(), callLogMeta{TaskName: "health_check"})
	if err != nil {
		return HealthStatus{Provider: OpenAIProviderName, Model: p.model, LatencyMS: time.Since(startedAt).Milliseconds()}, err
	}
	var decoded struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return HealthStatus{Provider: OpenAIProviderName, Model: p.model, LatencyMS: time.Since(startedAt).Milliseconds()}, fmt.Errorf("PROVIDER_INVALID_OUTPUT: decode openai health response: %w", err)
	}
	if !decoded.OK {
		return HealthStatus{Provider: OpenAIProviderName, Model: p.model, LatencyMS: time.Since(startedAt).Milliseconds()}, fmt.Errorf("PROVIDER_INVALID_OUTPUT: openai health response ok=false")
	}
	return HealthStatus{Provider: OpenAIProviderName, Model: p.model, LatencyMS: time.Since(startedAt).Milliseconds()}, nil
}

func (p OpenAIProvider) EnhanceObserve(ctx context.Context, input capture.SemanticEnhanceInput) (capture.SemanticEnhanceOutput, error) {
	raw, err := p.callStructured(ctx, p.semanticEnhancePrompt, map[string]any{
		"task":  "semantic_preserving_observe_simplification",
		"input": input,
	}, "theone_semantic_enhance", semanticEnhanceSchema(), callLogMeta{
		TaskName:  "semantic_preserving_observe_simplification",
		EventType: string(input.EventType),
	})
	if err != nil {
		return capture.SemanticEnhanceOutput{}, err
	}
	var out capture.SemanticEnhanceOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return capture.SemanticEnhanceOutput{}, fmt.Errorf("PROVIDER_INVALID_OUTPUT: decode openai semantic enhancement response: %w", err)
	}
	out.InputSummary = strings.TrimSpace(out.InputSummary)
	out.OutputSummary = strings.TrimSpace(out.OutputSummary)
	out.ContentSummary = strings.TrimSpace(out.ContentSummary)
	out.Keywords = trimStringSlice(out.Keywords)
	out.SalientSpans = trimStringSlice(out.SalientSpans)
	return out, nil
}

func (p OpenAIProvider) ExtractEvidence(ctx context.Context, input EvidenceInput) ([]EvidenceDraft, error) {
	payload := map[string]any{
		"task":      "extract_evidence",
		"raw_event": openAIRawEventView(input.RawEvent),
	}
	raw, err := p.callStructured(ctx, p.extractEvidencePrompt, payload, "theone_evidence", evidenceSchema(), callLogMeta{
		TaskName:   "extract_evidence",
		RawEventID: input.RawEvent.ID,
		EventType:  string(input.RawEvent.EventType),
		SessionID:  input.RawEvent.SessionID,
		TaskID:     input.RawEvent.TaskID,
	})
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Evidence []openAIEvidenceDraft `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("PROVIDER_INVALID_OUTPUT: decode openai evidence response: %w", err)
	}
	out := make([]EvidenceDraft, 0, len(decoded.Evidence))
	for _, evidence := range decoded.Evidence {
		statement := strings.TrimSpace(evidence.InterpretedStatement)
		if statement == "" {
			continue
		}
		out = append(out, EvidenceDraft{
			SourceType:           strings.TrimSpace(evidence.SourceType),
			InterpretedStatement: statement,
			Keywords:             evidence.Keywords,
			SalientSpans:         evidence.SalientSpans,
			SourceRef:            evidence.SourceRef,
			Confidence:           clamp01(evidence.Confidence),
		})
	}
	return out, nil
}

func (p OpenAIProvider) ProcessRawEvent(ctx context.Context, input EvidenceInput) ([]ProcessedEvidence, error) {
	payload := map[string]any{
		"task":      "process_raw_event_memory",
		"raw_event": openAIRawEventCandidateView(input.RawEvent),
	}
	raw, err := p.callStructured(ctx, p.processRawEventPrompt, payload, "theone_event_memory", eventMemorySchema(), callLogMeta{
		TaskName:   "process_raw_event_memory",
		RawEventID: input.RawEvent.ID,
		EventType:  string(input.RawEvent.EventType),
		SessionID:  input.RawEvent.SessionID,
		TaskID:     input.RawEvent.TaskID,
	})
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Evidence []openAIProcessedEvidenceDraft `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("PROVIDER_INVALID_OUTPUT: decode openai event memory response: %w", err)
	}
	out := make([]ProcessedEvidence, 0, len(decoded.Evidence))
	for _, item := range decoded.Evidence {
		evidence, ok := materializeOpenAIEvidenceDraft(item.openAIEvidenceDraft)
		if !ok {
			continue
		}
		eventScore := scoring.ScoreRawEvent(scoring.RawEventInput{
			EventType:      input.RawEvent.EventType,
			OccurredAt:     input.RawEvent.OccurredAt,
			ContentSummary: input.RawEvent.ContentSummary,
			InputSummary:   input.RawEvent.InputSummary,
			OutputSummary:  input.RawEvent.OutputSummary,
			KeywordsJSON:   input.RawEvent.KeywordsJSON,
			SourceRefsJSON: input.RawEvent.SourceRefsJSON,
			Query:          evidence.InterpretedStatement,
			Now:            input.Now,
		})
		candidateInput := CandidateInput{
			Evidence: memory.Evidence{
				SourceType:           evidence.SourceType,
				InterpretedStatement: evidence.InterpretedStatement,
				KeywordsJSON:         mustJSONText(evidence.Keywords),
				Confidence:           evidence.Confidence,
			},
			RawEvent: input.RawEvent,
			Session:  input.Session,
			Task:     input.Task,
			Now:      input.Now,
		}
		candidates := make([]MemoryCandidate, 0, len(item.Candidates))
		for _, draft := range item.Candidates {
			candidate, ok := materializeOpenAICandidate(candidateInput, draft, eventScore, nil)
			if !ok {
				continue
			}
			candidates = append(candidates, candidate)
		}
		out = append(out, ProcessedEvidence{Evidence: evidence, Candidates: candidates})
	}
	return out, nil
}

func (p OpenAIProvider) GenerateCandidates(ctx context.Context, input CandidateInput) ([]MemoryCandidate, error) {
	statement := strings.TrimSpace(input.Evidence.InterpretedStatement)
	if statement == "" {
		return nil, nil
	}
	payload := map[string]any{
		"task":      "generate_memory_candidate",
		"raw_event": openAIRawEventCandidateView(input.RawEvent),
		"evidence": map[string]any{
			"source_type":           strings.TrimSpace(input.Evidence.SourceType),
			"interpreted_statement": statement,
			"keywords":              decodeStringSlice(input.Evidence.KeywordsJSON),
		},
	}
	raw, err := p.callStructured(ctx, p.generateCandidatesPrompt, payload, "theone_candidates", candidatesSchema(), callLogMeta{
		TaskName:   "generate_memory_candidate",
		RawEventID: input.RawEvent.ID,
		EventType:  string(input.RawEvent.EventType),
		SessionID:  input.RawEvent.SessionID,
		TaskID:     input.RawEvent.TaskID,
		EvidenceID: input.Evidence.ID,
	})
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Candidates []openAIMemoryCandidateDraft `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("PROVIDER_INVALID_OUTPUT: decode openai candidates response: %w", err)
	}
	eventScore := scoring.ScoreRawEvent(scoring.RawEventInput{
		EventType:      input.RawEvent.EventType,
		OccurredAt:     input.RawEvent.OccurredAt,
		ContentSummary: input.RawEvent.ContentSummary,
		InputSummary:   input.RawEvent.InputSummary,
		OutputSummary:  input.RawEvent.OutputSummary,
		KeywordsJSON:   input.RawEvent.KeywordsJSON,
		SourceRefsJSON: input.RawEvent.SourceRefsJSON,
		Query:          statement,
		Now:            input.Now,
	})
	evidenceIDs := []string{}
	if input.Evidence.ID != "" {
		evidenceIDs = append(evidenceIDs, input.Evidence.ID)
	}
	out := make([]MemoryCandidate, 0, len(decoded.Candidates))
	for _, draft := range decoded.Candidates {
		candidate, ok := materializeOpenAICandidate(input, draft, eventScore, evidenceIDs)
		if !ok {
			continue
		}
		out = append(out, candidate)
	}
	return out, nil
}

func openAIRawEventView(event capture.RawEvent) map[string]any {
	out := map[string]any{}
	if eventType := strings.TrimSpace(event.EventType); eventType != "" {
		out["event_type"] = eventType
	}
	hasBody := false
	if input := strings.TrimSpace(event.InputSummary); input != "" {
		out["input_summary"] = input
		hasBody = true
	}
	if output := strings.TrimSpace(event.OutputSummary); output != "" {
		out["output_summary"] = output
		hasBody = true
	}
	if !hasBody {
		if summary := strings.TrimSpace(event.ContentSummary); summary != "" {
			out["content_summary"] = summary
		}
	}
	return out
}

func openAIRawEventCandidateView(event capture.RawEvent) map[string]any {
	out := openAIRawEventView(event)
	if refs := decodeSourceRefs(event.SourceRefsJSON); len(refs) > 0 {
		out["source_refs"] = refs
	}
	if event.WorkspaceID != "" {
		out["workspace_id"] = event.WorkspaceID
	}
	if event.ProjectID != "" {
		out["project_id"] = event.ProjectID
	}
	if event.RepoID != "" {
		out["repo_id"] = event.RepoID
	}
	if event.SessionID != "" {
		out["session_id"] = event.SessionID
	}
	if event.TaskID != "" {
		out["task_id"] = event.TaskID
	}
	return out
}

type openAIMemoryCandidateDraft struct {
	MemoryType       string                 `json:"memory_type"`
	Scope            string                 `json:"scope"`
	Content          string                 `json:"content"`
	Title            string                 `json:"title"`
	Keywords         []string               `json:"keywords"`
	CandidateReason  []string               `json:"candidate_reason"`
	Confidence       float64                `json:"confidence"`
	Importance       float64                `json:"importance"`
	ReviewCheckpoint *ReviewCheckpointDraft `json:"review_checkpoint"`
}

func materializeOpenAICandidate(input CandidateInput, draft openAIMemoryCandidateDraft, eventScore float64, evidenceIDs []string) (MemoryCandidate, bool) {
	memoryType := strings.TrimSpace(draft.MemoryType)
	scope := strings.TrimSpace(draft.Scope)
	content := strings.TrimSpace(draft.Content)
	if !isAllowedMemoryType(memoryType) || !isAllowedScope(scope) || content == "" {
		return MemoryCandidate{}, false
	}
	if memoryType == memory.TypeReviewCheckpoint {
		if draft.ReviewCheckpoint == nil || !reviewCheckpointDraftValid(*draft.ReviewCheckpoint) {
			return MemoryCandidate{}, false
		}
	}
	keywords := semanticKeywords(trimStringSlice(draft.Keywords))
	if len(keywords) == 0 {
		keywords = semanticKeywords(decodeStringSlice(input.Evidence.KeywordsJSON))
	}
	workspaceID, userID, projectID, repoID, sessionID := scopedIdentity(input.RawEvent, scope)
	title := strings.TrimSpace(draft.Title)
	if title == "" {
		title = candidateTitle(memoryType, content)
	}
	reasons := trimStringSlice(draft.CandidateReason)
	if len(reasons) == 0 {
		reasons = []string{"openai_classified"}
	}
	sourceType := strings.TrimSpace(input.Evidence.SourceType)
	if sourceType == "" {
		sourceType = "agent_summary"
	}
	return MemoryCandidate{
		MemoryType:        memoryType,
		Scope:             scope,
		WorkspaceID:       workspaceID,
		UserID:            userID,
		ProjectID:         projectID,
		RepoID:            repoID,
		SessionID:         sessionID,
		TaskID:            input.RawEvent.TaskID,
		SourceType:        sourceType,
		Title:             title,
		Content:           content,
		Keywords:          keywords,
		RetrievalCues:     keywords,
		Confidence:        defaultFloat(clamp01(draft.Confidence), defaultFloat(input.Evidence.Confidence, 0.7)),
		Importance:        defaultFloat(clamp01(draft.Importance), defaultImportance(memoryType)),
		EncodingDepth:     2,
		EventScore:        eventScore,
		ReviewCheckpoint:  draft.ReviewCheckpoint,
		CandidateReason:   reasons,
		SourceEvidenceIDs: evidenceIDs,
	}, true
}

func isAllowedMemoryType(memoryType string) bool {
	switch memoryType {
	case memory.TypePreference, memory.TypeRequirement, memory.TypeDecision, memory.TypeConstraint,
		memory.TypeAssumption, memory.TypeOpenIssue, memory.TypeFailure, memory.TypeProjectFact,
		memory.TypeProcedure, memory.TypeTemporaryState, memory.TypeSessionSummary, memory.TypeReviewCheckpoint:
		return true
	default:
		return false
	}
}

func isAllowedScope(scope string) bool {
	switch scope {
	case memory.ScopeUserGlobal, memory.ScopeProjectLocal, memory.ScopeRepoLocal, memory.ScopeSession:
		return true
	default:
		return false
	}
}

func reviewCheckpointDraftValid(draft ReviewCheckpointDraft) bool {
	return len(draft.TargetDocs) > 0 && len(draft.ReviewIntent) > 0 && strings.TrimSpace(draft.Conclusion) != ""
}

type openAIEvidenceDraft struct {
	SourceType           string         `json:"source_type"`
	InterpretedStatement string         `json:"interpreted_statement"`
	Keywords             []string       `json:"keywords"`
	SalientSpans         []string       `json:"salient_spans"`
	SourceRef            map[string]any `json:"source_ref"`
	Confidence           float64        `json:"confidence"`
}

type openAIProcessedEvidenceDraft struct {
	openAIEvidenceDraft
	Candidates []openAIMemoryCandidateDraft `json:"candidates"`
}

func materializeOpenAIEvidenceDraft(evidence openAIEvidenceDraft) (EvidenceDraft, bool) {
	statement := strings.TrimSpace(evidence.InterpretedStatement)
	if statement == "" {
		return EvidenceDraft{}, false
	}
	return EvidenceDraft{
		SourceType:           strings.TrimSpace(evidence.SourceType),
		InterpretedStatement: statement,
		Keywords:             evidence.Keywords,
		SalientSpans:         evidence.SalientSpans,
		SourceRef:            evidence.SourceRef,
		Confidence:           clamp01(evidence.Confidence),
	}, true
}

func mustJSONText(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func (p OpenAIProvider) callStructured(ctx context.Context, instructions string, payload any, schemaName string, schema map[string]any, meta callLogMeta) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("PROVIDER_INPUT_INVALID: encode openai payload: %w", err)
	}
	systemContent, err := chatCompletionSystemContent(instructions, schemaName, schema)
	if err != nil {
		return "", fmt.Errorf("PROVIDER_INPUT_INVALID: encode openai system content: %w", err)
	}
	jsonObjectFormat := shared.NewResponseFormatJSONObjectParam()
	requestBody, err := json.Marshal(map[string]any{
		"model":       p.model,
		"schema_name": schemaName,
		"messages": []map[string]string{
			{"role": "system", "content": systemContent},
			{"role": "user", "content": string(data)},
		},
		"max_tokens":      p.maxOutputTokens,
		"temperature":     0,
		"response_format": jsonObjectFormat,
	})
	if err != nil {
		return "", fmt.Errorf("PROVIDER_INPUT_INVALID: encode openai request log body: %w", err)
	}
	requestID, err := idgen.New("oreq")
	if err != nil {
		requestID = fmt.Sprintf("oreq_%d", time.Now().UnixNano())
	}
	startedAt := time.Now()
	commonFields := p.providerLogFields(ctx, requestID, schemaName, meta, len(data), string(data))
	p.logInfo("openai provider request",
		append(commonFields,
			"timeout_ms", p.timeout.Milliseconds(),
			"request_body", providerLogBody(string(requestBody)),
		)...,
	)
	reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	resp, err := p.client.Chat.Completions.New(reqCtx, openai.ChatCompletionNewParams{
		Model: shared.ChatModel(p.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemContent),
			openai.UserMessage(string(data)),
		},
		MaxTokens:   openai.Int(p.maxOutputTokens),
		Temperature: openai.Float(0),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &jsonObjectFormat,
		},
	}, option.WithRequestTimeout(p.timeout))
	if err != nil {
		failureFields := append(commonFields,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"timeout_ms", p.timeout.Milliseconds(),
			"failure_reason", classifyProviderFailure(err),
			"error", err.Error(),
		)
		if classifyProviderFailure(err) == "client_timeout" {
			failureFields = append(failureFields,
				"hint", "model did not return within configured processor.openai.timeout_ms; consider increasing timeout or using a faster model variant",
			)
		}
		p.logError("openai provider request failed", failureFields...)
		return "", fmt.Errorf("PROVIDER_UNAVAILABLE: openai chat completions request failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		p.logError("openai provider response invalid",
			append(commonFields,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"response_body", providerChatCompletionLogBody(resp, ""),
				"error", "choices is empty",
			)...,
		)
		return "", fmt.Errorf("PROVIDER_INVALID_OUTPUT: openai response choices is empty")
	}
	out := strings.TrimSpace(resp.Choices[0].Message.Content)
	if out == "" {
		p.logError("openai provider response invalid",
			append(commonFields,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"response_body", providerChatCompletionLogBody(resp, ""),
				"error", "message content is empty",
			)...,
		)
		return "", fmt.Errorf("PROVIDER_INVALID_OUTPUT: openai response message content is empty")
	}
	p.logInfo("openai provider response",
		append(commonFields,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"response_body", providerChatCompletionLogBody(resp, out),
		)...,
	)
	normalized, err := normalizeStructuredProviderOutput(out)
	if err != nil {
		p.logError("openai provider response invalid",
			append(commonFields,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"response_body", providerChatCompletionLogBody(resp, out),
				"error", err.Error(),
			)...,
		)
		return "", fmt.Errorf("PROVIDER_INVALID_OUTPUT: normalize openai response: %w", err)
	}
	return normalized, nil
}

// normalizeStructuredProviderOutput 从模型输出中提取可解析的 JSON 文本。
// 兼容直接返回 JSON，以及 reasoning 标签、markdown 围栏等包裹形式。
func normalizeStructuredProviderOutput(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("response is empty")
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed, nil
	}
	cleaned := strings.TrimSpace(stripMarkdownJSONFences(stripReasoningBlocks(trimmed)))
	if cleaned != "" && json.Valid([]byte(cleaned)) {
		return cleaned, nil
	}
	extracted, err := extractLastJSONObject(cleaned)
	if err != nil {
		return "", err
	}
	if !json.Valid([]byte(extracted)) {
		return "", fmt.Errorf("extracted payload is not valid json")
	}
	return extracted, nil
}

func stripReasoningBlocks(s string) string {
	for {
		lower := strings.ToLower(s)
		start := strings.Index(lower, providerReasoningOpen)
		if start < 0 {
			return s
		}
		restLower := lower[start+len(providerReasoningOpen):]
		endRel := strings.Index(restLower, providerReasoningClose)
		if endRel < 0 {
			return strings.TrimSpace(s[:start])
		}
		end := start + len(providerReasoningOpen) + endRel + len(providerReasoningClose)
		s = s[:start] + s[end:]
	}
}

func stripMarkdownJSONFences(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	if idx := strings.Index(trimmed, "\n"); idx >= 0 {
		trimmed = strings.TrimSpace(trimmed[idx+1:])
	}
	trimmed = strings.TrimSuffix(trimmed, "```")
	return strings.TrimSpace(trimmed)
}

func extractLastJSONObject(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("no json object found")
	}
	for i := len(trimmed) - 1; i >= 0; i-- {
		if trimmed[i] != '{' {
			continue
		}
		candidate := extractBalancedJSONObject(trimmed, i)
		if candidate == "" {
			continue
		}
		if json.Valid([]byte(candidate)) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no valid json object found")
}

func extractBalancedJSONObject(s string, start int) string {
	if start < 0 || start >= len(s) || s[start] != '{' {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func (p OpenAIProvider) providerLogFields(ctx context.Context, requestID, schemaName string, meta callLogMeta, payloadBytes int, payloadJSON string) []any {
	fields := []any{
		"request_id", requestID,
		"schema_name", schemaName,
		"model", p.model,
		"payload_bytes", payloadBytes,
		"input_preview", providerInputPreview(payloadJSON),
	}
	if p.baseURL != "" {
		fields = append(fields, "base_url", p.baseURL)
	}
	if meta.TaskName != "" {
		fields = append(fields, "task", meta.TaskName)
	}
	if meta.RawEventID != "" {
		fields = append(fields, "raw_event_id", meta.RawEventID)
	}
	if meta.EventType != "" {
		fields = append(fields, "event_type", meta.EventType)
	}
	if meta.SessionID != "" {
		fields = append(fields, "session_id", meta.SessionID)
	}
	if meta.TaskID != "" {
		fields = append(fields, "task_id", meta.TaskID)
	}
	if meta.EvidenceID != "" {
		fields = append(fields, "evidence_id", meta.EvidenceID)
	}
	if jobID := LogContextFrom(ctx).JobID; jobID != "" {
		fields = append(fields, "job_id", jobID)
	}
	return fields
}

func providerInputPreview(payloadJSON string) string {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &decoded); err == nil {
		if rawEvent, ok := decoded["raw_event"].(map[string]any); ok {
			for _, key := range []string{"content_summary", "input_summary", "output_summary"} {
				if value, ok := rawEvent[key].(string); ok && strings.TrimSpace(value) != "" {
					return providerLogBody(strings.TrimSpace(value))
				}
			}
		}
		if input, ok := decoded["input"].(map[string]any); ok {
			for _, key := range []string{"content_summary", "input_summary", "output_summary", "event_type"} {
				if value, ok := input[key].(string); ok && strings.TrimSpace(value) != "" {
					return providerLogBody(strings.TrimSpace(value))
				}
			}
		}
	}
	return providerLogBody(payloadJSON)
}

func classifyProviderFailure(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "client_timeout"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "context deadline exceeded") || strings.Contains(message, "timeout") {
		return "client_timeout"
	}
	return "provider_error"
}

func chatCompletionSystemContent(instructions, schemaName string, schema map[string]any) (string, error) {
	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(instructions) + "\n\n" +
		"Return exactly one JSON object matching schema " + schemaName + ". " +
		"Do not include markdown fences or any text outside the JSON object.\n" +
		string(schemaJSON), nil
}

func providerChatCompletionLogBody(resp *openai.ChatCompletion, outputText string) string {
	if resp == nil {
		return providerLogBody(fmt.Sprintf(`{"content":%q}`, outputText))
	}
	body, err := json.Marshal(map[string]any{
		"response_id": resp.ID,
		"model":       resp.Model,
		"content":     outputText,
		"usage":       resp.Usage,
	})
	if err != nil {
		return providerLogBody(fmt.Sprintf(`{"marshal_error":%q}`, err.Error()))
	}
	return providerLogBody(string(body))
}

func providerLogBody(value string) string {
	if providerLogBodyMaxChars <= 0 || len(value) <= providerLogBodyMaxChars {
		return value
	}
	return value[:providerLogBodyMaxChars] + "...(truncated)"
}

func (p OpenAIProvider) logInfo(msg string, args ...any) {
	if p.logger == nil {
		return
	}
	p.logger.Info(msg, args...)
}

func (p OpenAIProvider) logError(msg string, args ...any) {
	if p.logger == nil {
		return
	}
	p.logger.Error(msg, args...)
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func semanticEnhanceSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"input_summary", "output_summary", "content_summary", "keywords", "salient_spans", "semantic_equivalent"},
		"properties": map[string]any{
			"input_summary":       stringSchema(),
			"output_summary":      stringSchema(),
			"content_summary":     stringSchema(),
			"keywords":            stringArraySchema(),
			"salient_spans":       stringArraySchema(),
			"semantic_equivalent": map[string]any{"type": "boolean"},
		},
	}
}

func candidatesSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"candidates"},
		"properties": map[string]any{
			"candidates": map[string]any{
				"type":  "array",
				"items": candidateSchema(),
			},
		},
	}
}

func eventMemorySchema() map[string]any {
	evidenceItem := evidenceItemSchema()
	properties := evidenceItem["properties"].(map[string]any)
	properties["candidates"] = map[string]any{
		"type":  "array",
		"items": candidateSchema(),
	}
	evidenceItem["required"] = []string{"source_type", "interpreted_statement", "keywords", "salient_spans", "source_ref", "confidence", "candidates"}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"evidence"},
		"properties": map[string]any{
			"evidence": map[string]any{
				"type":  "array",
				"items": evidenceItem,
			},
		},
	}
}

func candidateSchema() map[string]any {
	checkpointProps := map[string]any{
		"checkpoint_type":    stringSchema(),
		"review_intent":      stringArraySchema(),
		"conclusion":         stringSchema(),
		"confirmed_baseline": stringArraySchema(),
		"ignored_items":      stringArraySchema(),
		"deferred_items":     stringArraySchema(),
		"open_items":         stringArraySchema(),
		"target_docs":        map[string]any{"type": "array"},
		"target_sections":    map[string]any{"type": "array"},
		"target_hashes":      map[string]any{"type": "array"},
		"next_review_policy": map[string]any{"type": "object"},
	}
	return map[string]any{
		"type": "object",
		"required": []string{
			"memory_type", "scope", "content", "keywords", "candidate_reason", "confidence", "importance",
		},
		"properties": map[string]any{
			"memory_type":      stringSchema(),
			"scope":            stringSchema(),
			"content":          stringSchema(),
			"title":            stringSchema(),
			"keywords":         stringArraySchema(),
			"candidate_reason": stringArraySchema(),
			"confidence":       numberSchema(),
			"importance":       numberSchema(),
			"review_checkpoint": map[string]any{
				"type":       "object",
				"properties": checkpointProps,
			},
		},
	}
}

func evidenceSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"evidence"},
		"properties": map[string]any{
			"evidence": map[string]any{
				"type":  "array",
				"items": evidenceItemSchema(),
			},
		},
	}
}

func evidenceItemSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"source_type", "interpreted_statement", "keywords", "salient_spans", "source_ref", "confidence"},
		"properties": map[string]any{
			"source_type":           stringSchema(),
			"interpreted_statement": stringSchema(),
			"keywords":              stringArraySchema(),
			"salient_spans":         stringArraySchema(),
			"source_ref":            map[string]any{"type": "object"},
			"confidence":            numberSchema(),
		},
	}
}

func healthSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"ok"},
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
		},
	}
}

func stringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func numberSchema() map[string]any {
	return map[string]any{"type": "number"}
}

func stringArraySchema() map[string]any {
	return map[string]any{
		"type":  "array",
		"items": stringSchema(),
	}
}

func trimStringSlice(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
