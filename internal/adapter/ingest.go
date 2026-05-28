package adapter

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateIngestEnvelope 校验三阶段入站包络最小约束。
func ValidateIngestEnvelope(env IngestEnvelope) error {
	if strings.TrimSpace(env.IngestID) == "" {
		return fmt.Errorf("missing required field: ingest_id")
	}
	if strings.TrimSpace(env.ProtocolVersion) == "" {
		return fmt.Errorf("missing required field: protocol_version")
	}
	if strings.TrimSpace(env.Producer) == "" {
		return fmt.Errorf("missing required field: producer")
	}
	if strings.TrimSpace(env.SessionID) == "" {
		return fmt.Errorf("missing required field: session_id")
	}
	if strings.TrimSpace(env.EventType) == "" {
		return fmt.Errorf("missing required field: event_type")
	}
	if len(env.Payload) == 0 {
		return fmt.Errorf("missing required field: payload")
	}
	return nil
}

// TurnPayloadFromEnvelope 将入站包络转换为回合载荷。
func TurnPayloadFromEnvelope(env IngestEnvelope) (TurnPayload, error) {
	raw, err := json.Marshal(env.Payload)
	if err != nil {
		return TurnPayload{}, fmt.Errorf("marshal envelope payload: %w", err)
	}
	var payload TurnPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return TurnPayload{}, fmt.Errorf("decode turn payload: %w", err)
	}
	if payload.SessionID == "" {
		payload.SessionID = env.SessionID
	}
	if payload.TurnID == "" {
		payload.TurnID = env.TurnID
	}
	if payload.StartedAt == "" {
		payload.StartedAt = env.OccurredAt
	}
	return payload, nil
}
