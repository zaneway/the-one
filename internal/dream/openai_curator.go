package dream

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

	"github.com/zaneway/theone/internal/idgen"
	"github.com/zaneway/theone/internal/logging"
)

type OpenAICuratorConfig struct {
	APIKey          string
	BaseURL         string
	Model           string
	Timeout         time.Duration
	MaxOutputTokens int64
	HTTPClient      *http.Client
	Logger          *slog.Logger
}

type OpenAICurator struct {
	client          openai.Client
	baseURL         string
	model           string
	timeout         time.Duration
	maxOutputTokens int64
	logger          *slog.Logger
}

func NewOpenAICurator(cfg OpenAICuratorConfig) (OpenAICurator, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return OpenAICurator{}, fmt.Errorf("CONFIG_INVALID: openai api key is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return OpenAICurator{}, fmt.Errorf("CONFIG_INVALID: openai model is required")
	}
	if cfg.Timeout <= 0 {
		return OpenAICurator{}, fmt.Errorf("CONFIG_INVALID: openai timeout must be positive")
	}
	if cfg.MaxOutputTokens <= 0 {
		return OpenAICurator{}, fmt.Errorf("CONFIG_INVALID: openai max output tokens must be positive")
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL := strings.TrimSpace(cfg.BaseURL); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	return OpenAICurator{
		client:          openai.NewClient(opts...),
		baseURL:         strings.TrimSpace(cfg.BaseURL),
		model:           model,
		timeout:         cfg.Timeout,
		maxOutputTokens: cfg.MaxOutputTokens,
		logger:          cfg.Logger,
	}, nil
}

func (c OpenAICurator) Curate(ctx context.Context, input CurationInput) (CurationResult, error) {
	memories := boundedMemoryViews(input.Memories, input.Config)
	if len(memories) == 0 {
		return CurationResult{}, nil
	}
	payload := map[string]any{
		"task":      "dream_curate_memories",
		"memories":  memories,
		"relations": relationViews(input.Relations),
		"constraints": map[string]any{
			"min_group_size":            curationMinGroupSize(input.Config.MinGroupSize),
			"require_source_memory_ids": input.Config.RequireSourceMemoryIDs,
			"allowed_route_categories":  []string{RouteKnowledge, RouteThinking, RouteSkills},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return CurationResult{}, fmt.Errorf("PROVIDER_INPUT_INVALID: encode dream curation payload: %w", err)
	}
	requestID, err := idgen.New("dreq")
	if err != nil {
		requestID = fmt.Sprintf("dreq_%d", time.Now().UnixNano())
	}
	startedAt := time.Now()
	commonFields := c.curatorLogFields(requestID, len(memories), string(data))
	c.logInfo(logging.ExternalModelRequestStartMsg,
		append(commonFields,
			"timeout_ms", c.timeout.Milliseconds(),
			"input_body", logging.ExternalModelLogBody(string(data)),
		)...,
	)
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	format := shared.NewResponseFormatJSONObjectParam()
	resp, err := c.client.Chat.Completions.New(reqCtx, openai.ChatCompletionNewParams{
		Model: shared.ChatModel(c.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(dreamCuratorInstructions()),
			openai.UserMessage(string(data)),
		},
		MaxTokens:   openai.Int(c.maxOutputTokens),
		Temperature: openai.Float(0),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &format,
		},
	}, option.WithRequestTimeout(c.timeout))
	if err != nil {
		c.logError(logging.ExternalModelRequestFailedMsg,
			append(commonFields,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"failure_reason", classifyDreamCuratorFailure(err),
				"error", err.Error(),
			)...,
		)
		return CurationResult{}, fmt.Errorf("PROVIDER_UNAVAILABLE: openai dream curation request failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		c.logError(logging.ExternalModelResponseInvalidMsg,
			append(commonFields,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"output_body", logging.ExternalModelLogBody(""),
				"response_body", dreamChatCompletionLogBody(resp, ""),
				"error", "choices is empty",
			)...,
		)
		return CurationResult{}, fmt.Errorf("PROVIDER_INVALID_OUTPUT: openai dream curation response choices is empty")
	}
	raw := strings.TrimSpace(resp.Choices[0].Message.Content)
	if raw == "" {
		c.logError(logging.ExternalModelResponseInvalidMsg,
			append(commonFields,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"output_body", logging.ExternalModelLogBody(""),
				"response_body", dreamChatCompletionLogBody(resp, ""),
				"error", "message content is empty",
			)...,
		)
		return CurationResult{}, fmt.Errorf("PROVIDER_INVALID_OUTPUT: openai dream curation response message content is empty")
	}
	normalized, err := normalizeDreamCuratorJSON(raw)
	if err != nil {
		c.logError(logging.ExternalModelResponseInvalidMsg,
			append(commonFields,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"output_body", logging.ExternalModelLogBody(raw),
				"response_body", dreamChatCompletionLogBody(resp, raw),
				"error", err.Error(),
			)...,
		)
		return CurationResult{}, fmt.Errorf("PROVIDER_INVALID_OUTPUT: normalize dream curation response: %w", err)
	}
	var decoded CurationResult
	if err := json.Unmarshal([]byte(normalized), &decoded); err != nil {
		c.logError(logging.ExternalModelResponseInvalidMsg,
			append(commonFields,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"output_body", logging.ExternalModelLogBody(normalized),
				"response_body", dreamChatCompletionLogBody(resp, raw),
				"error", err.Error(),
			)...,
		)
		return CurationResult{}, fmt.Errorf("PROVIDER_INVALID_OUTPUT: decode dream curation response: %w", err)
	}
	result := sanitizeCurationResult(decoded, input.Memories, input.Config)
	c.logInfo(logging.ExternalModelResponseOKMsg,
		append(commonFields,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"output_body", logging.ExternalModelLogBody(normalized),
			"response_body", dreamChatCompletionLogBody(resp, normalized),
			"output_group_count", len(result.Groups),
		)...,
	)
	return result, nil
}

func (c OpenAICurator) curatorLogFields(requestID string, inputMemoryCount int, inputJSON string) []any {
	fields := []any{
		"request_id", requestID,
		"task", "dream_curate_memories",
		"model", c.model,
		"input_memory_count", inputMemoryCount,
		"input_bytes", len(inputJSON),
	}
	if c.baseURL != "" {
		fields = append(fields, "base_url", c.baseURL)
	}
	return fields
}

func dreamChatCompletionLogBody(resp *openai.ChatCompletion, outputText string) string {
	if resp == nil {
		return logging.ExternalModelLogBody(fmt.Sprintf(`{"content":%q}`, outputText))
	}
	body, err := json.Marshal(map[string]any{
		"response_id": resp.ID,
		"model":       resp.Model,
		"content":     outputText,
		"usage":       resp.Usage,
	})
	if err != nil {
		return logging.ExternalModelLogBody(fmt.Sprintf(`{"marshal_error":%q}`, err.Error()))
	}
	return logging.ExternalModelLogBody(string(body))
}

func classifyDreamCuratorFailure(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "context deadline exceeded") || strings.Contains(message, "timeout") {
		return "client_timeout"
	}
	return "provider_error"
}

