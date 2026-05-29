package adapter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zaneway/theone/internal/capture"
)

func observeFromLifecycle(env IngestEnvelope, sessionID, taskID string) (capture.ObserveRequest, error) {
	req := capture.ObserveRequest{
		SessionID:      sessionID,
		TaskID:         taskID,
		AgentType:      agentTypeFromEnvelope(env),
		WorkspaceID:    scopeField(env.Payload, "workspace_id", "local_default_workspace"),
		ProjectID:      scopeField(env.Payload, "project_id", "the-one"),
		RepoID:         scopeField(env.Payload, "repo_id", "the-one"),
		EventType:      strings.TrimSpace(env.EventType),
		SourceChannel:  capture.SourceChannelAgentSession,
		Actor:          actorOrDefault(env.Payload, capture.ActorAdapter),
		OccurredAt:     env.OccurredAt,
		ContentSummary: stringFromPayload(env.Payload, "content_summary"),
		SourceRefs:     defaultSourceRefs(env.Producer),
	}
	if req.EventType == capture.EventSessionStart {
		req.CaptureCapabilities = defaultCaptureCapabilities()
	}
	if s := decodeSessionInput(env.Payload); s != nil {
		req.Session = s
	}
	if t := decodeTaskInput(env.Payload); t != nil {
		req.Task = t
	}
	if req.ContentSummary == "" {
		req.ContentSummary = "session lifecycle: " + req.EventType
	}
	return req, nil
}

func observeFromAtomic(env IngestEnvelope, sessionID, taskID string) (capture.ObserveRequest, error) {
	eventType := strings.TrimSpace(env.EventType)
	req := capture.ObserveRequest{
		SessionID:      sessionID,
		TaskID:         taskID,
		AgentType:      agentTypeFromEnvelope(env),
		WorkspaceID:    scopeField(env.Payload, "workspace_id", "local_default_workspace"),
		ProjectID:      scopeField(env.Payload, "project_id", "the-one"),
		RepoID:         scopeField(env.Payload, "repo_id", "the-one"),
		EventType:      eventType,
		SourceChannel:  capture.SourceChannelAgentSession,
		Actor:          actorOrDefault(env.Payload, capture.ActorTool),
		OccurredAt:     env.OccurredAt,
		ContentSummary: stringFromPayload(env.Payload, "content_summary"),
		ToolName:       stringFromPayload(env.Payload, "tool_name"),
		InputSummary:   stringFromPayload(env.Payload, "input_summary"),
		OutputSummary:  stringFromPayload(env.Payload, "output_summary"),
		Keywords:       stringSliceFromPayload(env.Payload, "keywords"),
		SalientSpans:   stringSliceFromPayload(env.Payload, "salient_spans"),
		SourceRefs:     defaultSourceRefs(env.Producer),
	}
	switch eventType {
	case capture.EventToolResultSummary:
		req.Actor = capture.ActorTool
		if req.ContentSummary == "" && req.ToolName != "" {
			req.ContentSummary = "工具结果：" + req.ToolName
		}
	case capture.EventFileEditSummary:
		req.Actor = capture.ActorAgent
		if req.ContentSummary == "" {
			fp := stringFromPayload(env.Payload, "file_path")
			if fp != "" {
				req.ContentSummary = "文件修改：" + fp
			}
		}
		if fp := stringFromPayload(env.Payload, "file_path"); fp != "" {
			req.SourceRefs = append(req.SourceRefs, capture.SourceRef{
				"source_type":    "file_edit_summary",
				"file_path":      fp,
				"change_type":    stringFromPayload(env.Payload, "change_type"),
				"capture_method": capture.CaptureMethodAdapterHook,
			})
		}
	}
	if req.ContentSummary == "" {
		req.ContentSummary = eventType
	}
	return req, nil
}

func bootstrapObserveRequest(env IngestEnvelope, sessionID, taskID, producer string) capture.ObserveRequest {
	prod := strings.TrimSpace(producer)
	if !strings.Contains(prod, ":auto_bootstrap") {
		prod = prod + ":auto_bootstrap"
	}
	payload := env.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	return capture.ObserveRequest{
		SessionID:           sessionID,
		TaskID:              taskID,
		AgentType:           agentTypeFromEnvelope(env),
		WorkspaceID:         scopeField(payload, "workspace_id", "local_default_workspace"),
		ProjectID:           scopeField(payload, "project_id", "the-one"),
		RepoID:              scopeField(payload, "repo_id", "the-one"),
		EventType:           capture.EventSessionStart,
		SourceChannel:       capture.SourceChannelAgentSession,
		Actor:               capture.ActorAdapter,
		ContentSummary:      "ingest auto bootstrap session",
		CaptureCapabilities: defaultCaptureCapabilities(),
		SourceRefs:          defaultSourceRefs(prod),
		Session: &capture.SessionInput{
			GoalSummary: "auto bootstrap",
			Status:      capture.StatusActive,
		},
		Task: &capture.TaskInput{
			TaskSummary: taskID,
			Status:      capture.StatusActive,
		},
	}
}

func agentTypeFromEnvelope(env IngestEnvelope) string {
	if v := strings.TrimSpace(env.AgentType); v != "" {
		return v
	}
	return stringFromPayload(env.Payload, "agent_type")
}

func scopeField(payload map[string]any, key, fallback string) string {
	if v := stringFromPayload(payload, key); v != "" {
		return v
	}
	return fallback
}

func actorOrDefault(payload map[string]any, fallback string) string {
	if v := stringFromPayload(payload, "actor"); v != "" {
		return v
	}
	return fallback
}

func defaultSourceRefs(producer string) []capture.SourceRef {
	return []capture.SourceRef{{
		"source_type":      "agent_session",
		"capture_method":   capture.CaptureMethodAdapterHook,
		"protocol_version": ProtocolV1,
		"producer":         producer,
	}}
}

func defaultCaptureCapabilities() capture.CaptureCapabilities {
	return capture.CaptureCapabilities{
		ConversationCapture: true,
		ToolCallCapture:     true,
		ToolOutputCapture:   true,
		FileEditCapture:     true,
		SessionLifecycle:    true,
		MCPObserve:          true,
	}
}

func decodeSessionInput(payload map[string]any) *capture.SessionInput {
	raw, ok := payload["session"]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var s capture.SessionInput
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return &s
}

func decodeTaskInput(payload map[string]any) *capture.TaskInput {
	raw, ok := payload["task"]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var t capture.TaskInput
	if err := json.Unmarshal(data, &t); err != nil {
		return nil
	}
	return &t
}

func stringSliceFromPayload(payload map[string]any, key string) []string {
	raw, ok := payload[key]
	if !ok || raw == nil {
		return nil
	}
	switch t := raw.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func toObserveError(err error) (code, summary string) {
	if err == nil {
		return "", ""
	}
	msg := err.Error()
	if strings.Contains(msg, ":") {
		parts := strings.SplitN(msg, ":", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "INGEST_FAILED", msg
}

func ensureObserveHash(req *capture.ObserveRequest) error {
	if strings.TrimSpace(req.ContentHash) != "" {
		return nil
	}
	hash, err := capture.ComputeContentHash(*req)
	if err != nil {
		return fmt.Errorf("compute content hash: %w", err)
	}
	req.ContentHash = hash
	return nil
}
