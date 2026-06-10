package tools

import "github.com/zaneway/theone/internal/mcp"

// memoryRememberSpec 构建 "memory.remember" 工具规格。
// 该工具用于将一条高价值记忆写入持久化存储。设计思路是：
//   - 记忆不是简单的文本存档，而是带有结构化元数据的事实单元，包含作用域(scope)、
//     类型(memory_type)、置信度(confidence)、重要性(importance)等维度，方便后续检索和生命周期管理。
//   - 通过 scope 字段实现多级可见性隔离（用户全局 / 项目级 / 仓库级 / 会话级），
//     确保不同上下文的记忆不会互相污染。
//   - 支持 evidence（证据链）和 review_checkpoint（审查检查点），使记忆具备可溯源性和可审核性。
//   - IdempotentHint 为 true，表明相同 content_hash 的重复写入应当去重，避免记忆膨胀。
//
// 参数:
//   - handler: MCP 调用处理器，实际执行记忆的持久化写入逻辑。
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

// memorySearchSpec 构建 "memory.search" 工具规格。
// 该工具用于根据查询文本和多维过滤条件搜索相关记忆。设计思路是：
//   - 以 query 为唯一必填参数，采用"语义检索 + 结构化过滤"的混合模式：
//     query 负责语义匹配，其余参数（scope、memory_types、workspace 等）负责缩小搜索范围。
//   - 支持 include_archived 控制是否检索已归档记忆，实现"软删除"与"彻底遗忘"的分离。
//   - 支持 include_evidence 和 include_code_refs 按需返回证据和代码引用，减少不必要的序列化开销。
//   - ReadOnlyHint 为 true，标记为只读操作，不修改任何状态。
//
// 参数:
//   - handler: MCP 调用处理器，实际执行检索逻辑（通常涉及向量相似度匹配 + 过滤）。
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

// memoryContextSpec 构建 "memory.context" 工具规格。
// 该工具用于为当前任务构建一个紧凑的上下文包（context pack），将相关记忆、约束、
// 代码引用等打包后注入到 Agent 的 prompt 中。设计思路是：
//   - 这是记忆系统的"出站"接口——不是给人看的，而是给 Agent 消费的。
//     通过 token_budget 参数控制返回内容的大小，避免超出模型上下文窗口。
//   - 区分 agent_type（codex / claude_code / cursor），因为不同 Agent 的上下文格式
//     和消费能力不同，需要针对性地裁剪和组织记忆。
//   - 支持 include_code_refs 和 include_evidence_summary 的按需开关，让调用方
//     精确控制上下文包的信息密度。
//
// 参数:
//   - handler: MCP 调用处理器，执行记忆检索、排序、裁剪和打包逻辑。
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

// memoryReviewSpec 构建 "memory.review" 工具规格。
// 该工具用于管理记忆的审核生命周期，支持列表查询、批准、拒绝、编辑、归档和删除操作。
// 设计思路是：
//   - 采用单一工具 + action 枚举的模式，将记忆治理的所有操作收敛到一个接口，
//     避免工具数量膨胀，同时保持语义清晰。
//   - DestructiveHint 为 true，因为 approve（确认写入）、delete（永久删除）
//     等操作具有不可逆性，需要调用方谨慎操作。
//   - 支持 reviewer 和 feedback 字段，建立记忆审核的审计追踪能力。
//   - 通过 state 过滤器可以查看处于不同生命周期阶段的记忆（如待审核、已批准等）。
//
// 参数:
//   - handler: MCP 调用处理器，执行审核操作的业务逻辑。
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