func (c OpenAICurator) logInfo(msg string, args ...any) {
	if c.logger == nil {
		return
	}
	c.logger.Info(msg, args...)
}

func (c OpenAICurator) logError(msg string, args ...any) {
	if c.logger == nil {
		return
	}
	c.logger.Error(msg, args...)
}

func dreamCuratorInstructions() string {
	return `You curate persistent memory records into stable Obsidian knowledge projections.
Return only a JSON object:
{
  "groups": [{
    "projection_id": "stable lowercase id",
    "topic_key": "domain key",
    "title": "short note title",
    "summary": "consolidated Markdown-ready summary",
    "source_memory_ids": ["exact input memory ids"],
    "source_map": {"claim": ["exact input memory ids"]},
    "route_category": "knowledge|thinking|skills",
    "route_subject": "directory subject",
    "memory_type_bucket": "directory bucket"
  }]
}
Never invent source_memory_ids. Prefer fewer, coherent groups over many fragmented notes.`
}

func boundedMemoryViews(memories []MemoryRecord, cfg CurationConfig) []map[string]any {
	limit := cfg.MaxInputMemories
	if limit <= 0 || limit > len(memories) {
		limit = len(memories)
	}
	maxChars := cfg.MaxInputChars
	usedChars := 0
	out := make([]map[string]any, 0, limit)
	for _, item := range memories[:limit] {
		content := strings.TrimSpace(item.Content)
		if maxChars > 0 && usedChars+len(content) > maxChars {
			remain := maxChars - usedChars
			if remain <= 0 {
				break
			}
			content = content[:remain]
		}
		usedChars += len(content)
		out = append(out, map[string]any{
			"id":           item.ID,
			"memory_type":  item.MemoryType,
			"title":        item.Title,
			"content":      content,
			"workspace_id": item.WorkspaceID,
			"project_id":   item.ProjectID,
			"repo_id":      item.RepoID,
			"confidence":   item.Confidence,
			"importance":   item.Importance,
			"updated_at":   item.UpdatedAt,
		})
		if maxChars > 0 && usedChars >= maxChars {
			break
		}
	}
	return out
}

