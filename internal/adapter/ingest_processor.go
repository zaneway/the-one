package adapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/zaneway/theone/internal/capture"
)

// ObserveFunc 调用 capture 写入单条事件。
// 入参：ctx 与 capture.ObserveRequest；返回响应与错误。
// 调用方通常为 capture service 自身或 ingest 控制面测试替身。
type ObserveFunc func(ctx context.Context, req capture.ObserveRequest) (capture.ObserveResponse, error)

// EnsureSessionFunc 只确保 agent_session / agent_task 存在，不写 raw_event。
// 用于 bootstrap 阶段或被 suppress 的事件类型，避免漏建 session。
type EnsureSessionFunc func(ctx context.Context, req capture.ObserveRequest) error

// IngestProcessor P0 ingest 平面。
// 负责：
//  1. 拒绝 mcp: 前缀的 producer（必须走 MCP 控制面而非 ingest CLI）；
//  2. 调用 SessionBinder 解析 session/task；
//  3. 按 EventType 决定 kind 并对 atomic 事件做去重；
//  4. 调用 capture.ObserveFunc 写入 raw_event。
//
// 设计约束：所有去重/状态查询与写入分离，避免长事务持锁。
type IngestProcessor struct {
	Binder          *SessionBinder
	Ledger          *IngestLedger
	Failures        *FailureQueue
	StateStore      StateStore
	AtomicDedup     *AtomicDedupStore
	ExpandMode      string
	AtomicStripTurn bool
	Observe         ObserveFunc
	EnsureSession   EnsureSessionFunc
}

// IngestResult ingest 命令 stdout 结构。
// 字段含义：
//   - OK：整批是否成功（任何一条 Failed 且其它计数全 0 时为 false）；
//   - IngestID：本次 ingest 批次的全局 ID；
//   - Accepted：被 capture 实际接受并落库的 raw_event 数量；
//   - Deduped：被 ledger 或 atomic dedup 命中并跳过的数量；
//   - Suppressed：因业务策略（如 session.start）被压制不写 raw_event 的数量；
//   - Failed：单条失败计数，详情见 Failures；
//   - Failures：失败明细，便于 CLI 输出。
type IngestResult struct {
	OK         bool            `json:"ok"`
	IngestID   string          `json:"ingest_id"`
	Accepted   int             `json:"accepted"`
	Deduped    int             `json:"deduped"`
	Suppressed int             `json:"suppressed"`
	Failed     int             `json:"failed"`
	Failures   []FailureRecord `json:"failures"`
	Error      string          `json:"error,omitempty"`
}

// Process 处理一批 IngestWorkItem。
// 入参：ctx、ingestID（批次 ID）、items（待处理事件）。
// 返回：IngestResult，含 OK/Accepted/Deduped/Suppressed/Failed 计数与失败明细。
// 处理流程：
//  1. 空批次直接返回默认成功结果；
//  2. 第一条事件 producer 若以 mcp: 开头则整批拒绝（避免 ingest CLI 与 MCP 控制面互相覆盖）；
//  3. 逐条调用 processOne 处理；
//  4. 全部失败且无任何 accepted/deduped/suppressed 时把 OK 置为 false。
func (p *IngestProcessor) Process(ctx context.Context, ingestID string, items []IngestWorkItem) IngestResult {
	result := IngestResult{OK: true, IngestID: ingestID}
	if len(items) == 0 {
		return result
	}
	producer := strings.TrimSpace(items[0].Envelope.Producer)
	// 拒绝 mcp: 前缀 producer：MCP 控制面与 ingest CLI 不应同时写入同一路径
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
	// 只有"全失败且无任何有效处理"才把 OK 置为 false：部分成功仍视为整批可用
	if result.Failed > 0 && result.Accepted == 0 && result.Deduped == 0 && result.Suppressed == 0 {
		result.OK = false
	}
	return result
}

