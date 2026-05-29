package adapter

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/zaneway/theone/internal/memory"
)

// PrefetchRequest beforeSubmit / prefetch-context 入参（与 memory.context 对齐 + Hook 字段）。
type PrefetchRequest struct {
	Task                   string `json:"task"`
	WorkspaceID            string `json:"workspace_id"`
	ProjectID              string `json:"project_id"`
	RepoID                 string `json:"repo_id"`
	SessionID              string `json:"session_id"`
	ConversationID         string `json:"conversation_id"`
	GenerationID           string `json:"generation_id"`
	AgentType              string `json:"agent_type"`
	TokenBudget            int    `json:"token_budget"`
	IncludeCodeRefs        bool   `json:"include_code_refs"`
	IncludeEvidenceSummary bool   `json:"include_evidence_summary"`
	MaxInjectChars         int    `json:"max_inject_chars"`
	RuleFile               string `json:"rule_file,omitempty"`
}

// PrefetchResult prefetch-context stdout / 缓存结构。
type PrefetchResult struct {
	OK                bool               `json:"ok"`
	ContextPack       memory.ContextPack `json:"context_pack,omitempty"`
	InjectMarkdown    string             `json:"inject_markdown"`
	RetrievalTraceID  string             `json:"retrieval_trace_id,omitempty"`
	UsedMemoryIDs     []string           `json:"used_memory_ids,omitempty"`
	SessionID         string             `json:"session_id,omitempty"`
	TaskID            string             `json:"task_id,omitempty"`
	GenerationID      string             `json:"generation_id,omitempty"`
	PromptFingerprint string             `json:"prompt_fingerprint,omitempty"`
	TurnID            string             `json:"turn_id,omitempty"`
	TaskBound         bool               `json:"task_bound"`
	Degraded          bool               `json:"degraded"`
	ErrorSummary      string             `json:"error_summary,omitempty"`
	LatencyMS         int64              `json:"latency_ms,omitempty"`
}

// ContextFunc 调用 memory.context。
type ContextFunc func(ctx context.Context, req memory.ContextRequest) (memory.ContextResponse, error)

// PrefetchProcessor P3 读路径：binding + BindTaskFromPrompt + context + Surface 元数据。
type PrefetchProcessor struct {
	Binder    *SessionBinder
	Context   ContextFunc
	Observe   ObserveFunc
	Timeout   time.Duration
	StateDir  string
	RepoRoot  string
	MaxInject int
}

