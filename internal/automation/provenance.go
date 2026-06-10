package automation

import (
	"encoding/json"
	"strings"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/memory"
)

const provenancePipelineAutomated = "raw_event->evidence->candidate->admission->memory"

func buildMemoryProvenance(record MemoryCandidateRecord, evidence memory.Evidence, rawEvent capture.RawEvent, admission AdmissionResult) *MemoryProvenance {
	ref := firstSourceRef(rawEvent.SourceRefsJSON)
	producer := sourceRefString(ref, "producer")
	if producer == "" {
		producer = inferProducerFromRawEvent(rawEvent)
	}
	hookName := hookNameFromProducer(producer)
	return &MemoryProvenance{
		RawEventID:        firstNonEmpty(record.RawEventID, evidence.RawEventID, rawEvent.ID),
		EvidenceID:        firstNonEmpty(record.EvidenceID, evidence.ID),
		CandidateID:       record.ID,
		AgentType:         rawEvent.AgentType,
		SourceChannel:     rawEvent.SourceChannel,
		SourceProducer:    producer,
		HookName:          hookName,
		HookPhase:         hookPhaseFromSource(producer, hookName, rawEvent),
		EventType:         rawEvent.EventType,
		CaptureMethod:     sourceRefString(ref, "capture_method"),
		Pipeline:          provenancePipelineAutomated,
		Provider:          record.Provider,
		DerivationStage:   JobTypeComputeAdmission,
		AdmissionDecision: admission.Decision,
		AdmissionScore:    admission.AdmissionScore,
		TraceJSON:         provenanceTraceJSON(record, evidence, rawEvent),
	}
}

func inferProducerFromRawEvent(rawEvent capture.RawEvent) string {
	agentType := strings.TrimSpace(rawEvent.AgentType)
	if rawEvent.SourceChannel == capture.SourceChannelMCPTool {
		return "mcp:memory_observe"
	}
	if agentType == "" {
		return ""
	}
	switch rawEvent.EventType {
	case capture.EventSessionStart:
		return agentType + "_hook:SessionStart"
	case capture.EventSessionEnd:
		return agentType + "_hook:SessionEnd"
	case capture.EventAgentResponseSummary:
		if agentType == "cursor" {
			return "cursor_hook:afterAgentResponse"
		}
		return agentType + "_hook:Stop"
	case capture.EventTurnCompleted:
		if agentType == "cursor" {
			return "cursor_hook:afterAgentResponse"
		}
		return agentType + "_hook:Stop"
	case capture.EventToolResultSummary:
		if agentType == "cursor" {
			return "cursor_hook:afterMCPExecution"
		}
		return agentType + "_hook:PostToolUse"
	case capture.EventFileEditSummary:
		if agentType == "cursor" {
			return "cursor_hook:afterFileEdit"
		}
		return agentType + "_hook:PostToolUse"
	default:
		return ""
	}
}

func firstSourceRef(sourceRefsJSON string) map[string]any {
	if strings.TrimSpace(sourceRefsJSON) == "" {
		return nil
	}
	var refs []map[string]any
	if err := json.Unmarshal([]byte(sourceRefsJSON), &refs); err == nil && len(refs) > 0 {
		return refs[0]
	}
	var ref map[string]any
	if err := json.Unmarshal([]byte(sourceRefsJSON), &ref); err == nil {
		return ref
	}
	return nil
}

func sourceRefString(ref map[string]any, key string) string {
	if len(ref) == 0 {
		return ""
	}
	if value, ok := ref[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func hookNameFromProducer(producer string) string {
	producer = strings.TrimSpace(producer)
	if producer == "" {
		return ""
	}
	if idx := strings.LastIndex(producer, ":"); idx >= 0 && idx+1 < len(producer) {
		return strings.TrimSpace(producer[idx+1:])
	}
	return producer
}

func hookPhaseFromSource(producer, hookName string, rawEvent capture.RawEvent) string {
	normalizedProducer := strings.ToLower(strings.TrimSpace(producer))
	normalizedHook := strings.ToLower(strings.TrimSpace(hookName))
	switch {
	case strings.Contains(normalizedProducer, "auto_bootstrap") || strings.Contains(normalizedHook, "auto_bootstrap"):
		return HookPhaseAutoBootstrap
	case strings.HasPrefix(normalizedProducer, "mcp:") || rawEvent.SourceChannel == capture.SourceChannelMCPTool:
		return HookPhaseManualObserve
	case normalizedHook == "userpromptsubmit" || normalizedHook == "beforesubmitprompt" || normalizedHook == "before-submit-prompt":
		return HookPhasePrePrompt
	case normalizedHook == "posttooluse" || normalizedHook == "aftermcpexecution" || normalizedHook == "after-mcp-execution" || normalizedHook == "aftertooluse" || normalizedHook == "after-tool-use":
		return HookPhasePostTool
	case normalizedHook == "afterfileedit" || normalizedHook == "after-file-edit":
		return HookPhaseFileEdit
	case normalizedHook == "sessionstart" || normalizedHook == "session-start":
		return HookPhaseSessionStart
	case normalizedHook == "sessionend" || normalizedHook == "session-end":
		return HookPhaseSessionEnd
	case normalizedHook == "stop" || normalizedHook == "afteragentresponse" || normalizedHook == "after-agent-response":
		return HookPhaseTurnEnd
	case rawEvent.EventType == capture.EventSessionStart:
		return HookPhaseSessionStart
	case rawEvent.EventType == capture.EventSessionEnd:
		return HookPhaseSessionEnd
	case rawEvent.EventType == capture.EventTurnCompleted:
		return HookPhaseTurnEnd
	default:
		return HookPhaseUnknown
	}
}

func provenanceTraceJSON(record MemoryCandidateRecord, evidence memory.Evidence, rawEvent capture.RawEvent) string {
	trace := map[string]any{
		"raw_event_id":  firstNonEmpty(record.RawEventID, evidence.RawEventID, rawEvent.ID),
		"evidence_id":   firstNonEmpty(record.EvidenceID, evidence.ID),
		"candidate_id":  record.ID,
		"source_refs":   rawEvent.SourceRefsJSON,
		"event_type":    rawEvent.EventType,
		"source_type":   evidence.SourceType,
		"candidate_key": record.DedupKey,
	}
	data, err := json.Marshal(trace)
	if err != nil {
		return ""
	}
	return string(data)
}