// processOne 处理单条 IngestWorkItem 并把结果累加到 result。
// 处理流程：
//  1. 通过 Ledger 查批次内幂等性，已处理则计 Deduped；
//  2. 解析 agent_type 并写回 envelope；
//  3. 通过 Binder 解析 session/task，重置 dedup 状态按需触发；
//  4. 推断 kind（lifecycle/atomic/turn_completed），atomic 事件额外做指纹去重；
//  5. 应抑制的 raw_event（session.start / tool.result.summary）走 ensureSuppressedEventReady；
//  6. 其余事件先 ensureSessionReady 再 buildRequests 并依次 Observe；
//  7. 全部成功时把 fingerprint 标记为已处理，并把 Ledger 中本条标记为已处理。
//
// 失败路径：任一步骤失败调用 failItem，累计到 result.Failures/Failed 并写 FailureQueue。
func (p *IngestProcessor) processOne(ctx context.Context, ingestID string, item IngestWorkItem, result *IngestResult) {
	// 批次内幂等：同 ingestID+eventIndex 已处理过则跳过
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
	// Binder 显式要求重置 dedup 状态时清空（典型场景：session 切换）
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

	// atomic 事件 v2 模式下走指纹去重，避免 hook 重发时重复入 capture
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

	// 被抑制的 raw_event 仍然要保证 session/task 就绪，但不再产生 raw_event
	if shouldSuppressRawEvent(env.EventType) {
		if err := p.ensureSuppressedEventReady(ctx, env, sessionID, taskID); err != nil {
			code, summary := toObserveError(err)
			p.failItem(ingestID, item, sessionID, taskID, code, summary, result)
			return
		}
		_ = p.Ledger.Mark(ingestID, item.EventIndex)
		result.Suppressed++
		return
	}

	// session.start 自身就是 session/task 的建立点，不应再触发 ensure
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

	// 真正写入后再回填 atomic 指纹，重复事件下轮直接命中 Dedup
	if kind == KindCaptureAtomic && p.AtomicDedup != nil && IsExpandModeV2(p.ExpandMode) && accepted > 0 {
		if fp, fpErr := AtomicFingerprint(env.EventType, env.Payload); fpErr == nil {
			_ = p.AtomicDedup.Mark(sessionID, env.EventType, fp)
		}
	}

	_ = p.Ledger.Mark(ingestID, item.EventIndex)
	result.Accepted += accepted
	result.Deduped += deduped
}

// ensureSessionReady 在写 raw_event 前确保 session/task 在 store 中已存在。
// 入参：env、sessionID、taskID。
// 处理流程：构造 bootstrap ObserveRequest → 计算 observe hash → 调用 EnsureSession；
// 成功后再调用 Binder.MarkBootstrapTask 把任务记为已 bootstrap。
// 错误语义：EnsureSession 失败会被包装为 errSessionNotReady，让上层 failItem 拿到 SESSION_NOT_READED 类错误码。
func (p *IngestProcessor) ensureSessionReady(ctx context.Context, env IngestEnvelope, sessionID, taskID string) error {
	bootstrap := bootstrapObserveRequest(env, sessionID, taskID, env.Producer)
	if err := ensureObserveHash(&bootstrap); err != nil {
		return err
	}
	if p.EnsureSession == nil {
		return nil
	}
	if err := p.EnsureSession(ctx, bootstrap); err != nil {
		return fmt.Errorf("%w: %v", errSessionNotReady, err)
	}
	_ = p.Binder.MarkBootstrapTask(agentTypeFromEnvelope(env))
	return nil
}

// ensureSuppressedEventReady 对被抑制的 raw_event 仍保证 session/task 元数据就绪。
// 当前仅处理 session.start：它本身不会产生 raw_event，但需要把 AgentSession 落库。
// tool.result.summary 等其它抑制事件直接返回 nil，由其它路径处理。
func (p *IngestProcessor) ensureSuppressedEventReady(ctx context.Context, env IngestEnvelope, sessionID, taskID string) error {
	if p.EnsureSession == nil {
		return nil
	}
	var req capture.ObserveRequest
	var err error
	switch env.EventType {
	case capture.EventSessionStart:
		req, err = observeFromLifecycle(env, sessionID, taskID)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	if err := ensureObserveHash(&req); err != nil {
		return err
	}
	return p.EnsureSession(ctx, req)
}

// shouldSuppressRawEvent 决定某种 EventType 是否要写 raw_event。
// 当前白名单：session.start 与 tool.result.summary。这两类事件或属于元数据，
// 或由 Observe 在 tool_call 上下文已聚合，落库会造成冗余与重复计数。
func shouldSuppressRawEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case capture.EventSessionStart, capture.EventToolResultSummary:
		return true
	default:
		return false
	}
}

// buildRequests 根据 kind 把 IngestEnvelope 转换为一组 capture.ObserveRequest。
// 入参：env、kind、sessionID、taskID。
// 返回：待 Observe 的请求切片；任一子步失败返回错误。
// 关键分支：
//   - lifecycle/atomic：单条 ObserveRequest；
//   - turn_completed：拆出 TurnPayload，由 TurnRuntime 展开为多条 observe；
//   - v2 模式下 turn.completed 误带 tool_results/file_edits 时按 AtomicStripTurn 决定剥离还是拒绝。
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
			// v2 模式强制 turn.completed 不带 tool_results/file_edits；dogfood 场景下可剥离而非拒绝
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

// attachEnvelopeProducer 把 envelope.Producer 注入每条 ObserveRequest 的 source_ref。
// 已存在 source_ref 时只更新其中 source_type=agent_session 的条目；不存在则使用 defaultSourceRefs 生成。
// 设计约束：保持原有 source_ref 结构不被破坏，仅在最小颗粒上回填 producer，便于排障时定位事件来源。
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
		// 找到 source_type=agent_session 的 source_ref，把 producer 注入到其 map 中
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

// failItem 把单条失败累计到 result 并写入 FailureQueue。
// 入参：ingestID/EventIndex/错误码/可读摘要/sessionID/taskID。
// 设计说明：code 为空时退化为 INGEST_FAILED，避免空错误码落到 CLI 输出。
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
