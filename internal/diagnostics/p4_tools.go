package diagnostics

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/zaneway/the-one/internal/docindex"
	"github.com/zaneway/the-one/internal/mcp"
	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/retrieval"
)

const diagnosticsLimitMax = 100

// RetrievalTracesRequest 是 memory.retrieval.traces 的请求参数。
// 查询必须带 workspace_id，避免 retrieval_trace 在增长后被无边界扫描。
type RetrievalTracesRequest struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id,omitempty"`
	RepoID      string `json:"repo_id,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	Status      string `json:"status,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type RetrievalTracesResponse struct {
	Traces      []RetrievalTraceDiagnostic `json:"traces"`
	Diagnostics []string                   `json:"diagnostics,omitempty"`
}

type RetrievalTraceDiagnostic struct {
	TraceID        string    `json:"trace_id"`
	SessionID      string    `json:"session_id,omitempty"`
	TaskID         string    `json:"task_id,omitempty"`
	WorkspaceID    string    `json:"workspace_id,omitempty"`
	ProjectID      string    `json:"project_id,omitempty"`
	RepoID         string    `json:"repo_id,omitempty"`
	QuerySummary   string    `json:"query_summary,omitempty"`
	TaskSummary    string    `json:"task_summary,omitempty"`
	Intent         string    `json:"retrieval_intent,omitempty"`
	Mode           string    `json:"retrieval_mode,omitempty"`
	UsedFTS        bool      `json:"used_fts"`
	UsedVector     bool      `json:"used_vector"`
	UsedRelation   bool      `json:"used_relation"`
	UsedCodeIndex  bool      `json:"used_code_index"`
	UsedDocIndex   bool      `json:"used_doc_index"`
	FallbackReason string    `json:"fallback_reason,omitempty"`
	CandidateCount int       `json:"candidate_count"`
	InjectedCount  int       `json:"injected_count"`
	LatencyMS      int64     `json:"latency_ms"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// RetrievalAccessLogsRequest 是 memory.retrieval.access_logs 的请求参数。
// 必须按 retrieval_trace_id 或 memory_id 查询，避免访问日志诊断入口扫描全表。
type RetrievalAccessLogsRequest struct {
	RetrievalTraceID string `json:"retrieval_trace_id,omitempty"`
	MemoryID         string `json:"memory_id,omitempty"`
	EventType        string `json:"event_type,omitempty"`
	Limit            int    `json:"limit,omitempty"`
}

type RetrievalAccessLogsResponse struct {
	AccessLogs  []AccessLogDiagnostic `json:"access_logs"`
	Diagnostics []string              `json:"diagnostics,omitempty"`
}

type AccessLogDiagnostic struct {
	AccessLogID      string                `json:"access_log_id"`
	MemoryID         string                `json:"memory_id"`
	SessionID        string                `json:"session_id,omitempty"`
	TaskID           string                `json:"task_id,omitempty"`
	RetrievalTraceID string                `json:"retrieval_trace_id,omitempty"`
	EventType        string                `json:"event_type"`
	EventWeight      float64               `json:"event_weight"`
	SourceType       string                `json:"source_type,omitempty"`
	SourceQuality    float64               `json:"source_quality"`
	QuerySummary     string                `json:"query_summary,omitempty"`
	Rank             int                   `json:"rank,omitempty"`
	Score            float64               `json:"score,omitempty"`
	ScoreBreakdown   memory.ScoreBreakdown `json:"score_breakdown"`
	InclusionReasons []string              `json:"inclusion_reasons,omitempty"`
	UsedInContext    bool                  `json:"used_in_context"`
	FeedbackSummary  string                `json:"feedback_summary,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
}