// captureObserveSpec 构建 "memory.observe" 工具规格。
// 该工具用于捕获 Agent 的事件到原始事件存储（raw_event），是事件采集管道的入口。
// 设计思路是：
//   - 核心原则是"事实层有界采集"：摘要（summary）、关键词（keywords）和显著片段（salient_spans）
//     用于检索，raw_payload_json 可保存有界原始事实并用 redaction/truncation 元数据描述处理状态。
//   - event_type 枚举覆盖了 Agent 交互的完整生命周期：会话开始/结束、任务开始/结果、
//     对话消息、工具调用/结果、文件编辑、用户纠正/声明、Agent 决策等。
//   - source_channel 区分事件来源（Agent 会话 / MCP 工具调用 / 手动 CLI），
//     便于后续按来源做质量分析和采集能力评估。
//   - content_hash 支持幂等去重，防止重复采集；sensitivity 和 retention_hint
//     为下游的存储策略和保留策略提供决策依据。
//
// 参数:
//   - handler: MCP 调用处理器，执行事件的有界事实采集和持久化存储。
func captureObserveSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.observe",
		Title:       "Observe agent event",
		Description: "Capture a bounded agent event into raw_event storage with summaries plus optional raw_payload_json metadata for replayable evidence extraction.",
		InputSchema: mcp.ObjectSchema([]string{"event_type"}, map[string]any{
			"session_id":           mcp.StringProp("Agent session id."),
			"task_id":              mcp.StringProp("Agent task id."),
			"agent_type":           mcp.EnumStringProp("Agent type.", "codex", "claude_code", "cursor", "unknown"),
			"workspace_id":         mcp.StringProp("Workspace id."),
			"project_id":           mcp.StringProp("Project id."),
			"repo_id":              mcp.StringProp("Repository id."),
			"event_type":           mcp.EnumStringProp("Captured event type.", "session.start", "session.end", "task.start", "task.result", "conversation.message", "agent.response.summary", "turn.completed", "tool.call", "tool.result.summary", "file.edit.summary", "user.correction", "user.declaration", "agent.decision"),
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
			"raw_payload_json":     mcp.StringProp("Optional bounded raw or near-raw event payload JSON for replayable evidence extraction."),
			"payload_schema":       mcp.StringProp("Schema/version for raw_payload_json, such as turn.completed.v1 or tool_result.v1."),
			"raw_payload_hash":     mcp.StringProp("SHA256 hash of raw_payload_json. If empty, server computes it."),
			"redaction_state":      mcp.EnumStringProp("Raw payload redaction state.", "raw", "redacted", "minimized"),
			"redaction_policy":     mcp.StringProp("Optional redaction policy/version applied before capture."),
			"truncation":           mcp.ObjectProp("Raw payload truncation metadata: truncated, original_size_bytes, stored_size_bytes, max_size_bytes, reason."),
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

// captureSessionsSpec 构建 "memory.capture.sessions" 工具规格。
// 该工具用于查询已捕获的 Agent 会话列表，是事件采集管道的诊断入口之一。
// 设计思路是：
//   - 提供按 workspace / project / repo / agent_type / status 多维过滤能力，
//     方便运维和调试时快速定位特定会话。
//   - status 枚举（active / completed / failed / interrupted / unknown）
//     覆盖了会话的所有终止态，可用于异常会话的排查。
//   - 复用 diagnosticListSpec 通用构造器，保持诊断类工具的统一风格。
//
// 参数:
//   - handler: MCP 调用处理器，执行会话列表的查询逻辑。
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

// captureTasksSpec 构建 "memory.capture.tasks" 工具规格。
// 该工具用于查询已捕获的 Agent 任务列表。设计思路是：
//   - 任务是会话内的子单元，通过 session_id 可以级联查看某个会话下的所有任务。
//   - status 枚举（active / succeeded / failed / interrupted / unknown）
//     与会话状态对齐，保持生命周期语义的一致性。
//   - 同样复用 diagnosticListSpec 构造器。
//
// 参数:
//   - handler: MCP 调用处理器，执行任务列表的查询逻辑。
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

// captureEventsSpec 构建 "memory.capture.events" 工具规格。
// 该工具用于查询已捕获的最小化原始事件列表。设计思路是：
//   - 支持按 session / task / workspace / agent_type / source_channel / event_type
//     等多维过滤，是事件采集管道中最灵活的查询入口。
//   - 同时支持 event_type（单个）和 event_types（多个）过滤，兼顾简单查询和批量过滤场景。
//   - 复用 diagnosticListSpec 构造器，保持只读诊断工具的统一行为。
//
// 参数:
//   - handler: MCP 调用处理器，执行事件列表的查询逻辑。
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

// captureQualitySpec 构建 "memory.capture.quality" 工具规格。
// 该工具用于获取指定会话的采集能力和质量报告。设计思路是：
//   - 采集质量直接影响记忆生成的可靠性——如果上游事件采集不完整（比如缺少工具输出），
//     生成的记忆质量也会下降。此工具让运维人员能够评估采集管道的健康度。
//   - 以 session_id 为唯一参数，返回该会话的采集能力声明和实际质量指标。
//
// 参数:
//   - handler: MCP 调用处理器，执行采集质量评估逻辑。
func captureQualitySpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticSpecWithRequired("memory.capture.quality", "Get capture quality", "Get capture capability and quality report for one captured session.", handler, []string{"session_id"}, map[string]any{
		"session_id": mcp.StringProp("Session id."),
	})
}

// automationListJobsSpec 构建 "memory.jobs.list" 工具规格。
// 该工具用于查询异步自动化任务（automation job）列表。设计思路是：
//   - 记忆系统中的"自动化任务"是指由事件驱动的后台作业，例如从原始事件生成记忆候选、
//     执行保留策略清理等。此工具提供对这些后台作业的可观测性。
//   - 支持按 status / job_type / target_type / target_id / workspace / project / repo
//     多维过滤，方便定位特定作业或排查失败作业。
//   - 复用 diagnosticListSpec 构造器。
//
// 参数:
//   - handler: MCP 调用处理器，执行作业列表的查询逻辑。
func automationListJobsSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.jobs.list", "List automation jobs", "List async automation jobs by status, type, target, and scope. Provide workspace_id for scoped listing or target_id for a specific target.", handler, map[string]any{
		"status":       mcp.EnumStringProp("Job status filter.", "pending", "running", "succeeded", "failed", "cancelled"),
		"job_type":     mcp.StringProp("Job type filter."),
		"target_type":  mcp.StringProp("Target type filter."),
		"target_id":    mcp.StringProp("Target id filter. Required when workspace_id is omitted."),
		"workspace_id": mcp.StringProp("Workspace id filter."),
		"project_id":   mcp.StringProp("Project id filter."),
		"repo_id":      mcp.StringProp("Repository id filter."),
		"limit":        mcp.IntegerProp("Maximum result count."),
	})
}

