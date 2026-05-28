package adapter

import "testing"

func TestValidateIngestEnvelope(t *testing.T) {
	valid := IngestEnvelope{
		IngestID:        "ing_001",
		ProtocolVersion: ProtocolV1,
		Producer:        "codex_wrapper",
		SessionID:       "sess_001",
		EventType:       "agent.response.summary",
		Payload: map[string]any{
			"user_summary":  "继续修复",
			"agent_summary": "已完成修复",
		},
	}
	if err := ValidateIngestEnvelope(valid); err != nil {
		t.Fatalf("ValidateIngestEnvelope() error = %v", err)
	}

	invalid := valid
	invalid.IngestID = ""
	if err := ValidateIngestEnvelope(invalid); err == nil {
		t.Fatalf("ValidateIngestEnvelope() error = nil, want missing ingest_id")
	}
}

func TestTurnPayloadFromEnvelope(t *testing.T) {
	env := IngestEnvelope{
		IngestID:        "ing_001",
		ProtocolVersion: ProtocolV1,
		Producer:        "codex_wrapper",
		SessionID:       "sess_001",
		TurnID:          "turn_001",
		OccurredAt:      "2026-05-28T00:00:00Z",
		EventType:       "agent.response.summary",
		Payload: map[string]any{
			"workspace_id":   "ws",
			"project_id":     "project_a",
			"repo_id":        "repo_a",
			"agent_type":     "codex",
			"user_summary":   "继续",
			"agent_summary":  "完成",
			"is_substantive": true,
		},
	}
	payload, err := TurnPayloadFromEnvelope(env)
	if err != nil {
		t.Fatalf("TurnPayloadFromEnvelope() error = %v", err)
	}
	if payload.SessionID != env.SessionID || payload.TurnID != env.TurnID {
		t.Fatalf("payload session/turn mismatch: %+v", payload)
	}
	if payload.StartedAt != env.OccurredAt {
		t.Fatalf("payload started_at = %q, want %q", payload.StartedAt, env.OccurredAt)
	}
}
