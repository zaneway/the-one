package adapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/zaneway/theone/internal/capture"
)

// ObserveFunc 调用 capture 写入单条事件。
type ObserveFunc func(ctx context.Context, req capture.ObserveRequest) (capture.ObserveResponse, error)

// IngestProcessor P0 ingest 平面。
type IngestProcessor struct {
	Binder          *SessionBinder
	Ledger          *IngestLedger
	Failures        *FailureQueue
	StateStore      StateStore
	AtomicDedup     *AtomicDedupStore
	ExpandMode      string
	AtomicStripTurn bool
	Observe         ObserveFunc
}

// IngestResult ingest 命令 stdout 结构。
type IngestResult struct {
	OK       bool            `json:"ok"`
	IngestID string          `json:"ingest_id"`
	Accepted int             `json:"accepted"`
	Deduped  int             `json:"deduped"`
	Failed   int             `json:"failed"`
	Failures []FailureRecord `json:"failures"`
	Error    string          `json:"error,omitempty"`
}

// Process 处理一批 IngestWorkItem。
func (p *IngestProcessor) Process(ctx context.Context, ingestID string, items []IngestWorkItem) IngestResult {
	result := IngestResult{OK: true, IngestID: ingestID}
	if len(items) == 0 {
		return result
	}
	producer := strings.TrimSpace(items[0].Envelope.Producer)
	if strings.HasPrefix(strings.ToLower(producer), "mcp:") {
		result.OK = false
		result.Error = errWrongTransport.Error()
		result.Failed = len(items)
		rec := FailureRecord{
			IngestID:     ingestID,
			EventIndex:   0,
			ErrorCode:    errWrongTransport.Error(),
			ErrorSummary: "producer must not use mcp: prefix for ingest",
		}
		result.Failures = append(result.Failures, rec)
		_ = p.Failures.Append(rec)
		return result
	}

	for _, item := range items {
		p.processOne(ctx, ingestID, item, &result)
	}
	if result.Failed > 0 && result.Accepted == 0 && result.Deduped == 0 {
		result.OK = false
	}
	return result
}

func (p *IngestProcessor) processOne(ctx context.Context, ingestID string, item IngestWorkItem, result *IngestResult) {
	if hit, err := p.Ledger.Contains(ingestID, item.EventIndex); err == nil && hit {
		result.Deduped++
		return
	}

	env := item.Envelope
	env.IngestID = ingestID
	agentType := agentTypeFromEnvelope(env)
	if agentType == "" {
		p.failItem(ingestID, item, "", "", errInvalidSession.Error(), "missing agent_type", result)
		return
	}
	env.AgentType = agentType

	resolved, err := p.Binder.Resolve(ResolveInput{
		AgentType: agentType,
		Producer:  env.Producer,
		EventType: env.EventType,
		Envelope:  env,
	})
	if err != nil {
		code, summary := toObserveError(err)
		p.failItem(ingestID, item, "", "", code, summary, result)
		return
	}
	sessionID, taskID := resolved.SessionID, resolved.TaskID
	if resolved.ResetDedup && p.StateStore != nil {
		_ = p.StateStore.Clear()
	}

	kind := strings.TrimSpace(env.Kind)
	if kind == "" {
		kind, err = InferKindWithExpandMode(env.EventType, env.Payload, p.ExpandMode)
		if err != nil {
			code := err.Error()
			if err == errInvalidAtomicShape {
				code = "INVALID_ATOMIC_SHAPE"
			}
			p.failItem(ingestID, item, sessionID, taskID, code, err.Error(), result)
			return
		}
	}

	if kind == KindCaptureAtomic && p.AtomicDedup != nil && IsExpandModeV2(p.ExpandMode) {
		fp, fpErr := AtomicFingerprint(env.EventType, env.Payload)
		if fpErr == nil {
			if hit, err := p.AtomicDedup.Contains(sessionID, env.EventType, fp); err == nil && hit {
				_ = p.Ledger.Mark(ingestID, item.EventIndex)
				result.Deduped++
				return
			}
		}
	}

	if env.EventType != capture.EventSessionStart {
		if err := p.ensureSessionReady(ctx, env, sessionID, taskID); err != nil {
			code, summary := toObserveError(err)
			p.failItem(ingestID, item, sessionID, taskID, code, summary, result)
			return
		}
	}

	requests, err := p.buildRequests(env, kind, sessionID, taskID)
	if err != nil {
		p.failItem(ingestID, item, sessionID, taskID, "BUILD_FAILED", err.Error(), result)
		return
	}

	accepted := 0
	deduped := 0
	var lastErr error
	for _, req := range requests {
		if err := ensureObserveHash(&req); err != nil {
			lastErr = err
			break
		}
		resp, err := p.Observe(ctx, req)
		if err != nil {
			lastErr = err
			break
		}
		if resp.Deduped {
			deduped++
		} else if resp.Accepted {
			accepted++
		}
	}
	if lastErr != nil {
		code, summary := toObserveError(lastErr)
		p.failItem(ingestID, item, sessionID, taskID, code, summary, result)
		return
	}
	if accepted == 0 && deduped == 0 {
		p.failItem(ingestID, item, sessionID, taskID, "NO_EVENTS", "no observe requests produced", result)
		return
	}

	if kind == KindCaptureAtomic && p.AtomicDedup != nil && IsExpandModeV2(p.ExpandMode) && accepted > 0 {
		if fp, fpErr := AtomicFingerprint(env.EventType, env.Payload); fpErr == nil {
			_ = p.AtomicDedup.Mark(sessionID, env.EventType, fp)
		}
	}

	_ = p.Ledger.Mark(ingestID, item.EventIndex)
	result.Accepted += accepted
	result.Deduped += deduped
}

