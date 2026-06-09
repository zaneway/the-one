package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/prompts"
)

const OpenAIProviderName = "openai"

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
}

type OpenAIProvider struct {
	client                   openai.Client
	model                    string
	timeout                  time.Duration
	maxOutputTokens          int64
	extractEvidencePrompt    string
	generateCandidatesPrompt string
	semanticEnhancePrompt    string
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
	}, nil
}

func (p OpenAIProvider) Name() string {
	return OpenAIProviderName
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
	reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	resp, err := p.client.Responses.New(reqCtx, responses.ResponseNewParams{
		Model:           shared.ResponsesModel(p.model),
		Instructions:    openai.String(instructions),
		Input:           responses.ResponseNewParamsInputUnion{OfString: openai.String(string(data))},
		MaxOutputTokens: openai.Int(p.maxOutputTokens),
		Temperature:     openai.Float(0),
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigParamOfJSONSchema(schemaName, schema),
		},
	}, option.WithRequestTimeout(p.timeout))
	if err != nil {
		return "", fmt.Errorf("PROVIDER_UNAVAILABLE: openai responses request failed: %w", err)
	}
	out := strings.TrimSpace(resp.OutputText())
	if out == "" {
		return "", fmt.Errorf("PROVIDER_INVALID_OUTPUT: openai response output_text is empty")
	}
	return out, nil
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
