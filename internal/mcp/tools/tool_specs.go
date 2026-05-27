package tools

import "github.com/zaneway/theone/internal/mcp"

func memoryRememberSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.remember",
		Title:       "Remember memory",
		Description: "Write a high-value memory item with scope, type, retrieval cues, and optional evidence or review checkpoint.",
		InputSchema: mcp.ObjectSchema([]string{"content", "memory_type", "scope"}, map[string]any{
			"content":           mcp.StringProp("Memory content. Store concise interpreted facts, decisions, constraints, preferences, procedures, failures, or review checkpoints."),
			"title":             mcp.StringProp("Optional display title for the memory."),
			"memory_type":       mcp.EnumStringProp("Memory type.", "preference", "requirement", "decision", "constraint", "assumption", "open_issue", "failure", "project_fact", "procedure", "temporary_state", "session_summary", "review_checkpoint"),
			"scope":             mcp.EnumStringProp("Memory visibility scope.", "user_global", "project_local", "repo_local", "session"),
			"workspace_id":      mcp.StringProp("Workspace id. Required for project_local, repo_local, and session scopes."),
			"user_id":           mcp.StringProp("User id. Required for user_global scope when not using default local user."),
			"project_id":        mcp.StringProp("Project id. Required for project_local scope."),
			"repo_id":           mcp.StringProp("Repository id. Required for repo_local scope."),
			"session_id":        mcp.StringProp("Session id. Required for session scope."),
			"task_id":           mcp.StringProp("Optional task id for attribution."),
			"source_type":       mcp.StringProp("Source type such as user_declared, user_confirmed, manual_review, or agent_summary."),
			"importance":        mcp.NumberProp("Importance score in range 0..1."),
			"confidence":        mcp.NumberProp("Confidence score in range 0..1."),
			"pinned":            mcp.BooleanProp("Whether the memory should be pinned and durable."),
			"tags":              mcp.StringArrayProp("Classification tags."),
			"keywords":          mcp.StringArrayProp("Keywords for full-text retrieval."),
			"entities":          mcp.StringArrayProp("Mentioned entities."),
			"retrieval_cues":    mcp.StringArrayProp("Additional retrieval cues."),
			"review_checkpoint": mcp.ObjectProp("Structured review checkpoint when memory_type is review_checkpoint."),
			"evidence":          mcp.ObjectProp("Supporting evidence with interpreted_statement, keywords, salient_spans, and source_ref."),
		}),
		OutputSchema:    mcp.RawObjectSchema(),
		ReadOnlyHint:    false,
		DestructiveHint: mcp.BoolPtr(false),
		IdempotentHint:  true,
		OpenWorldHint:   mcp.BoolPtr(false),
		Handler:         handler,
	}
}

func memorySearchSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.search",
		Title:       "Search memory",
		Description: "Search relevant memories by query, scope, memory type, workspace, project, repo, and session filters.",
		InputSchema: mcp.ObjectSchema([]string{"query"}, map[string]any{
			"query":             mcp.StringProp("Search query or task summary."),
			"workspace_id":      mcp.StringProp("Workspace id filter."),
			"project_id":        mcp.StringProp("Project id filter."),
			"repo_id":           mcp.StringProp("Repository id filter."),
			"session_id":        mcp.StringProp("Session id filter."),
			"scope":             mcp.StringArrayProp("Scope filters such as project_local or user_global."),
			"memory_types":      mcp.StringArrayProp("Memory type filters such as decision, constraint, or failure."),
			"limit":             mcp.IntegerProp("Maximum result count."),
			"include_archived":  mcp.BooleanProp("Whether archived memories should be included."),
			"include_evidence":  mcp.BooleanProp("Whether evidence references should be included."),
			"include_code_refs": mcp.BooleanProp("Whether code references should be included."),
		}),
		OutputSchema:  mcp.RawObjectSchema(),
		ReadOnlyHint:  true,
		OpenWorldHint: mcp.BoolPtr(false),
		Handler:       handler,
	}
}

func memoryContextSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.context",
		Title:       "Build memory context",
		Description: "Build a compact memory context pack for the current task, including constraints, selected memories, optional code refs, and diagnostics.",
		InputSchema: mcp.ObjectSchema([]string{"task"}, map[string]any{
			"task":                     mcp.StringProp("Current task description."),
			"workspace_id":             mcp.StringProp("Workspace id filter."),
			"project_id":               mcp.StringProp("Project id filter."),
			"repo_id":                  mcp.StringProp("Repository id filter."),
			"session_id":               mcp.StringProp("Session id filter."),
			"agent_type":               mcp.EnumStringProp("Agent type.", "codex", "claude_code", "cursor", "unknown"),
			"token_budget":             mcp.IntegerProp("Approximate token budget for returned context."),
			"include_code_refs":        mcp.BooleanProp("Whether code references should be included."),
			"include_evidence_summary": mcp.BooleanProp("Whether evidence summaries should be included."),
		}),
		OutputSchema:  mcp.RawObjectSchema(),
		ReadOnlyHint:  true,
		OpenWorldHint: mcp.BoolPtr(false),
		Handler:       handler,
	}
}

func memoryReviewSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.review",
		Title:       "Review memory",
		Description: "List, approve, reject, edit, archive, or delete memories in the review lifecycle.",
		InputSchema: mcp.ObjectSchema([]string{"action"}, map[string]any{
			"action":       mcp.EnumStringProp("Review action.", "list", "approve", "reject", "edit", "archive", "delete"),
			"workspace_id": mcp.StringProp("Workspace id filter for list."),
			"project_id":   mcp.StringProp("Project id filter for list."),
			"repo_id":      mcp.StringProp("Repository id filter for list."),
			"state":        mcp.StringProp("Memory state filter for list."),
			"limit":        mcp.IntegerProp("Maximum result count for list."),
			"memory_id":    mcp.StringProp("Target memory id for approve, reject, edit, archive, or delete."),
			"edit_content": mcp.StringProp("Replacement content for edit action."),
			"feedback":     mcp.StringProp("Reviewer feedback."),
			"reviewer":     mcp.StringProp("Reviewer identifier."),
		}),
		OutputSchema:    mcp.RawObjectSchema(),
		ReadOnlyHint:    false,
		DestructiveHint: mcp.BoolPtr(true),
		IdempotentHint:  false,
		OpenWorldHint:   mcp.BoolPtr(false),
		Handler:         handler,
	}
}

func captureObserveSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.observe",
		Title:       "Observe agent event",
		Description: "Capture a minimized agent event into raw_event storage without full prompts, full tool output, full diffs, or source code.",
		InputSchema: mcp.ObjectSchema([]string{"event_type"}, map[string]any{
			"session_id":           mcp.StringProp("Agent session id."),
			"task_id":              mcp.StringProp("Agent task id."),
			"agent_type":           mcp.EnumStringProp("Agent type.", "codex", "claude_code", "cursor", "unknown"),
			"workspace_id":         mcp.StringProp("Workspace id."),
			"project_id":           mcp.StringProp("Project id."),
			"repo_id":              mcp.StringProp("Repository id."),
			"event_type":           mcp.EnumStringProp("Captured event type.", "session.start", "session.end", "task.start", "task.result", "conversation.message", "agent.response.summary", "tool.call", "tool.result.summary", "file.edit.summary", "user.correction", "user.declaration", "agent.decision"),
			"source_channel":       mcp.EnumStringProp("Capture source channel.", "agent_session", "mcp_tool", "manual_cli"),
			"occurred_at":          mcp.StringProp("Event occurrence timestamp. If empty, server time is used."),
			"actor":                mcp.EnumStringProp("Event actor.", "user", "agent", "tool", "adapter", "system"),
			"tool_name":            mcp.StringProp("Tool name for tool events."),
			"input_summary":        mcp.StringProp("Minimized tool input summary."),
			"output_summary":       mcp.StringProp("Minimized tool output summary."),
			"content_summary":      mcp.StringProp("Generic minimized content summary."),
			"keywords":             mcp.StringArrayProp("Keywords for downstream retrieval."),
			"salient_spans":        mcp.StringArrayProp("Short salient spans, not full original content."),
			"source_refs":          mcp.ObjectArrayProp("Source references such as hashes, paths, symbols, exit_code, and capture_method."),
			"content_hash":         mcp.StringProp("Content hash for idempotent deduplication."),
			"sensitivity":          mcp.StringProp("Sensitivity label such as normal."),
			"retention_hint":       mcp.StringProp("Retention hint such as short_term or long_term."),
			"capture_capabilities": mcp.ObjectProp("Adapter capture capability declaration."),
			"session":              mcp.ObjectProp("Session lifecycle summary."),
			"task":                 mcp.ObjectProp("Task boundary summary."),
		}),
		OutputSchema:    mcp.RawObjectSchema(),
		ReadOnlyHint:    false,
		DestructiveHint: mcp.BoolPtr(false),
		IdempotentHint:  true,
		OpenWorldHint:   mcp.BoolPtr(false),
		Handler:         handler,
	}
}

func captureSessionsSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.capture.sessions", "List capture sessions", "List captured agent sessions by workspace, project, repo, agent type, and status.", handler, map[string]any{
		"workspace_id": mcp.StringProp("Workspace id filter."),
		"project_id":   mcp.StringProp("Project id filter."),
		"repo_id":      mcp.StringProp("Repository id filter."),
		"agent_type":   mcp.EnumStringProp("Agent type filter.", "codex", "claude_code", "cursor", "unknown"),
		"status":       mcp.EnumStringProp("Session status filter.", "active", "completed", "failed", "interrupted", "unknown"),
		"limit":        mcp.IntegerProp("Maximum result count."),
	})
}

func captureTasksSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.capture.tasks", "List capture tasks", "List captured agent tasks by session, workspace, project, repo, and status.", handler, map[string]any{
		"session_id":   mcp.StringProp("Session id filter."),
		"workspace_id": mcp.StringProp("Workspace id filter."),
		"project_id":   mcp.StringProp("Project id filter."),
		"repo_id":      mcp.StringProp("Repository id filter."),
		"status":       mcp.EnumStringProp("Task status filter.", "active", "succeeded", "failed", "interrupted", "unknown"),
		"limit":        mcp.IntegerProp("Maximum result count."),
	})
}

func captureEventsSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.capture.events", "List captured events", "List minimized raw events by session, task, workspace, agent, source channel, and event type.", handler, map[string]any{
		"session_id":     mcp.StringProp("Session id filter."),
		"task_id":        mcp.StringProp("Task id filter."),
		"workspace_id":   mcp.StringProp("Workspace id filter."),
		"project_id":     mcp.StringProp("Project id filter."),
		"repo_id":        mcp.StringProp("Repository id filter."),
		"agent_type":     mcp.EnumStringProp("Agent type filter.", "codex", "claude_code", "cursor", "unknown"),
		"source_channel": mcp.EnumStringProp("Source channel filter.", "agent_session", "mcp_tool", "manual_cli"),
		"event_type":     mcp.StringProp("Single event type filter."),
		"event_types":    mcp.StringArrayProp("Multiple event type filters."),
		"limit":          mcp.IntegerProp("Maximum result count."),
	})
}

func captureQualitySpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.capture.quality", "Get capture quality", "Get capture capability and quality report for a session.", handler, map[string]any{
		"session_id": mcp.StringProp("Session id."),
	})
}

func automationListJobsSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.jobs.list", "List automation jobs", "List async automation jobs by status, type, target, and scope.", handler, map[string]any{
		"status":       mcp.EnumStringProp("Job status filter.", "pending", "running", "succeeded", "failed", "cancelled"),
		"job_type":     mcp.StringProp("Job type filter."),
		"target_type":  mcp.StringProp("Target type filter."),
		"target_id":    mcp.StringProp("Target id filter."),
		"workspace_id": mcp.StringProp("Workspace id filter."),
		"project_id":   mcp.StringProp("Project id filter."),
		"repo_id":      mcp.StringProp("Repository id filter."),
		"limit":        mcp.IntegerProp("Maximum result count."),
	})
}

func automationGetJobSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.jobs.get", "Get automation job", "Get one async automation job diagnostic record.", handler, map[string]any{
		"job_id": mcp.StringProp("Async job id."),
	})
}

func automationListCandidatesSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.candidates.list", "List memory candidates", "List generated memory candidates by status, memory type, provider, scope, and source ids.", handler, map[string]any{
		"status":       mcp.EnumStringProp("Candidate status filter.", "generated", "admitted", "dropped", "merged", "failed"),
		"memory_type":  mcp.StringProp("Memory type filter."),
		"provider":     mcp.StringProp("Provider filter."),
		"workspace_id": mcp.StringProp("Workspace id filter."),
		"project_id":   mcp.StringProp("Project id filter."),
		"repo_id":      mcp.StringProp("Repository id filter."),
		"raw_event_id": mcp.StringProp("Raw event id filter."),
		"evidence_id":  mcp.StringProp("Evidence id filter."),
		"limit":        mcp.IntegerProp("Maximum result count."),
	})
}

func automationGetCandidateSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.candidates.get", "Get memory candidate", "Get one generated memory candidate diagnostic record.", handler, map[string]any{
		"candidate_id": mcp.StringProp("Memory candidate id."),
	})
}

func automationStatusSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.automation.status", "Get automation status", "Get automation worker and queue health summary.", handler, map[string]any{})
}

func automationReconcileSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.jobs.reconcile",
		Title:       "Reconcile automation jobs",
		Description: "Find orphan raw events and optionally enqueue missing automation jobs.",
		InputSchema: mcp.ObjectSchema([]string{"workspace_id", "mode", "dry_run"}, map[string]any{
			"workspace_id": mcp.StringProp("Workspace id."),
			"project_id":   mcp.StringProp("Project id filter."),
			"repo_id":      mcp.StringProp("Repository id filter."),
			"mode":         mcp.EnumStringProp("Reconcile mode.", "orphan_raw_event"),
			"dry_run":      mcp.BooleanProp("When true, report work without enqueueing jobs."),
			"limit":        mcp.IntegerProp("Maximum raw events to inspect."),
		}),
		OutputSchema:    mcp.RawObjectSchema(),
		ReadOnlyHint:    false,
		DestructiveHint: mcp.BoolPtr(false),
		IdempotentHint:  true,
		OpenWorldHint:   mcp.BoolPtr(false),
		Handler:         handler,
	}
}

func retentionRunSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.retention.run",
		Title:       "Run retention job",
		Description: "Manually run retention maintenance for temporary cleanup or retention score recomputation; supports dry_run.",
		InputSchema: mcp.ObjectSchema([]string{"mode", "dry_run"}, map[string]any{
			"workspace_id": mcp.StringProp("Workspace id filter."),
			"project_id":   mcp.StringProp("Project id filter."),
			"mode":         mcp.EnumStringProp("Retention mode.", "cleanup_temporary", "recompute_scores"),
			"dry_run":      mcp.BooleanProp("When true, report changes without applying them."),
			"limit":        mcp.IntegerProp("Maximum memories to process."),
		}),
		OutputSchema:    mcp.RawObjectSchema(),
		ReadOnlyHint:    false,
		DestructiveHint: mcp.BoolPtr(true),
		IdempotentHint:  false,
		OpenWorldHint:   mcp.BoolPtr(false),
		Handler:         handler,
	}
}

func mvpStartRunSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.mvp.run.start",
		Title:       "Start MVP run",
		Description: "Start a P5 MVP acceptance run for synthetic, real_agent, or mixed validation.",
		InputSchema: mcp.ObjectSchema([]string{"name", "workspace_id"}, map[string]any{
			"name":           mcp.StringProp("Run name."),
			"mode":           mcp.EnumStringProp("Run mode.", "synthetic", "real_agent", "mixed"),
			"workspace_id":   mcp.StringProp("Workspace id."),
			"project_id":     mcp.StringProp("Project id."),
			"repo_id":        mcp.StringProp("Repository id."),
			"baseline_type":  mcp.EnumStringProp("Baseline type.", "no_memory", "full_chat_history", "summary_only"),
			"candidate_type": mcp.EnumStringProp("Candidate type.", "hybrid_memory"),
		}),
		OutputSchema:    mcp.RawObjectSchema(),
		ReadOnlyHint:    false,
		DestructiveHint: mcp.BoolPtr(false),
		IdempotentHint:  false,
		OpenWorldHint:   mcp.BoolPtr(false),
		Handler:         handler,
	}
}

func mvpRecordTaskSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.mvp.task.record",
		Title:       "Record MVP task",
		Description: "Record one P5 MVP scenario task result.",
		InputSchema: mcp.ObjectSchema([]string{"run_id", "scenario_id", "agent_type", "task_success"}, map[string]any{
			"run_id":             mcp.StringProp("MVP run id."),
			"scenario_id":        mcp.StringProp("Scenario id."),
			"round":              mcp.IntegerProp("Round number."),
			"agent_type":         mcp.EnumStringProp("Agent type.", "codex", "claude_code", "cursor"),
			"baseline":           mcp.BooleanProp("Whether this task belongs to baseline execution."),
			"session_id":         mcp.StringProp("Session id."),
			"task_id":            mcp.StringProp("Task id."),
			"retrieval_trace_id": mcp.StringProp("Retrieval trace id."),
			"status":             mcp.EnumStringProp("Task status.", "running", "passed", "failed", "skipped"),
			"task_success":       mcp.BooleanProp("Whether the task succeeded."),
			"expected":           mcp.ObjectProp("Expected result summary."),
			"observed":           mcp.ObjectProp("Observed result summary."),
			"failure_reason":     mcp.StringProp("Failure reason summary."),
		}),
		OutputSchema:    mcp.RawObjectSchema(),
		ReadOnlyHint:    false,
		DestructiveHint: mcp.BoolPtr(false),
		IdempotentHint:  true,
		OpenWorldHint:   mcp.BoolPtr(false),
		Handler:         handler,
	}
}

func mvpRecordCapabilitySpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.mvp.capability.record",
		Title:       "Record MVP capability",
		Description: "Record real Agent capability snapshot for Codex, Claude Code, or Cursor certification.",
		InputSchema: mcp.ObjectSchema([]string{"run_id", "agent_type", "capture_level"}, map[string]any{
			"run_id":               mcp.StringProp("MVP run id."),
			"agent_type":           mcp.EnumStringProp("Agent type.", "codex", "claude_code", "cursor"),
			"adapter_name":         mcp.StringProp("Adapter name."),
			"adapter_version":      mcp.StringProp("Adapter version."),
			"capture_level":        mcp.IntegerProp("Capture level 1..4."),
			"conversation_capture": mcp.BooleanProp("Whether conversation summaries can be captured."),
			"tool_call_capture":    mcp.BooleanProp("Whether tool call events can be captured."),
			"tool_output_capture":  mcp.BooleanProp("Whether tool output summaries can be captured."),
			"file_edit_capture":    mcp.BooleanProp("Whether file edit summaries can be captured."),
			"session_lifecycle":    mcp.BooleanProp("Whether session lifecycle can be captured."),
			"memory_observe":       mcp.BooleanProp("Whether memory.observe can be used."),
			"completeness":         mcp.NumberProp("Completeness score in range 0..1."),
			"degradation_reasons":  mcp.ObjectProp("Structured degradation reasons."),
		}),
		OutputSchema:    mcp.RawObjectSchema(),
		ReadOnlyHint:    false,
		DestructiveHint: mcp.BoolPtr(false),
		IdempotentHint:  true,
		OpenWorldHint:   mcp.BoolPtr(false),
		Handler:         handler,
	}
}

func mvpComputeMetricsSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.mvp.metrics.compute",
		Title:       "Compute MVP metrics",
		Description: "Compute or recompute P5 MVP metrics for a run.",
		InputSchema: mcp.ObjectSchema([]string{"run_id"}, map[string]any{
			"run_id":    mcp.StringProp("MVP run id."),
			"recompute": mcp.BooleanProp("Whether to recompute existing metrics."),
		}),
		OutputSchema:    mcp.RawObjectSchema(),
		ReadOnlyHint:    false,
		DestructiveHint: mcp.BoolPtr(false),
		IdempotentHint:  true,
		OpenWorldHint:   mcp.BoolPtr(false),
		Handler:         handler,
	}
}

func mvpReportSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.mvp.report", "Generate MVP report", "Generate a P5 MVP report for a run.", handler, map[string]any{
		"run_id":           mcp.StringProp("MVP run id."),
		"format":           mcp.EnumStringProp("Report format.", "markdown", "json"),
		"include_failures": mcp.BooleanProp("Whether failure details should be included."),
	})
}

func diagnosticListSpec(name, title, description string, handler mcp.Handler, properties map[string]any) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:          name,
		Title:         title,
		Description:   description,
		InputSchema:   mcp.ObjectSchema(nil, properties),
		OutputSchema:  mcp.RawObjectSchema(),
		ReadOnlyHint:  true,
		OpenWorldHint: mcp.BoolPtr(false),
		Handler:       handler,
	}
}