// automationGetJobSpec 构建 "memory.jobs.get" 工具规格。
// 该工具用于获取单个异步自动化任务的诊断详情。设计思路是：
//   - 与 automationListJobsSpec 配合使用——列表工具用于发现，此工具用于深入查看
//     特定作业的详细诊断信息（如输入参数、执行状态、错误信息等）。
//   - 以 job_id 为唯一参数，精确定位单条记录。
//
// 参数:
//   - handler: MCP 调用处理器，执行单条作业的查询逻辑。
func automationGetJobSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticSpecWithRequired("memory.jobs.get", "Get automation job", "Get one async automation job diagnostic record by job_id.", handler, []string{"job_id"}, map[string]any{
		"job_id": mcp.StringProp("Async job id."),
	})
}

// automationListCandidatesSpec 构建 "memory.candidates.list" 工具规格。
// 该工具用于查询由自动化流程生成的记忆候选（memory candidate）列表。设计思路是：
//   - 记忆候选是自动化管道的中间产物：原始事件 → 记忆候选 → 审核 → 正式记忆。
//     此工具提供对候选阶段的可观测性，方便评估生成质量和审核流程。
//   - status 枚举（generated / admitted / dropped / merged / failed）
//     覆盖了候选的完整生命周期，支持按状态筛选以查看不同阶段的候选。
//   - 支持按 provider（生成提供者）和原始事件/证据 ID 追溯，便于问题定位。
//
// 参数:
//   - handler: MCP 调用处理器，执行候选列表的查询逻辑。
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

// automationGetCandidateSpec 构建 "memory.candidates.get" 工具规格。
// 该工具用于获取单个记忆候选的诊断详情。设计思路是：
//   - 与 automationListCandidatesSpec 配合——列表发现，此工具深入查看。
//   - 以 candidate_id 为唯一参数，返回候选的完整诊断信息。
//
// 参数:
//   - handler: MCP 调用处理器，执行单条候选的查询逻辑。
func automationGetCandidateSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticSpecWithRequired("memory.candidates.get", "Get memory candidate", "Get one generated memory candidate diagnostic record by candidate_id.", handler, []string{"candidate_id"}, map[string]any{
		"candidate_id": mcp.StringProp("Memory candidate id."),
	})
}

