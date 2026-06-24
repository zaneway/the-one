package diagnostics

import "github.com/zaneway/theone/internal/mcp"

func healthSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:          "memory.health",
		Title:         "Health check",
		Description:   "Check theone runtime health, SQLite reachability, and configured external AI availability.",
		InputSchema:   mcp.ObjectSchema(nil, map[string]any{}),
		OutputSchema:  mcp.RawObjectSchema(),
		ReadOnlyHint:  true,
		OpenWorldHint: mcp.BoolPtr(true),
		Handler:       handler,
	}
}

func statusSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.status",
		Title:       "Runtime status",
		Description: "Return storage, migration, code index, embedding, vector, and optional non-sensitive config status.",
		InputSchema: mcp.ObjectSchema(nil, map[string]any{
			"include_config": mcp.BooleanProp("Whether to include non-sensitive configuration summary."),
		}),
		OutputSchema:  mcp.RawObjectSchema(),
		ReadOnlyHint:  true,
		OpenWorldHint: mcp.BoolPtr(false),
		Handler:       handler,
	}
}

func retrievalTracesSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticSpec("memory.retrieval.traces", "List retrieval traces", "List retrieval trace diagnostics for a workspace and optional project, repo, session, task, or status filter.", handler, []string{"workspace_id"}, map[string]any{
		"workspace_id": mcp.StringProp("Workspace id."),
		"project_id":   mcp.StringProp("Project id filter."),
		"repo_id":      mcp.StringProp("Repository id filter."),
		"session_id":   mcp.StringProp("Session id filter."),
		"task_id":      mcp.StringProp("Task id filter."),
		"status":       mcp.StringProp("Trace status filter."),
		"limit":        mcp.IntegerProp("Maximum result count."),
	})
}

func retrievalAccessLogsSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticSpec("memory.retrieval.access_logs", "List retrieval access logs", "List memory access logs by retrieval_trace_id or memory_id for retrieval diagnostics.", handler, nil, map[string]any{
		"retrieval_trace_id": mcp.StringProp("Retrieval trace id filter."),
		"memory_id":          mcp.StringProp("Memory id filter."),
		"event_type":         mcp.StringProp("Access event type filter."),
		"limit":              mcp.IntegerProp("Maximum result count."),
	})
}

func codeRefsSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticSpec("memory.code_refs", "List code references", "List code reference diagnostics by memory, repo, file path, symbol, and resolve status without returning source code.", handler, nil, map[string]any{
		"memory_id":      mcp.StringProp("Memory id filter."),
		"repo_id":        mcp.StringProp("Repository id filter."),
		"file_path":      mcp.StringProp("Repository-relative file path filter."),
		"symbol":         mcp.StringProp("Symbol filter."),
		"resolve_status": mcp.EnumStringProp("Resolve status filter.", "unresolved", "resolved", "stale", "missing", "ambiguous"),
		"limit":          mcp.IntegerProp("Maximum result count."),
	})
}

func docSnapshotsSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticSpec("memory.docindex.snapshots", "List document snapshots", "List Markdown document index snapshots and optional section metadata without returning full document bodies.", handler, []string{"workspace_id", "doc_path"}, map[string]any{
		"workspace_id":     mcp.StringProp("Workspace id."),
		"project_id":       mcp.StringProp("Project id filter."),
		"repo_id":          mcp.StringProp("Repository id filter."),
		"doc_path":         mcp.StringProp("Document path."),
		"content_hash":     mcp.StringProp("Content hash filter."),
		"include_sections": mcp.BooleanProp("Whether section metadata should be returned."),
		"limit":            mcp.IntegerProp("Maximum result count."),
	})
}

func docDiffSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticSpec("memory.docindex.diff", "Diff document snapshots", "Compare current and baseline document snapshots and return changed section diagnostics.", handler, []string{"workspace_id", "doc_path"}, map[string]any{
		"workspace_id":     mcp.StringProp("Workspace id."),
		"project_id":       mcp.StringProp("Project id filter."),
		"repo_id":          mcp.StringProp("Repository id filter."),
		"doc_path":         mcp.StringProp("Document path."),
		"base_snapshot_id": mcp.StringProp("Optional explicit baseline snapshot id."),
		"limit":            mcp.IntegerProp("Maximum changed section count."),
	})
}

func diagnosticSpec(name, title, description string, handler mcp.Handler, required []string, properties map[string]any) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:          name,
		Title:         title,
		Description:   description,
		InputSchema:   mcp.ObjectSchema(required, properties),
		OutputSchema:  mcp.RawObjectSchema(),
		ReadOnlyHint:  true,
		OpenWorldHint: mcp.BoolPtr(false),
		Handler:       handler,
	}
}