func (p *IngestProcessor) ensureSessionReady(ctx context.Context, env IngestEnvelope, sessionID, taskID string) error {
	bootstrap := bootstrapObserveRequest(env, sessionID, taskID, env.Producer)
	if err := ensureObserveHash(&bootstrap); err != nil {
		return err
	}
	if _, err := p.Observe(ctx, bootstrap); err != nil {
		return fmt.Errorf("%w: %v", errSessionNotReady, err)
	}
	_ = p.Binder.MarkBootstrapTask(agentTypeFromEnvelope(env))
	return nil
}

func (p *IngestProcessor) buildRequests(env IngestEnvelope, kind, sessionID, taskID string) ([]capture.ObserveRequest, error) {
	switch kind {
	case KindSessionLifecycle:
		req, err := observeFromLifecycle(env, sessionID, taskID)
		if err != nil {
			return nil, err
		}
		return []capture.ObserveRequest{req}, nil
	case KindCaptureAtomic:
		req, err := observeFromAtomic(env, sessionID, taskID)
		if err != nil {
			return nil, err
		}
		return []capture.ObserveRequest{req}, nil
	case KindTurnCompleted:
		payload, err := TurnPayloadFromEnvelope(env)
		if err != nil {
			return nil, err
		}
		payload.SessionID = sessionID
		payload.TaskID = taskID
		if IsExpandModeV2(p.ExpandMode) {
			if len(payload.ToolResults) > 0 || len(payload.FileEdits) > 0 {
				if p.AtomicStripTurn {
					payload.ToolResults = nil
					payload.FileEdits = nil
				} else {
					return nil, fmt.Errorf("%w: turn.completed must not include tool_results/file_edits in v2", errInvalidAtomicShape)
				}
			}
		}
		runtime := NewTurnRuntimeWithExpandMode(p.StateStore, p.ExpandMode)
		requests, err := runtime.BuildObserveRequests(payload)
		if err != nil {
			return nil, err
		}
		return attachEnvelopeProducer(requests, env.Producer), nil
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
}

func attachEnvelopeProducer(requests []capture.ObserveRequest, producer string) []capture.ObserveRequest {
	producer = strings.TrimSpace(producer)
	if producer == "" {
		return requests
	}
	for i := range requests {
		if len(requests[i].SourceRefs) == 0 {
			requests[i].SourceRefs = defaultSourceRefs(producer)
			continue
		}
		target := 0
		for idx, ref := range requests[i].SourceRefs {
			if ref["source_type"] == "agent_session" {
				target = idx
				break
			}
		}
		requests[i].SourceRefs[target]["producer"] = producer
	}
	return requests
}

func (p *IngestProcessor) failItem(ingestID string, item IngestWorkItem, sessionID, taskID, code, summary string, result *IngestResult) {
	if code == "" {
		code = "INGEST_FAILED"
	}
	rec := FailureRecord{
		IngestID:     ingestID,
		EventIndex:   item.EventIndex,
		ErrorCode:    code,
		ErrorSummary: summary,
		SessionID:    sessionID,
		TaskID:       taskID,
	}
	result.Failures = append(result.Failures, rec)
	result.Failed++
	_ = p.Failures.Append(rec)
}
