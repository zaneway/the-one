package adapter

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// BatchEnvelope 批量入站包络。
type BatchEnvelope struct {
	IngestID        string            `json:"ingest_id"`
	ProtocolVersion string            `json:"protocol_version"`
	Producer        string            `json:"producer"`
	AgentType       string            `json:"agent_type,omitempty"`
	SessionID       string            `json:"session_id"`
	Events          []IngestEventItem `json:"events"`
}

// IngestEventItem 批内单条事件。
type IngestEventItem struct {
	Kind       string         `json:"kind,omitempty"`
	EventType  string         `json:"event_type"`
	TurnID     string         `json:"turn_id,omitempty"`
	OccurredAt string         `json:"occurred_at,omitempty"`
	Payload    map[string]any `json:"payload"`
}

// IngestWorkItem 处理器内部统一条目。
type IngestWorkItem struct {
	EventIndex int
	Envelope   IngestEnvelope
}

// DecodeIngestInput 判别根对象为单条或批量包络。
func DecodeIngestInput(r io.Reader) ([]IngestWorkItem, string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, "", err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, "", fmt.Errorf("stdin is empty, expected JSON object")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, "", fmt.Errorf("decode ingest root: %w", err)
	}
	if _, ok := probe["events"]; ok {
		var batch BatchEnvelope
		if err := json.Unmarshal(raw, &batch); err != nil {
			return nil, "", fmt.Errorf("decode batch envelope: %w", err)
		}
		if err := ValidateBatchEnvelope(batch); err != nil {
			return nil, "", err
		}
		items := make([]IngestWorkItem, 0, len(batch.Events))
		for i, ev := range batch.Events {
			items = append(items, IngestWorkItem{
				EventIndex: i,
				Envelope:   batch.ToEnvelope(ev, batch.SessionID),
			})
		}
		return items, batch.IngestID, nil
	}
	var single IngestEnvelope
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, "", fmt.Errorf("decode ingest envelope: %w", err)
	}
	if err := ValidateIngestEnvelope(single); err != nil {
		return nil, "", err
	}
	return []IngestWorkItem{{EventIndex: 0, Envelope: single}}, single.IngestID, nil
}

// ValidateBatchEnvelope 校验批量包络。
func ValidateBatchEnvelope(batch BatchEnvelope) error {
	if strings.TrimSpace(batch.IngestID) == "" {
		return fmt.Errorf("missing required field: ingest_id")
	}
	if strings.TrimSpace(batch.ProtocolVersion) == "" {
		return fmt.Errorf("missing required field: protocol_version")
	}
	if strings.TrimSpace(batch.Producer) == "" {
		return fmt.Errorf("missing required field: producer")
	}
	if strings.TrimSpace(batch.SessionID) == "" {
		return fmt.Errorf("missing required field: session_id")
	}
	if batch.Events == nil {
		return fmt.Errorf("missing required field: events")
	}
	for i, ev := range batch.Events {
		if strings.TrimSpace(ev.EventType) == "" {
			return fmt.Errorf("events[%d]: missing event_type", i)
		}
		if len(ev.Payload) == 0 {
			return fmt.Errorf("events[%d]: missing payload", i)
		}
	}
	return nil
}

func (b BatchEnvelope) ToEnvelope(item IngestEventItem, defaultSessionID string) IngestEnvelope {
	sessionID := strings.TrimSpace(defaultSessionID)
	if sid := stringFromPayload(item.Payload, "session_id"); sid != "" {
		sessionID = sid
	}
	return IngestEnvelope{
		IngestID:        b.IngestID,
		ProtocolVersion: b.ProtocolVersion,
		Producer:        b.Producer,
		SessionID:       sessionID,
		TurnID:          item.TurnID,
		EventType:       item.EventType,
		OccurredAt:      item.OccurredAt,
		Payload:         item.Payload,
	}
}