func relationViews(relations []RelationRecord) []map[string]any {
	out := make([]map[string]any, 0, len(relations))
	for _, relation := range relations {
		out = append(out, map[string]any{
			"source_id":     relation.SourceID,
			"target_id":     relation.TargetID,
			"relation_type": relation.RelationType,
			"weight":        relation.Weight,
		})
	}
	return out
}

func sanitizeCurationResult(result CurationResult, memories []MemoryRecord, cfg CurationConfig) CurationResult {
	known := map[string]bool{}
	for _, item := range memories {
		known[item.ID] = true
	}
	minGroupSize := curationMinGroupSize(cfg.MinGroupSize)
	out := CurationResult{Groups: make([]CurationGroup, 0, len(result.Groups))}
	for _, group := range result.Groups {
		group.SourceMemoryIDs = validSourceIDs(group.SourceMemoryIDs, known)
		if cfg.RequireSourceMemoryIDs && len(group.SourceMemoryIDs) < minGroupSize {
			continue
		}
		group.ProjectionID = strings.TrimSpace(group.ProjectionID)
		group.TopicKey = strings.TrimSpace(group.TopicKey)
		group.Title = strings.TrimSpace(group.Title)
		group.Summary = strings.TrimSpace(group.Summary)
		group.RouteCategory = sanitizeRouteCategory(group.RouteCategory)
		group.RouteSubject = strings.TrimSpace(group.RouteSubject)
		group.MemoryTypeBucket = strings.TrimSpace(group.MemoryTypeBucket)
		group.SourceMap = sanitizeSourceMap(group.SourceMap, known)
		out.Groups = append(out.Groups, group)
	}
	return out
}

func validSourceIDs(ids []string, known map[string]bool) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || !known[id] || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func sanitizeSourceMap(input map[string][]string, known map[string]bool) map[string][]string {
	if len(input) == 0 {
		return nil
	}
	out := map[string][]string{}
	for key, ids := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if valid := validSourceIDs(ids, known); len(valid) > 0 {
			out[key] = valid
		}
	}
	return out
}

func sanitizeRouteCategory(value string) string {
	switch strings.TrimSpace(value) {
	case RouteKnowledge:
		return RouteKnowledge
	case RouteThinking:
		return RouteThinking
	case RouteSkills:
		return RouteSkills
	default:
		return ""
	}
}

func normalizeDreamCuratorJSON(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("response is empty")
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed, nil
	}
	cleaned := strings.TrimSpace(strings.TrimSuffix(stripDreamJSONFence(trimmed), "```"))
	if json.Valid([]byte(cleaned)) {
		return cleaned, nil
	}
	return "", fmt.Errorf("response is not valid json")
}

func stripDreamJSONFence(input string) string {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	if idx := strings.Index(trimmed, "\n"); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+1:])
	}
	return trimmed
}