// automationStatusSpec 构建 "memory.automation.status" 工具规格。
// 该工具用于获取自动化工作器和队列的健康状态摘要。设计思路是：
//   - 提供全局视角的自动化管道健康度概览，无需指定具体作业或候选。
//   - 无输入参数，返回工作器运行状态、队列深度、错误率等关键指标。
//   - 适用于健康检查和监控告警场景。
//
// 参数:
//   - handler: MCP 调用处理器，执行自动化状态聚合查询。
func automationStatusSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticListSpec("memory.automation.status", "Get automation status", "Get automation worker and queue health summary.", handler, map[string]any{})
}

// automationReconcileSpec 构建 "memory.jobs.reconcile" 工具规格。
// 该工具用于修复"孤儿原始事件"——即已采集但未被任何自动化任务处理的原始事件。
// 设计思路是：
//   - 在分布式或异步处理场景中，可能因服务重启、任务失败等原因导致部分原始事件
//     没有对应的自动化作业。此工具扫描这些"孤儿"事件并可选择性地补发作业。
//   - dry_run 参数支持"只看不做"模式，先预览需要修复的范围，确认后再执行。
//   - mode 枚举目前仅支持 orphan_raw_event，未来可扩展其他修复模式。
//   - IdempotentHint 为 true，重复执行不会产生副作用。
//
// 参数:
//   - handler: MCP 调用处理器，执行孤儿事件扫描和作业补发逻辑。
func automationReconcileSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.jobs.reconcile",
		Title:       "Reconcile automation jobs",
		Description: "Find orphan raw events and optionally enqueue missing automation jobs; use dry_run=true to inspect before modifying the queue.",
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

// retentionRunSpec 构建 "memory.retention.run" 工具规格。
// 该工具用于手动触发记忆保留策略的维护作业。设计思路是：
//   - 记忆保留策略负责两个关键任务：清理过期的临时记忆（cleanup_temporary）
//     和重新计算保留分数（recompute_scores），后者影响记忆在检索中的排序权重。
//   - DestructiveHint 为 true，因为 cleanup_temporary 模式会归档过期记忆。
//   - dry_run 支持预览模式，先查看哪些记忆会被清理/重算，确认后再执行。
//   - 正常情况下保留策略由自动化调度器定期执行，此工具用于紧急手动干预。
//
// 参数:
//   - handler: MCP 调用处理器，执行保留策略的维护逻辑。
func retentionRunSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.retention.run",
		Title:       "Run retention job",
		Description: "Manually run retention maintenance: archive expired temporary memories or recompute retention scores; use dry_run=true to preview.",
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

// mvpStartRunSpec 构建 "memory.mvp.run.start" 工具规格。
// 该工具用于启动一次 MVP 验收测试（acceptance run），用于评估记忆系统的效果。
// 设计思路是：
//   - MVP 验收采用"对比实验"模式：baseline（无记忆 / 完整聊天历史 / 仅摘要）
//     vs candidate（混合记忆），在相同场景下对比 Agent 的表现差异。
//   - mode 枚举支持 synthetic（合成场景）、real_agent（真实 Agent 测试）、
//     mixed（混合），覆盖从快速验证到端到端测试的不同阶段。
//   - 通过 baseline_type 和 candidate_type 明确实验组和对照组，确保评估的科学性。
//
// 参数:
//   - handler: MCP 调用处理器，创建验收测试运行并初始化实验配置。
func mvpStartRunSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.mvp.run.start",
		Title:       "Start MVP run",
		Description: "Start an MVP acceptance run for synthetic, real_agent, or mixed validation.",
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

// mvpRecordTaskSpec 构建 "memory.mvp.task.record" 工具规格。
// 该工具用于记录一次 MVP 场景任务的执行结果。设计思路是：
//   - 每次 MVP 运行包含多个场景（scenario），每个场景包含多个任务。
//     此工具记录单个任务的执行结果，包括期望值（expected）和实际值（observed）。
//   - baseline 字段标记该任务属于对照组还是实验组，便于后续按组对比分析。
//   - 支持 retrieval_trace_id 关联检索追踪，可以分析记忆检索对任务结果的影响。
//   - failure_reason 提供结构化的失败原因，支持聚合统计失败模式。
//
// 参数:
//   - handler: MCP 调用处理器，将任务结果持久化到验收测试存储。
func mvpRecordTaskSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.mvp.task.record",
		Title:       "Record MVP task",
		Description: "Record one MVP scenario task result.",
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

