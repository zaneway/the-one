package automation

import (
	"testing"
	"time"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/memory"
)

func TestBuildMemoryProvenanceFromRawEventAndAdmission(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	rawEvent := capture.RawEvent{
		ID:             "evt_prov_build",
		AgentType:      "codex",
		EventType:      capture.EventToolResultSummary,
		SourceChannel:  capture.SourceChannelAgentSession,
		SourceRefsJSON: `[{"source_type":"agent_session","capture_method":"adapter_hook","producer":"codex_hook:PostToolUse"}]`,
		CreatedAt:      now,
	}
	record := MemoryCandidateRecord{
		ID:         "cand_prov_build",
		RawEventID: rawEvent.ID,
		EvidenceID: "ev_prov_build",
		Provider:   "rule_based",
	}
	evidence := memory.Evidence{ID: "ev_prov_build", RawEventID: rawEvent.ID}
	admission := AdmissionResult{Decision: DecisionWriteProvisional, AdmissionScore: 0.74}

	provenance := buildMemoryProvenance(record, evidence, rawEvent, admission)

	if provenance.RawEventID != rawEvent.ID || provenance.EvidenceID != evidence.ID || provenance.CandidateID != record.ID {
		t.Fatalf("provenance = %+v, want raw/evidence/candidate ids", provenance)
	}
	if provenance.AgentType != "codex" || provenance.SourceChannel != capture.SourceChannelAgentSession {
		t.Fatalf("provenance = %+v, want codex agent_session", provenance)
	}
	if provenance.SourceProducer != "codex_hook:PostToolUse" || provenance.HookName != "PostToolUse" || provenance.HookPhase != HookPhasePostTool {
		t.Fatalf("provenance = %+v, want PostToolUse post_tool", provenance)
	}
	if provenance.CaptureMethod != capture.CaptureMethodAdapterHook || provenance.Provider != "rule_based" {
		t.Fatalf("provenance = %+v, want adapter_hook rule_based", provenance)
	}
	if provenance.DerivationStage != JobTypeComputeAdmission || provenance.AdmissionDecision != DecisionWriteProvisional || provenance.AdmissionScore != 0.74 {
		t.Fatalf("provenance = %+v, want compute admission decision", provenance)
	}
}

func TestBuildMemoryProvenanceMapsLegacyAfterToolUseToPostTool(t *testing.T) {
	rawEvent := capture.RawEvent{
		ID:             "evt_legacy_tool",
		AgentType:      "cursor",
		EventType:      capture.EventToolResultSummary,
		SourceChannel:  capture.SourceChannelAgentSession,
		SourceRefsJSON: `[{"capture_method":"adapter_hook","producer":"cursor_hook:afterToolUse"}]`,
	}
	record := MemoryCandidateRecord{ID: "cand_legacy_tool", RawEventID: rawEvent.ID, EvidenceID: "ev_legacy_tool", Provider: "rule_based"}
	evidence := memory.Evidence{ID: "ev_legacy_tool", RawEventID: rawEvent.ID}

	provenance := buildMemoryProvenance(record, evidence, rawEvent, AdmissionResult{Decision: DecisionWriteProvisional})

	if provenance.HookPhase != HookPhasePostTool {
		t.Fatalf("hook phase = %q, want %q for legacy afterToolUse producer", provenance.HookPhase, HookPhasePostTool)
	}
}

func TestBuildMemoryProvenanceInfersProducerWhenSourceRefMissesIt(t *testing.T) {
	rawEvent := capture.RawEvent{
		ID:             "evt_missing_producer",
		AgentType:      "claude_code",
		EventType:      capture.EventAgentResponseSummary,
		SourceChannel:  capture.SourceChannelAgentSession,
		SourceRefsJSON: `[{"source_type":"agent_session","capture_method":"adapter_hook","protocol_version":"v1"}]`,
	}
	record := MemoryCandidateRecord{ID: "cand_missing_producer", RawEventID: rawEvent.ID, EvidenceID: "ev_missing_producer", Provider: "rule_based"}
	evidence := memory.Evidence{ID: "ev_missing_producer", RawEventID: rawEvent.ID}

	provenance := buildMemoryProvenance(record, evidence, rawEvent, AdmissionResult{Decision: DecisionWriteProvisional})

	if provenance.SourceProducer != "claude_code_hook:Stop" || provenance.HookPhase != HookPhaseTurnEnd {
		t.Fatalf("provenance = %+v, want inferred claude Stop turn_end", provenance)
	}
}
