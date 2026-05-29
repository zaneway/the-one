package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/zaneway/theone/internal/capture"
)

type TurnRuntime struct {
	store StateStore
}

func NewTurnRuntime(store StateStore) *TurnRuntime {
	return &TurnRuntime{store: store}
}

type ToolResultInput struct {
	ToolName      string `json:"tool_name"`
	InputSummary  string `json:"input_summary"`
	OutputSummary string `json:"output_summary"`
	ExitCode      int    `json:"exit_code"`
}

type FileEditInput struct {
	FilePath       string `json:"file_path"`
	ContentSummary string `json:"content_summary"`
	Symbol         string `json:"symbol"`
	BeforeHash     string `json:"before_hash"`
	AfterHash      string `json:"after_hash"`
	ChangeType     string `json:"change_type"`
}

type TurnPayload struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	RepoID      string `json:"repo_id"`
	AgentType   string `json:"agent_type"`
	SessionID   string `json:"session_id"`
	TaskID      string `json:"task_id"`

	TurnID        string `json:"turn_id"`
	UserSummary   string `json:"user_summary"`
	AgentSummary  string `json:"agent_summary"`
	StartedAt     string `json:"started_at"`
	CompletedAt   string `json:"completed_at"`
	IsSubstantive bool   `json:"is_substantive"`

	TaskSummary    string `json:"task_summary"`
	TaskStatus     string `json:"task_status"`
	OutcomeSummary string `json:"outcome_summary"`

	UserDeclarationSummary string `json:"user_declaration_summary"`
	UserCorrectionSummary  string `json:"user_correction_summary"`
	DecisionSummary        string `json:"decision_summary"`
	ReasonSummary          string `json:"reason_summary"`

	Keywords     []string          `json:"keywords"`
	SalientSpans []string          `json:"salient_spans"`
	ToolResults  []ToolResultInput `json:"tool_results"`
	FileEdits    []FileEditInput   `json:"file_edits"`

	RetrievalTraceID string   `json:"retrieval_trace_id"`
	UsedMemoryIDs    []string `json:"used_memory_ids"`
	InjectedToPrompt bool     `json:"injected_to_prompt"`

	// SkipBaseTurn 为 true 时仅展开 tool/file/decision 等增量事件，不写 conversation/agent 回合。
	SkipBaseTurn bool `json:"skip_base_turn"`
}

