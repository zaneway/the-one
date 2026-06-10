package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/prompts"
)

const (
	OpenAIProviderName = "openai"
	// providerLogBodyMaxChars 限制单条日志字段长度，避免 prompt/事件正文撑爆日志文件。
	providerLogBodyMaxChars = 32000
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
	model                    string
	timeout                  time.Duration
	maxOutputTokens          int64
	extractEvidencePrompt    string
	generateCandidatesPrompt string
	semanticEnhancePrompt    string
	logger                   *slog.Logger
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
		model:                    model,
		timeout:                  cfg.Timeout,
		maxOutputTokens:          cfg.MaxOutputTokens,
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
	}, "theone_provider_health", healthSchema())
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
	}, "theone_semantic_enhance", semanticEnhanceSchema())
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
		"task":            "extract_evidence",
		"raw_event":       input.RawEvent,
		"session":         input.Session,
		"agent_task":      input.Task,
		"capture_quality": input.CaptureQuality,
		"related_events":  input.RelatedEvents,
		"now":             input.Now,
	}
	raw, err := p.callStructured(ctx, p.extractEvidencePrompt, payload, "theone_evidence", evidenceSchema())
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

func (p OpenAIProvider) GenerateCandidates(ctx context.Context, input CandidateInput) ([]MemoryCandidate, error) {
	payload := map[string]any{
		"task":           "generate_memory_candidates",
		"evidence":       input.Evidence,
		"raw_event":      input.RawEvent,
		"session":        input.Session,
		"agent_task":     input.Task,
		"related_memory": input.RelatedMemory,
		"now":            input.Now,
	}
	raw, err := p.callStructured(ctx, p.generateCandidatesPrompt, payload, "theone_candidates", candidateSchema())
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Candidates []openAIMemoryCandidate `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("PROVIDER_INVALID_OUTPUT: decode openai candidate response: %w", err)
	}
	out := make([]MemoryCandidate, 0, len(decoded.Candidates))
	for _, candidate := range decoded.Candidates {
		mapped := candidate.toMemoryCandidate()
		applyCandidateLineage(&mapped, input)
		if mapped.MemoryType == "" || mapped.Scope == "" || mapped.Content == "" {
			continue
		}
		out = append(out, mapped)
	}
	return out, nil
}

type openAIEvidenceDraft struct {
	SourceType           string         `json:"source_type"`
	InterpretedStatement string         `json:"interpreted_statement"`
	Keywords             []string       `json:"keywords"`
	SalientSpans         []string       `json:"salient_spans"`
	SourceRef            map[string]any `json:"source_ref"`
	Confidence           float64        `json:"confidence"`
}

type openAIMemoryCandidate struct {
	MemoryType        string   `json:"memory_type"`
	Scope             string   `json:"scope"`
	WorkspaceID       string   `json:"workspace_id"`
	UserID            string   `json:"user_id"`
	ProjectID         string   `json:"project_id"`
	RepoID            string   `json:"repo_id"`
	SessionID         string   `json:"session_id"`
	TaskID            string   `json:"task_id"`
	SourceType        string   `json:"source_type"`
	Title             string   `json:"title"`
	Content           string   `json:"content"`
	Keywords          []string `json:"keywords"`
	Entities          []string `json:"entities"`
	RetrievalCues     []string `json:"retrieval_cues"`
	Tags              []string `json:"tags"`
	Confidence        float64  `json:"confidence"`
	Importance        float64  `json:"importance"`
	EncodingDepth     int      `json:"encoding_depth"`
	CandidateReason   []string `json:"candidate_reason"`
	SourceEvidenceIDs []string `json:"source_evidence_ids"`
}

func (c openAIMemoryCandidate) toMemoryCandidate() MemoryCandidate {
	return MemoryCandidate{
		MemoryType:        c.MemoryType,
		Scope:             c.Scope,
		WorkspaceID:       c.WorkspaceID,
		UserID:            c.UserID,
		ProjectID:         c.ProjectID,
		RepoID:            c.RepoID,
		SessionID:         c.SessionID,
		TaskID:            c.TaskID,
		SourceType:        c.SourceType,
		Title:             c.Title,
		Content:           c.Content,
		Keywords:          c.Keywords,
		Entities:          c.Entities,
		RetrievalCues:     c.RetrievalCues,
		Tags:              c.Tags,
		Confidence:        c.Confidence,
		Importance:        c.Importance,
		EncodingDepth:     c.EncodingDepth,
		CandidateReason:   c.CandidateReason,
		SourceEvidenceIDs: c.SourceEvidenceIDs,
	}
}

func (p OpenAIProvider) callStructured(ctx context.Context, instructions string, payload any, schemaName string, schema map[string]any) (string, error) {
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
	startedAt := time.Now()
	p.logInfo("openai provider request",
		"schema_name", schemaName,
		"model", p.model,
		"request_body", providerLogBody(string(requestBody)),
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
		p.logError("openai provider request failed",
			"schema_name", schemaName,
			"model", p.model,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"error", err.Error(),
		)
		return "", fmt.Errorf("PROVIDER_UNAVAILABLE: openai chat completions request failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		p.logError("openai provider response invalid",
			"schema_name", schemaName,
			"model", p.model,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"response_body", providerChatCompletionLogBody(resp, ""),
			"error", "choices is empty",
		)
		return "", fmt.Errorf("PROVIDER_INVALID_OUTPUT: openai response choices is empty")
	}
	out := strings.TrimSpace(resp.Choices[0].Message.Content)
	if out == "" {
		p.logError("openai provider response invalid",
			"schema_name", schemaName,
			"model", p.model,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"response_body", providerChatCompletionLogBody(resp, ""),
			"error", "message content is empty",
		)
		return "", fmt.Errorf("PROVIDER_INVALID_OUTPUT: openai response message content is empty")
	}
	p.logInfo("openai provider response",
		"schema_name", schemaName,
		"model", p.model,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"response_body", providerChatCompletionLogBody(resp, out),
	)
	return out, nil
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

func applyCandidateLineage(candidate *MemoryCandidate, input CandidateInput) {
	candidate.MemoryType = strings.TrimSpace(candidate.MemoryType)
	candidate.Scope = strings.TrimSpace(candidate.Scope)
	candidate.Title = strings.TrimSpace(candidate.Title)
	candidate.Content = strings.TrimSpace(candidate.Content)
	candidate.WorkspaceID = firstNonEmpty(candidate.WorkspaceID, input.RawEvent.WorkspaceID, input.Session.WorkspaceID)
	candidate.ProjectID = firstNonEmpty(candidate.ProjectID, input.RawEvent.ProjectID, input.Session.ProjectID)
	candidate.RepoID = firstNonEmpty(candidate.RepoID, input.RawEvent.RepoID, input.Session.RepoID)
	candidate.SessionID = firstNonEmpty(candidate.SessionID, input.RawEvent.SessionID, input.Session.ID)
	candidate.TaskID = firstNonEmpty(candidate.TaskID, input.RawEvent.TaskID, input.Task.ID)
	candidate.SourceType = firstNonEmpty(candidate.SourceType, input.Evidence.SourceType)
	candidate.Confidence = clamp01(candidate.Confidence)
	candidate.Importance = clamp01(candidate.Importance)
	if candidate.EncodingDepth < 0 {
		candidate.EncodingDepth = 0
	}
	if candidate.EncodingDepth > 4 {
		candidate.EncodingDepth = 4
	}
	if len(candidate.SourceEvidenceIDs) == 0 && input.Evidence.ID != "" {
		candidate.SourceEvidenceIDs = []string{input.Evidence.ID}
	}
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

func evidenceSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"evidence"},
		"properties": map[string]any{
			"evidence": map[string]any{
				"type": "array",
				"items": map[string]any{
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
				},
			},
		},
	}
}

func candidateSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"candidates"},
		"properties": map[string]any{
			"candidates": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":     "object",
					"required": []string{"memory_type", "scope", "title", "content", "keywords", "entities", "retrieval_cues", "tags", "confidence", "importance", "encoding_depth", "candidate_reason"},
					"properties": map[string]any{
						"memory_type":         stringSchema(),
						"scope":               stringSchema(),
						"title":               stringSchema(),
						"content":             stringSchema(),
						"keywords":            stringArraySchema(),
						"entities":            stringArraySchema(),
						"retrieval_cues":      stringArraySchema(),
						"tags":                stringArraySchema(),
						"confidence":          numberSchema(),
						"importance":          numberSchema(),
						"encoding_depth":      map[string]any{"type": "integer"},
						"candidate_reason":    stringArraySchema(),
						"source_evidence_ids": stringArraySchema(),
					},
				},
			},
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
