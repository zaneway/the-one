package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zaneway/theone/internal/capture"
)

// BindingState 持久化在 binding.{agent_type}.json。
type BindingState struct {
	AgentType             string `json:"agent_type"`
	SessionID             string `json:"session_id"`
	TaskID                string `json:"task_id"`
	ExternalSessionKey    string `json:"external_session_key"`
	TaskFromPromptPending bool   `json:"task_from_prompt_pending"`
	WorkspaceID           string `json:"workspace_id,omitempty"`
	ProjectID             string `json:"project_id,omitempty"`
	RepoID                string `json:"repo_id,omitempty"`
	BoundAt               string `json:"bound_at,omitempty"`
}

// SessionBinder 维护会话/任务绑定（P1：Reset、升级、mismatch 审计）。
type SessionBinder struct {
	dirPath string
}

// ResolveResult ingest / prefetch 共用的绑定解析结果。
type ResolveResult struct {
	SessionID  string
	TaskID     string
	ResetDedup bool
}

func NewSessionBinder(dirPath string) *SessionBinder {
	return &SessionBinder{dirPath: dirPath}
}

func (b *SessionBinder) bindingPath(agentType string) string {
	safe := strings.TrimSpace(agentType)
	if safe == "" {
		safe = "unknown"
	}
	return filepath.Join(b.dirPath, "binding."+safe+".json")
}

func (b *SessionBinder) Load(agentType string) (BindingState, bool, error) {
	path := b.bindingPath(agentType)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return BindingState{}, false, nil
		}
		return BindingState{}, false, fmt.Errorf("load binding: %w", err)
	}
	var state BindingState
	if err := json.Unmarshal(data, &state); err != nil {
		return BindingState{}, false, fmt.Errorf("decode binding: %w", err)
	}
	return state, true, nil
}

func (b *SessionBinder) Save(state BindingState) error {
	if err := os.MkdirAll(b.dirPath, 0o755); err != nil {
		return fmt.Errorf("create binding dir: %w", err)
	}
	if state.BoundAt == "" {
		state.BoundAt = time.Now().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode binding: %w", err)
	}
	if err := os.WriteFile(b.bindingPath(state.AgentType), data, 0o644); err != nil {
		return fmt.Errorf("write binding: %w", err)
	}
	return nil
}

// DeleteBinding 会话结束时删除 binding 文件（§6.4）。
func (b *SessionBinder) DeleteBinding(agentType string) error {
	path := b.bindingPath(agentType)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete binding: %w", err)
	}
	return nil
}

// ResolveInput 从包络解析绑定所需字段。
type ResolveInput struct {
	AgentType string
	Producer  string
	EventType string
	Envelope  IngestEnvelope
}

// Resolve 返回应注入 observe 的 session_id / task_id，并更新 binding 文件。
func (b *SessionBinder) Resolve(in ResolveInput) (ResolveResult, error) {
	agentType := strings.TrimSpace(in.AgentType)
	if agentType == "" {
		agentType = stringFromPayload(in.Envelope.Payload, "agent_type")
	}
	if agentType == "" {
		return ResolveResult{}, fmt.Errorf("%w: missing agent_type", errInvalidSession)
	}

	externalKey := pickExternalSessionKey(in.Envelope)
	isSessionStart := strings.TrimSpace(in.EventType) == "session.start"

	if externalKey == "" && strings.TrimSpace(in.Envelope.SessionID) != "" {
		externalKey = strings.TrimSpace(in.Envelope.SessionID)
	}
	if externalKey == "" {
		if isSessionStart {
			return ResolveResult{}, fmt.Errorf("%w: session.start requires conversation_id or session_id", errInvalidSession)
		}
		if allowSyntheticSession() {
			externalKey = fmt.Sprintf("sess_%s_%d", agentType, time.Now().UnixNano())
		} else {
			return ResolveResult{}, fmt.Errorf("%w: missing conversation_id/session_id", errInvalidSession)
		}
	}

	explicitTask := pickTaskID(in.Envelope)
	state, exists, err := b.Load(agentType)
	if err != nil {
		return ResolveResult{}, err
	}

	if isSessionStart {
		return b.resolveSessionStart(agentType, externalKey, explicitTask, in, state, exists)
	}

	if !exists {
		state = b.newBindingState(agentType, externalKey, explicitTask, in.Envelope)
		if err := b.Save(state); err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{SessionID: state.SessionID, TaskID: state.TaskID}, nil
	}

	if externalKey != "" && externalKey != state.ExternalSessionKey {
		_ = appendBindingMismatch(b.dirPath, externalKey, state.ExternalSessionKey, "mismatch")
	}

	if explicitTask != "" {
		state.TaskID = explicitTask
		state.TaskFromPromptPending = false
		_ = b.Save(state)
	}

	return ResolveResult{SessionID: state.SessionID, TaskID: state.TaskID}, nil
}

func (b *SessionBinder) resolveSessionStart(agentType, externalKey, explicitTask string, in ResolveInput, state BindingState, exists bool) (ResolveResult, error) {
	if !exists {
		state = b.newBindingState(agentType, externalKey, explicitTask, in.Envelope)
		if err := b.Save(state); err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{SessionID: state.SessionID, TaskID: state.TaskID}, nil
	}

	if externalKey == state.ExternalSessionKey {
		if explicitTask != "" {
			state.TaskID = explicitTask
			state.TaskFromPromptPending = false
			_ = b.Save(state)
		}
		return ResolveResult{SessionID: state.SessionID, TaskID: state.TaskID}, nil
	}

	if isSyntheticSessionID(agentType, state.SessionID) && !strictNoSessionUpgrade() {
		oldID := state.SessionID
		state.SessionID = externalKey
		state.ExternalSessionKey = externalKey
		if explicitTask != "" {
			state.TaskID = explicitTask
			state.TaskFromPromptPending = false
		}
		_ = appendBindingMismatch(b.dirPath, externalKey, oldID, "session_upgraded")
		if err := b.Save(state); err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{SessionID: state.SessionID, TaskID: state.TaskID}, nil
	}

	state = b.newBindingState(agentType, externalKey, explicitTask, in.Envelope)
	if err := b.Save(state); err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{SessionID: state.SessionID, TaskID: state.TaskID, ResetDedup: true}, nil
}