func (r *TurnRuntime) BuildObserveRequests(payload TurnPayload) ([]capture.ObserveRequest, error) {
	if strings.TrimSpace(payload.WorkspaceID) == "" || strings.TrimSpace(payload.ProjectID) == "" || strings.TrimSpace(payload.RepoID) == "" {
		return nil, fmt.Errorf("missing required scope fields: workspace_id/project_id/repo_id")
	}
	if strings.TrimSpace(payload.AgentType) == "" {
		return nil, fmt.Errorf("missing required field: agent_type")
	}
	state, err := r.store.Load()
	if err != nil {
		return nil, err
	}
	sessionID := firstNonEmpty(strings.TrimSpace(payload.SessionID), strings.TrimSpace(state.SessionID))
	taskID := firstNonEmpty(strings.TrimSpace(payload.TaskID), strings.TrimSpace(state.TaskID))
	if sessionID == "" {
		return nil, fmt.Errorf("missing required session_id")
	}

	common := capture.ObserveRequest{
		SessionID:     sessionID,
		TaskID:        taskID,
		AgentType:     payload.AgentType,
		WorkspaceID:   payload.WorkspaceID,
		ProjectID:     payload.ProjectID,
		RepoID:        payload.RepoID,
		SourceChannel: capture.SourceChannelAgentSession,
		OccurredAt:    firstNonEmpty(payload.CompletedAt, payload.StartedAt),
		Keywords:      payload.Keywords,
		SalientSpans:  payload.SalientSpans,
		SourceRefs: []capture.SourceRef{
			{
				"source_type":      "agent_session",
				"capture_method":   capture.CaptureMethodAdapterHook,
				"protocol_version": ProtocolV1,
			},
		},
	}

	requests := make([]capture.ObserveRequest, 0, 8)
	if payload.TaskSummary != "" && normalize(payload.TaskSummary) != normalize(state.LastTaskSummary) {
		taskReq := common
		taskReq.EventType = capture.EventTaskStart
		taskReq.Actor = capture.ActorAdapter
		taskReq.ContentSummary = "任务开始：" + payload.TaskSummary
		taskReq.Task = &capture.TaskInput{
			TaskSummary: payload.TaskSummary,
			Status:      capture.StatusActive,
		}
		requests = append(requests, taskReq)
		state.LastTaskSummary = payload.TaskSummary
	}

	hasHighSignal := len(payload.ToolResults) > 0 || len(payload.FileEdits) > 0 || strings.TrimSpace(payload.DecisionSummary) != ""
	signature := turnSignature(payload.TurnID, payload.UserSummary, payload.AgentSummary)
	emitBaseTurn := !payload.SkipBaseTurn && (payload.IsSubstantive || hasHighSignal)
	if emitBaseTurn && !(payload.TurnID != "" && payload.TurnID == state.LastTurnID && signature == state.LastTurnSig) {
		if strings.TrimSpace(payload.UserCorrectionSummary) != "" {
			userReq := common
			userReq.EventType = capture.EventUserCorrection
			userReq.Actor = capture.ActorUser
			userReq.ContentSummary = payload.UserCorrectionSummary
			requests = append(requests, userReq)
		} else if strings.TrimSpace(payload.UserDeclarationSummary) != "" {
			userReq := common
			userReq.EventType = capture.EventUserDeclaration
			userReq.Actor = capture.ActorUser
			userReq.ContentSummary = payload.UserDeclarationSummary
			requests = append(requests, userReq)
		} else if strings.TrimSpace(payload.UserSummary) != "" {
			userReq := common
			userReq.EventType = capture.EventConversationMessage
			userReq.Actor = capture.ActorUser
			userReq.ContentSummary = payload.UserSummary
			requests = append(requests, userReq)
		}
		if strings.TrimSpace(payload.AgentSummary) != "" {
			agentReq := common
			agentReq.EventType = capture.EventAgentResponseSummary
			agentReq.Actor = capture.ActorAgent
			agentReq.ContentSummary = payload.AgentSummary
			agentReq.SourceRefs = appendRetrievalSourceRefs(agentReq.SourceRefs, payload)
			requests = append(requests, agentReq)
		}
		state.LastTurnID = payload.TurnID
		state.LastTurnSig = signature
	}

	for _, item := range payload.ToolResults {
		if strings.TrimSpace(item.ToolName) == "" {
			continue
		}
		toolReq := common
		toolReq.EventType = capture.EventToolResultSummary
		toolReq.Actor = capture.ActorTool
		toolReq.ToolName = item.ToolName
		toolReq.InputSummary = item.InputSummary
		toolReq.OutputSummary = item.OutputSummary
		toolReq.ContentSummary = "工具结果：" + item.ToolName
		toolReq.SourceRefs = append(toolReq.SourceRefs, capture.SourceRef{
			"source_type": "tool_output",
			"exit_code":   item.ExitCode,
		})
		requests = append(requests, toolReq)
	}

	for _, item := range payload.FileEdits {
		if strings.TrimSpace(item.FilePath) == "" {
			continue
		}
		editReq := common
		editReq.EventType = capture.EventFileEditSummary
		editReq.Actor = capture.ActorAgent
		editReq.ContentSummary = firstNonEmpty(item.ContentSummary, "文件修改："+item.FilePath)
		editReq.SourceRefs = append(editReq.SourceRefs, capture.SourceRef{
			"source_type": "file_edit_summary",
			"file_path":   item.FilePath,
			"symbol":      item.Symbol,
			"before_hash": item.BeforeHash,
			"after_hash":  item.AfterHash,
			"change_type": item.ChangeType,
		})
		requests = append(requests, editReq)
	}

	if strings.TrimSpace(payload.DecisionSummary) != "" {
		decisionReq := common
		decisionReq.EventType = capture.EventAgentDecision
		decisionReq.Actor = capture.ActorAgent
		decisionReq.ContentSummary = payload.DecisionSummary
		decisionReq.SourceRefs = append(decisionReq.SourceRefs, capture.SourceRef{
			"source_type":      "agent_decision",
			"decision_summary": payload.DecisionSummary,
			"reason_summary":   payload.ReasonSummary,
		})
		requests = append(requests, decisionReq)
	}

	if isTerminalStatus(payload.TaskStatus) && strings.TrimSpace(payload.OutcomeSummary) != "" {
		resultReq := common
		resultReq.EventType = capture.EventTaskResult
		resultReq.Actor = capture.ActorAdapter
		resultReq.ContentSummary = payload.OutcomeSummary
		resultReq.Task = &capture.TaskInput{
			TaskSummary:    firstNonEmpty(payload.TaskSummary, state.LastTaskSummary),
			Status:         payload.TaskStatus,
			OutcomeSummary: payload.OutcomeSummary,
		}
		requests = append(requests, resultReq)
	}

	state.SessionID = sessionID
	state.TaskID = taskID
	if err := r.store.Save(state); err != nil {
		return nil, err
	}
	return requests, nil
}

func isTerminalStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case capture.StatusSucceeded, capture.StatusFailed, capture.StatusInterrupted, capture.StatusCompleted:
		return true
	default:
		return false
	}
}

func turnSignature(turnID, userSummary, agentSummary string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{turnID, userSummary, agentSummary}, "|")))
	return hex.EncodeToString(sum[:])
}

func normalize(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func appendRetrievalSourceRefs(refs []capture.SourceRef, payload TurnPayload) []capture.SourceRef {
	if strings.TrimSpace(payload.RetrievalTraceID) == "" && len(payload.UsedMemoryIDs) == 0 && !payload.InjectedToPrompt {
		return refs
	}
	ref := capture.SourceRef{
		"source_type":    "memory_context",
		"capture_method": capture.CaptureMethodAdapterHook,
	}
	if traceID := strings.TrimSpace(payload.RetrievalTraceID); traceID != "" {
		ref["retrieval_trace_id"] = traceID
	}
	if payload.InjectedToPrompt {
		ref["injected_to_prompt"] = true
	}
	if len(payload.UsedMemoryIDs) > 0 {
		ref["used_memory_ids"] = payload.UsedMemoryIDs
	}
	return append(refs, ref)
}