type CodeRefsRequest struct {
	MemoryID      string `json:"memory_id,omitempty"`
	RepoID        string `json:"repo_id,omitempty"`
	FilePath      string `json:"file_path,omitempty"`
	Symbol        string `json:"symbol,omitempty"`
	ResolveStatus string `json:"resolve_status,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type CodeRefsResponse struct {
	CodeRefs    []memory.CodeRef `json:"code_refs"`
	Diagnostics []string         `json:"diagnostics,omitempty"`
}

type DocSnapshotsRequest struct {
	WorkspaceID     string `json:"workspace_id"`
	ProjectID       string `json:"project_id,omitempty"`
	RepoID          string `json:"repo_id,omitempty"`
	Path            string `json:"doc_path"`
	ContentHash     string `json:"content_hash,omitempty"`
	IncludeSections bool   `json:"include_sections,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

type DocSnapshotsResponse struct {
	Snapshots   []docindex.DocumentSnapshot `json:"snapshots"`
	Diagnostics []string                    `json:"diagnostics,omitempty"`
}

type DocDiffRequest struct {
	WorkspaceID    string `json:"workspace_id"`
	ProjectID      string `json:"project_id,omitempty"`
	RepoID         string `json:"repo_id,omitempty"`
	Path           string `json:"doc_path"`
	BaseSnapshotID string `json:"base_snapshot_id,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type DocDiffResponse struct {
	DocPath           string                     `json:"doc_path"`
	CurrentSnapshotID string                     `json:"current_snapshot_id,omitempty"`
	BaseSnapshotID    string                     `json:"base_snapshot_id,omitempty"`
	CurrentHash       string                     `json:"current_hash,omitempty"`
	BaseHash          string                     `json:"base_hash,omitempty"`
	DocChanged        bool                       `json:"doc_changed"`
	ChangedSections   []DocSectionDiffDiagnostic `json:"changed_sections,omitempty"`
	Diagnostics       []string                   `json:"diagnostics,omitempty"`
}

type DocSectionDiffDiagnostic struct {
	SectionID   string   `json:"section_id"`
	HeadingPath []string `json:"heading_path,omitempty"`
	StartLine   int      `json:"start_line,omitempty"`
	EndLine     int      `json:"end_line,omitempty"`
	ChangeType  string   `json:"change_type"`
	CurrentHash string   `json:"current_hash,omitempty"`
	BaseHash    string   `json:"base_hash,omitempty"`
}

// RetrievalTracesTool 查询 P4 retrieval_trace 诊断记录。
// 返回 query/task 的短摘要和 used flags，不返回完整 prompt、工具输出或文档正文。
func (s *Service) RetrievalTracesTool(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req RetrievalTracesRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, diagnosticsValidationError("invalid retrieval traces params")
	}
	limit, notes := normalizeDiagnosticsLimit(req.Limit)
	traces, err := s.store.ListRetrievalTraces(ctx, retrieval.TraceQuery{
		WorkspaceID: strings.TrimSpace(req.WorkspaceID),
		ProjectID:   strings.TrimSpace(req.ProjectID),
		RepoID:      strings.TrimSpace(req.RepoID),
		SessionID:   strings.TrimSpace(req.SessionID),
		TaskID:      strings.TrimSpace(req.TaskID),
		Status:      retrieval.TraceStatus(strings.TrimSpace(req.Status)),
		Limit:       limit,
	})
	if err != nil {
		return nil, diagnosticsMCPError(err)
	}
	resp := RetrievalTracesResponse{Traces: make([]RetrievalTraceDiagnostic, 0, len(traces)), Diagnostics: notes}
	for _, trace := range traces {
		resp.Traces = append(resp.Traces, traceDiagnostic(trace))
	}
	return resp, nil
}

// RetrievalAccessLogsTool 查询 P4 memory_access_log 诊断记录。
// 响应只包含访问摘要、分数拆解和 inclusion reason，不返回 memory content。
func (s *Service) RetrievalAccessLogsTool(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req RetrievalAccessLogsRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, diagnosticsValidationError("invalid retrieval access logs params")
	}
	limit, notes := normalizeDiagnosticsLimit(req.Limit)
	records, err := s.store.ListMemoryAccessLogs(ctx, retrieval.AccessLogQuery{
		RetrievalTraceID: strings.TrimSpace(req.RetrievalTraceID),
		MemoryID:         strings.TrimSpace(req.MemoryID),
		EventType:        strings.TrimSpace(req.EventType),
		Limit:            limit,
	})
	if err != nil {
		return nil, diagnosticsMCPError(err)
	}
	resp := RetrievalAccessLogsResponse{AccessLogs: make([]AccessLogDiagnostic, 0, len(records)), Diagnostics: notes}
	for _, record := range records {
		resp.AccessLogs = append(resp.AccessLogs, accessLogDiagnostic(record))
	}
	return resp, nil
}

// CodeRefsTool 查询 code_ref 诊断记录。
// 该工具只返回 repo/file/symbol/hash/resolve_status，不返回源码正文或调用关系。
func (s *Service) CodeRefsTool(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req CodeRefsRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, diagnosticsValidationError("invalid code refs params")
	}
	limit, notes := normalizeDiagnosticsLimit(req.Limit)
	refs, err := s.store.ListCodeRefs(ctx, memory.CodeRefQuery{
		MemoryID:      strings.TrimSpace(req.MemoryID),
		RepoID:        strings.TrimSpace(req.RepoID),
		FilePath:      strings.TrimSpace(req.FilePath),
		Symbol:        strings.TrimSpace(req.Symbol),
		ResolveStatus: strings.TrimSpace(req.ResolveStatus),
		Limit:         limit,
	})
	if err != nil {
		return nil, diagnosticsMCPError(err)
	}
	return CodeRefsResponse{CodeRefs: refs, Diagnostics: notes}, nil
}

// DocSnapshotsTool 查询 Markdown 文档 snapshot 诊断记录。
// 响应只包含路径、hash、标题路径、行号和摘要，不返回完整文档正文。
func (s *Service) DocSnapshotsTool(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req DocSnapshotsRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, diagnosticsValidationError("invalid doc snapshots params")
	}
	limit, notes := normalizeDiagnosticsLimit(req.Limit)
	snapshots, err := s.store.ListDocSnapshots(ctx, docindex.SnapshotQuery{
		WorkspaceID:     strings.TrimSpace(req.WorkspaceID),
		ProjectID:       strings.TrimSpace(req.ProjectID),
		RepoID:          strings.TrimSpace(req.RepoID),
		Path:            strings.TrimSpace(req.Path),
		ContentHash:     strings.TrimSpace(req.ContentHash),
		IncludeSections: req.IncludeSections,
		Limit:           limit,
	})
	if err != nil {
		return nil, diagnosticsMCPError(err)
	}
	return DocSnapshotsResponse{Snapshots: snapshots, Diagnostics: notes}, nil
}

// DocDiffTool 比较同一文档的当前 snapshot 和基线 snapshot。
// 默认以最新 snapshot 为 current、前一个 snapshot 为 base；base_snapshot_id 可显式指定基线。
func (s *Service) DocDiffTool(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req DocDiffRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, diagnosticsValidationError("invalid doc diff params")
	}
	limit, notes := normalizeDiagnosticsLimit(req.Limit)
	snapshots, err := s.store.ListDocSnapshots(ctx, docindex.SnapshotQuery{
		WorkspaceID:     strings.TrimSpace(req.WorkspaceID),
		ProjectID:       strings.TrimSpace(req.ProjectID),
		RepoID:          strings.TrimSpace(req.RepoID),
		Path:            strings.TrimSpace(req.Path),
		IncludeSections: true,
		Limit:           maxInt(limit, 2),
	})
	if err != nil {
		return nil, diagnosticsMCPError(err)
	}
	if len(snapshots) == 0 {
		return DocDiffResponse{DocPath: strings.TrimSpace(req.Path), Diagnostics: append(notes, "no_snapshot")}, nil
	}
	current := snapshots[0]
	var base docindex.DocumentSnapshot
	foundBase := false
	if strings.TrimSpace(req.BaseSnapshotID) != "" {
		base, err = s.store.GetDocSnapshot(ctx, strings.TrimSpace(req.BaseSnapshotID), true)
		if err != nil {
			return nil, diagnosticsMCPError(err)
		}
		foundBase = true
	} else if len(snapshots) > 1 {
		base = snapshots[1]
		foundBase = true
	}
	resp := DocDiffResponse{
		DocPath:           current.Path,
		CurrentSnapshotID: current.ID,
		CurrentHash:       current.ContentHash,
		Diagnostics:       notes,
	}
	if !foundBase {
		resp.Diagnostics = append(resp.Diagnostics, "no_baseline_snapshot")
		return resp, nil
	}
	resp.BaseSnapshotID = base.ID
	resp.BaseHash = base.ContentHash
	resp.DocChanged = current.ContentHash != base.ContentHash
	resp.ChangedSections = diffDocSections(current, base, limit)
	return resp, nil
}

func traceDiagnostic(trace retrieval.TraceRecord) RetrievalTraceDiagnostic {
	return RetrievalTraceDiagnostic{
		TraceID:        trace.ID,
		SessionID:      trace.SessionID,
		TaskID:         trace.TaskID,
		WorkspaceID:    trace.WorkspaceID,
		ProjectID:      trace.ProjectID,
		RepoID:         trace.RepoID,
		QuerySummary:   trace.Query,
		TaskSummary:    trace.Task,
		Intent:         string(trace.Intent),
		Mode:           string(trace.Mode),
		UsedFTS:        trace.UsedFTS,
		UsedVector:     trace.UsedVector,
		UsedRelation:   trace.UsedRelation,
		UsedCodeIndex:  trace.UsedCodeIndex,
		UsedDocIndex:   trace.UsedDocIndex,
		FallbackReason: trace.FallbackReason,
		CandidateCount: trace.CandidateCount,
		InjectedCount:  trace.InjectedCount,
		LatencyMS:      trace.LatencyMS,
		Status:         string(trace.Status),
		CreatedAt:      trace.CreatedAt,
	}
}

func accessLogDiagnostic(record retrieval.AccessLogRecord) AccessLogDiagnostic {
	return AccessLogDiagnostic{
		AccessLogID:      record.ID,
		MemoryID:         record.MemoryID,
		SessionID:        record.SessionID,
		TaskID:           record.TaskID,
		RetrievalTraceID: record.RetrievalTraceID,
		EventType:        record.EventType,
		EventWeight:      record.EventWeight,
		SourceType:       record.SourceType,
		SourceQuality:    record.SourceQuality,
		QuerySummary:     record.Query,
		Rank:             record.Rank,
		Score:            record.Score,
		ScoreBreakdown:   record.ScoreBreakdown,
		InclusionReasons: record.InclusionReasons,
		UsedInContext:    record.UsedInContext,
		FeedbackSummary:  record.Feedback,
		CreatedAt:        record.CreatedAt,
	}
}

func diffDocSections(current, base docindex.DocumentSnapshot, limit int) []DocSectionDiffDiagnostic {
	if limit <= 0 || limit > diagnosticsLimitMax {
		limit = diagnosticsLimitMax
	}
	baseByID := make(map[string]docindex.DocumentSection, len(base.Sections))
	for _, section := range base.Sections {
		baseByID[section.SectionID] = section
	}
	currentByID := make(map[string]docindex.DocumentSection, len(current.Sections))
	changes := make([]DocSectionDiffDiagnostic, 0)
	for _, section := range current.Sections {
		currentByID[section.SectionID] = section
		old, ok := baseByID[section.SectionID]
		if !ok {
			changes = append(changes, sectionDiff(section, docindex.DocumentSection{}, "added"))
		} else if old.ContentHash != section.ContentHash {
			changes = append(changes, sectionDiff(section, old, "modified"))
		}
		if len(changes) >= limit {
			return changes
		}
	}
	for _, section := range base.Sections {
		if _, ok := currentByID[section.SectionID]; ok {
			continue
		}
		changes = append(changes, sectionDiff(docindex.DocumentSection{}, section, "removed"))
		if len(changes) >= limit {
			return changes
		}
	}
	return changes
}

func sectionDiff(current, base docindex.DocumentSection, changeType string) DocSectionDiffDiagnostic {
	section := current
	if section.SectionID == "" {
		section = base
	}
	return DocSectionDiffDiagnostic{
		SectionID:   section.SectionID,
		HeadingPath: append([]string(nil), section.HeadingPath...),
		StartLine:   section.StartLine,
		EndLine:     section.EndLine,
		ChangeType:  changeType,
		CurrentHash: current.ContentHash,
		BaseHash:    base.ContentHash,
	}
}

func normalizeDiagnosticsLimit(limit int) (int, []string) {
	if limit <= 0 {
		return diagnosticsLimitMax, nil
	}
	if limit > diagnosticsLimitMax {
		return diagnosticsLimitMax, []string{"limit_truncated"}
	}
	return limit, nil
}

func diagnosticsValidationError(message string) *mcp.Error {
	return &mcp.Error{ErrorCode: "VALIDATION_FAILED", Message: message, Retryable: false}
}

func diagnosticsMCPError(err error) *mcp.Error {
	message := err.Error()
	code := "INTERNAL_ERROR"
	retryable := true
	if i := strings.Index(message, ":"); i > 0 {
		prefix := message[:i]
		switch prefix {
		case "VALIDATION_FAILED", "RETRIEVAL_TRACE_NOT_FOUND", "CODE_REF_NOT_FOUND", "DOC_SNAPSHOT_NOT_FOUND":
			code = prefix
			retryable = false
		case "STORAGE_BUSY":
			code = prefix
			retryable = true
		}
	}
	return &mcp.Error{ErrorCode: code, Message: message, Retryable: retryable, FallbackHint: "check diagnostics query scope and identifiers"}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