// Run 执行 prefetch-context 主流程。
func (p *PrefetchProcessor) Run(ctx context.Context, req PrefetchRequest) PrefetchResult {
	result := PrefetchResult{OK: false}
	agentType := strings.TrimSpace(req.AgentType)
	if agentType == "" {
		agentType = "cursor"
	}
	conversationID := firstNonEmpty(strings.TrimSpace(req.ConversationID), strings.TrimSpace(req.SessionID))
	if conversationID == "" {
		result.ErrorSummary = "missing conversation_id/session_id"
		result.Degraded = true
		return result
	}

	normalizedTask := NormalizePrompt(req.Task)
	result.PromptFingerprint = PromptFingerprint(req.Task)
	if gen := strings.TrimSpace(req.GenerationID); gen != "" {
		result.GenerationID = gen
		result.TurnID = "turn_" + gen
	} else if result.PromptFingerprint != "" {
		result.TurnID = "turn_" + result.PromptFingerprint
	}

	resolved, err := p.Binder.Resolve(ResolveInput{
		AgentType: agentType,
		Producer:  HookProducer(agentType, "beforeSubmitPrompt"),
		EventType: "conversation.message",
		Envelope: IngestEnvelope{
			SessionID: conversationID,
			Payload: map[string]any{
				"agent_type":      agentType,
				"conversation_id": conversationID,
				"workspace_id":    scopeOrDefault(req.WorkspaceID, "local_default_workspace"),
				"project_id":      scopeOrDefault(req.ProjectID, "the-one"),
				"repo_id":         scopeOrDefault(req.RepoID, "the-one"),
			},
		},
	})
	if err != nil {
		result.ErrorSummary = err.Error()
		result.Degraded = true
		return result
	}
	sessionID, taskID := resolved.SessionID, resolved.TaskID
	result.SessionID = sessionID
	result.TaskID = taskID

	if normalizedTask != "" {
		bound, newTaskID, bindErr := p.Binder.BindTaskFromPrompt(BindTaskFromPromptInput{
			AgentType:          agentType,
			ExternalSessionKey: conversationID,
			NormalizedPrompt:   normalizedTask,
			TaskSummary:        truncate(normalizedTask, 200),
			Observe:            p.Observe,
		})
		if bindErr != nil {
			result.ErrorSummary = bindErr.Error()
			result.Degraded = true
			return result
		}
		if bound {
			result.TaskBound = true
			result.TaskID = newTaskID
			taskID = newTaskID
		}
	}

	if p.Context == nil {
		result.ErrorSummary = "context handler not configured"
		result.Degraded = true
		return result
	}

	ctxReq := memory.ContextRequest{
		Task:                   truncate(normalizedTask, 1000),
		WorkspaceID:            scopeOrDefault(req.WorkspaceID, "local_default_workspace"),
		ProjectID:              scopeOrDefault(req.ProjectID, "the-one"),
		RepoID:                 scopeOrDefault(req.RepoID, "the-one"),
		SessionID:              sessionID,
		AgentType:              agentType,
		TokenBudget:            req.TokenBudget,
		IncludeCodeRefs:        req.IncludeCodeRefs,
		IncludeEvidenceSummary: req.IncludeEvidenceSummary,
	}
	if ctxReq.Task == "" {
		ctxReq.Task = "用户输入摘要未直接可见"
	}
	if ctxReq.TokenBudget <= 0 {
		ctxReq.TokenBudget = 1200
	}

	runCtx := ctx
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}

	started := time.Now()
	ctxResp, err := p.Context(runCtx, ctxReq)
	result.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		result.ErrorSummary = err.Error()
		result.Degraded = true
		_ = p.writePrefetchCache(result, agentType)
		return result
	}

	result.OK = true
	result.ContextPack = ctxResp.ContextPack
	result.RetrievalTraceID = ctxResp.RetrievalTraceID
	result.UsedMemoryIDs = ctxResp.UsedMemoryIDs
	maxChars := p.MaxInject
	if maxChars <= 0 {
		maxChars = req.MaxInjectChars
	}
	if maxChars <= 0 {
		maxChars = 4000
	}
	result.InjectMarkdown = FormatInjectMarkdown(ctxResp, maxChars)
	surface := DriverSurface{
		AgentType: agentType,
		StateDir:  p.StateDir,
		RepoRoot:  strings.TrimSpace(p.RepoRoot),
	}
	_ = p.writePrefetchCache(result, agentType)
	_ = p.writeInjectCache(result, agentType)
	rulePath := strings.TrimSpace(req.RuleFile)
	if rulePath == "" && surface.RepoRoot != "" {
		rulePath = surface.SurfacePath()
	}
	if rulePath != "" {
		alwaysApply := strings.TrimSpace(result.InjectMarkdown) != ""
		_ = DriverSurface{
			AgentType: agentType,
			RepoRoot:  surface.RepoRoot,
		}.WriteSurface(result.InjectMarkdown, alwaysApply)
	}
	_ = p.writeContextCache(ctxResp, result, agentType)
	return result
}

func (p *PrefetchProcessor) writeContextCache(ctxResp memory.ContextResponse, result PrefetchResult, agentType string) error {
	if p.StateDir == "" {
		return nil
	}
	path := DriverSurface{AgentType: agentType, StateDir: p.StateDir}.ContextCachePath()
	payload := map[string]any{
		"context_pack":       ctxResp.ContextPack,
		"used_memory_ids":    ctxResp.UsedMemoryIDs,
		"retrieval_trace_id": ctxResp.RetrievalTraceID,
		"latency_ms":         ctxResp.LatencyMS,
		"session_id":         result.SessionID,
		"task_id":            result.TaskID,
		"ok":                 result.OK,
	}
	if ctxResp.Diagnostics != nil {
		payload["diagnostics"] = ctxResp.Diagnostics
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (p *PrefetchProcessor) writePrefetchCache(result PrefetchResult, agentType string) error {
	if p.StateDir == "" {
		return nil
	}
	path := DriverSurface{AgentType: agentType, StateDir: p.StateDir}.PrefetchCachePath()
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (p *PrefetchProcessor) writeInjectCache(result PrefetchResult, agentType string) error {
	if p.StateDir == "" {
		return nil
	}
	path := DriverSurface{AgentType: agentType, StateDir: p.StateDir}.InjectCachePath()
	payload := map[string]any{
		"generation_id":      result.GenerationID,
		"prompt_fingerprint": result.PromptFingerprint,
		"turn_id":            result.TurnID,
		"session_id":         result.SessionID,
		"task_id":            result.TaskID,
		"retrieval_trace_id": result.RetrievalTraceID,
		"used_memory_ids":    result.UsedMemoryIDs,
		"injected_to_prompt": strings.TrimSpace(result.InjectMarkdown) != "",
		"memory_count":       len(result.ContextPack.Memories),
		"latency_ms":         result.LatencyMS,
		"captured_at":        time.Now().Format(time.RFC3339Nano),
	}
	if !result.OK {
		payload["ok"] = false
		payload["error_summary"] = result.ErrorSummary
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func scopeOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

// BindTaskFromPromptInput BindTaskFromPrompt 入参。
type BindTaskFromPromptInput struct {
	AgentType          string
	ExternalSessionKey string
	NormalizedPrompt   string
	TaskSummary        string
	Observe            ObserveFunc
}