func (b *SessionBinder) newBindingState(agentType, externalKey, explicitTask string, env IngestEnvelope) BindingState {
	return BindingState{
		AgentType:             agentType,
		SessionID:             externalKey,
		ExternalSessionKey:    externalKey,
		TaskID:                defaultTaskID(agentType, explicitTask),
		TaskFromPromptPending: explicitTask == "",
		WorkspaceID:           stringFromPayload(env.Payload, "workspace_id"),
		ProjectID:             stringFromPayload(env.Payload, "project_id"),
		RepoID:                stringFromPayload(env.Payload, "repo_id"),
	}
}

// BindTaskFromPrompt 首条非空用户 prompt 将 task_*_auto 升级为稳定 task_id（§6.2.2，仅 prefetch 调用）。
func (b *SessionBinder) BindTaskFromPrompt(in BindTaskFromPromptInput) (bound bool, taskID string, err error) {
	agentType := strings.TrimSpace(in.AgentType)
	if agentType == "" {
		return false, "", fmt.Errorf("%w: missing agent_type", errInvalidSession)
	}
	normalized := NormalizePrompt(in.NormalizedPrompt)
	if normalized == "" {
		return false, "", nil
	}
	state, ok, err := b.Load(agentType)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "", nil
	}
	if !state.TaskFromPromptPending {
		return false, state.TaskID, nil
	}
	if in.ExternalSessionKey != "" && state.ExternalSessionKey != "" && in.ExternalSessionKey != state.ExternalSessionKey {
		return false, state.TaskID, nil
	}
	newTaskID := TaskIDFromPrompt(normalized)
	if newTaskID == "" {
		return false, state.TaskID, nil
	}
	summary := strings.TrimSpace(in.TaskSummary)
	if summary == "" {
		summary = truncate(normalized, 200)
	}
	state.TaskID = newTaskID
	state.TaskFromPromptPending = false
	if err := b.Save(state); err != nil {
		return false, "", err
	}
	if in.Observe != nil {
		req := capture.ObserveRequest{
			SessionID:      state.SessionID,
			TaskID:         newTaskID,
			AgentType:      agentType,
			WorkspaceID:    firstNonEmpty(state.WorkspaceID, "local_default_workspace"),
			ProjectID:      firstNonEmpty(state.ProjectID, "the-one"),
			RepoID:         firstNonEmpty(state.RepoID, "the-one"),
			EventType:      capture.EventTaskStart,
			SourceChannel:  capture.SourceChannelAgentSession,
			Actor:          capture.ActorAdapter,
			ContentSummary: capture.EnsureStructuredContentSummary(capture.EventTaskStart, "任务开始："+summary),
			Task: &capture.TaskInput{
				TaskSummary: summary,
				Status:      capture.StatusActive,
			},
		}
		if _, err := in.Observe(context.Background(), req); err != nil {
			return true, newTaskID, fmt.Errorf("sync task to capture: %w", err)
		}
	}
	return true, newTaskID, nil
}

// MarkBootstrapTask 在 auto_bootstrap 后设置占位 task_id。
func (b *SessionBinder) MarkBootstrapTask(agentType string) error {
	state, ok, err := b.Load(agentType)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if strings.TrimSpace(state.TaskID) == "" {
		state.TaskID = "task_" + agentType + "_auto"
	}
	state.TaskFromPromptPending = true
	return b.Save(state)
}

func isSyntheticSessionID(agentType, sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	agentType = strings.TrimSpace(agentType)
	if sessionID == "" {
		return false
	}
	if strings.HasPrefix(sessionID, "sess_cursor_auto_") {
		return true
	}
	prefix := "sess_" + agentType + "_"
	if !strings.HasPrefix(sessionID, prefix) {
		return false
	}
	rest := strings.TrimPrefix(sessionID, prefix)
	if rest == "auto" || strings.HasPrefix(rest, "auto_") {
		return true
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(rest) > 0
}

func strictNoSessionUpgrade() bool {
	return os.Getenv("THEONE_BINDING_STRICT_NO_UPGRADE") == "1"
}

func pickExternalSessionKey(env IngestEnvelope) string {
	for _, key := range []string{"conversation_id", "conversationId", "session_id", "sessionId"} {
		if v := stringFromPayload(env.Payload, key); v != "" {
			return v
		}
	}
	return strings.TrimSpace(env.SessionID)
}

func pickTaskID(env IngestEnvelope) string {
	if v := stringFromPayload(env.Payload, "task_id"); v != "" {
		return v
	}
	return stringFromPayload(env.Payload, "taskId")
}

func defaultTaskID(agentType, explicit string) string {
	if explicit != "" {
		return explicit
	}
	return "task_" + agentType + "_auto"
}

func allowSyntheticSession() bool {
	return os.Getenv("THEONE_ALLOW_SYNTHETIC_SESSION") == "1"
}

func appendBindingMismatch(dir, newKey, oldKey, action string) error {
	path := filepath.Join(dir, "binding_mismatch.log")
	line := fmt.Sprintf("%s action=%s new=%s old=%s\n", time.Now().Format(time.RFC3339Nano), action, newKey, oldKey)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}