// mvpRecordCapabilitySpec 构建 "memory.mvp.capability.record" 工具规格。
// 该工具用于记录真实 Agent 的能力快照（capability snapshot），用于 Agent 认证。
// 设计思路是：
//   - 不同 Agent（Codex / Claude Code / Cursor）的事件采集能力不同：
//     有的能捕获对话摘要，有的能捕获工具输出，有的能捕获文件编辑。
//     此工具记录每个 Agent 的实际采集能力等级（capture_level 1..4）。
//   - capture_level 逐级递增：1=基础会话生命周期，2=对话消息，3=工具调用，
//     4=工具输出和文件编辑。等级越高，生成的记忆质量越好。
//   - completeness 分数和 degradation_reasons 量化了能力缺失对记忆质量的影响。
//
// 参数:
//   - handler: MCP 调用处理器，将 Agent 能力快照持久化。
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

// mvpComputeMetricsSpec 构建 "memory.mvp.metrics.compute" 工具规格。
// 该工具用于计算或重新计算一次 MVP 运行的评估指标。设计思路是：
//   - 在所有场景任务结果记录完成后，调用此工具汇总计算指标（如成功率、
//     平均完成时间、记忆命中率等）。
//   - recompute 参数支持指标重算，当发现计算逻辑有误或需要调整指标定义时，
//     可以重新计算而不必重跑整个实验。
//   - IdempotentHint 为 true，相同输入的重复计算结果一致。
//
// 参数:
//   - handler: MCP 调用处理器，执行指标聚合计算逻辑。
func mvpComputeMetricsSpec(handler mcp.Handler) mcp.ToolSpec {
	return mcp.ToolSpec{
		Name:        "memory.mvp.metrics.compute",
		Title:       "Compute MVP metrics",
		Description: "Compute or recompute MVP metrics for a run.",
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

// mvpReportSpec 构建 "memory.mvp.report" 工具规格。
// 该工具用于生成 MVP 验收测试的最终报告。设计思路是：
//   - 支持 markdown 和 json 两种输出格式：markdown 适合人类阅读和分享，
//     json 适合程序化处理和 CI/CD 集成。
//   - include_failures 控制是否在报告中包含失败详情，便于深度分析。
//   - 复用 diagnosticListSpec 构造器，保持只读查询工具的统一风格。
//
// 参数:
//   - handler: MCP 调用处理器，执行报告生成逻辑。
func mvpReportSpec(handler mcp.Handler) mcp.ToolSpec {
	return diagnosticSpecWithRequired("memory.mvp.report", "Generate MVP report", "Generate an in-memory MVP report for a run by run_id.", handler, []string{"run_id"}, map[string]any{
		"run_id":           mcp.StringProp("MVP run id."),
		"format":           mcp.EnumStringProp("Report format.", "markdown", "json"),
		"include_failures": mcp.BooleanProp("Whether failure details should be included."),
	})
}

// diagnosticListSpec 是诊断类只读工具的通用构造器。
// 设计思路是：
//   - 大量诊断查询工具（如 captureSessions、captureTasks、automationListJobs 等）
//     具有相同的结构：名称 + 标题 + 描述 + 过滤属性 + 只读 + 不访问外部世界。
//     此构造器将这些共同模式抽取出来，避免重复代码。
//   - 所有通过此构造器创建的工具都标记为 ReadOnlyHint=true 和 OpenWorldHint=false，
//     表明它们是安全的只读查询操作，不会产生副作用也不会访问外部资源。
//   - 默认不声明 required 字段，表示诊断列表工具的输入参数都是可选过滤条件。
//
// 参数:
//   - name: 工具的唯一标识名称，采用 "memory.xxx.yyy" 的命名空间格式。
//   - title: 工具的简短显示标题。
//   - description: 工具的详细功能描述。
//   - handler: MCP 调用处理器，执行实际的查询逻辑。
//   - properties: 输入参数的 JSON Schema 属性定义，均为可选的过滤条件。
func diagnosticListSpec(name, title, description string, handler mcp.Handler, properties map[string]any) mcp.ToolSpec {
	return diagnosticSpecWithRequired(name, title, description, handler, nil, properties)
}

// diagnosticSpecWithRequired 构建带 required 字段的只读诊断工具规格。
// 用于 get/report/quality 这类只读但必须提供目标 ID 的工具。
func diagnosticSpecWithRequired(name, title, description string, handler mcp.Handler, required []string, properties map[string]any) mcp.ToolSpec {
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
