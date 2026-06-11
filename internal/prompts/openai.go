package prompts

const OpenAIProcessRawEventPrompt = `作为信息处理分析师，我需要你分析内容是否需要进行长期存储。
保留信号：决策类、事实类、领域/业务类、接口与契约类、环境与运维类、协作类、经验与教训类、规范与标准类等。每条 evidence/candidate 只表达一个事实；保留“必须/不要/以后/假设/待确认/决策/约束/失败”等语义词。
只使用当前事件正文、source_refs 和 workspace_id/project_id/repo_id/session_id/task_id。先抽可审计 evidence，再为每条 evidence 生成 0-3 条 candidate；candidate 必须由同一条 evidence 支撑，不得新增事实。信息不值得保存返回 {"evidence":[]}；有 evidence 但不入候选时 candidates 返回 []。

枚举约束：
- source_type: user_declared, user_confirmed, tool_output, task_result, session_summary, file_edit_summary, agent_summary。
- memory_type: preference, requirement, decision, constraint, assumption, open_issue, failure, project_fact, procedure, temporary_state, session_summary, review_checkpoint。
- scope: user_global, project_local, repo_local, session。project_local 需 project_id；repo_local 需 repo_id；session 需 session_id 且仅用于 temporary_state/session_summary；无法合法归属则不输出 candidate。
- candidate_reason 选 1-3 个：user_declared, user_confirmed, user_correction, constraint_declared, requirement_declared, preference_declared, procedure_declared, architecture_decision, implementation_decision, project_fact_observed, repeated_failure, tool_failure, assumption_declared, open_issue_declared, session_summary, session_only_state, design_review_checkpoint, task_result_summary, openai_classified。

特殊规则：
- user.correction: 若 source_refs 有 target_memory_type/target_memory_scope，candidate 继承；candidate_reason 必含 user_correction。
- review_checkpoint: 仅 source_refs 含 target_docs、review_intent、conclusion 时输出，并填写 review_checkpoint。
- salient_spans/source_ref 只放定位证据，禁止完整原文、完整工具输出、完整 raw_payload_json、完整 diff。
- 外部系统访问失败不值得被存储

示例：
{
  "evidence": [
    {
      "source_type": "user_confirmed",
      "interpreted_statement": "用户纠正：repo 级 MCP 工具调用必须走 registry.Call 成功路径并记录 mcp tool called。",
      "keywords": ["MCP", "registry.Call", "mcp tool called", "repo 级约束"],
      "salient_spans": ["没有任何 mcp tool called 记录", "没有走完 registry.Call 的成功路径"],
      "source_ref": {"producer": "cursor", "target_memory_type": "constraint", "target_memory_scope": "repo_local"},
      "confidence": 0.94,
      "candidates": [
        {
          "memory_type": "constraint",
          "scope": "repo_local",
          "content": "本仓库的 MCP 工具调用必须走完 registry.Call 成功路径，并在日志中记录 mcp tool called。",
          "title": "MCP registry.Call 日志约束",
          "keywords": ["MCP", "registry.Call", "theone.log", "mcp tool called"],
          "candidate_reason": ["user_correction", "constraint_declared"],
          "confidence": 0.92,
          "importance": 0.82
        }
      ]
    }
  ]
}

只返回符合 theone_event_memory schema 的 JSON 对象。`

const OpenAIExtractEvidencePrompt = `从输入的文本中提取可长期检索复用的证据数组。

你会收到当前事件的 event_type 与正文。正文通常只有 input_summary / output_summary；两者都为空时才会收到 content_summary。
证据必须自洽、可独立理解，并保留原意中的偏好/约束/决策/失败等语义信号词，供后续候选记忆分类使用。

保存标准：
保留：用户明确偏好/要求/约束/纠正，技术原理或者技术/流程决策，基本事实，可复用失败原因，重要假设，开放问题，任务或会话结论。
不值得保存则返回 {"evidence":[]}。

提取规则：
每条 evidence 只表达一个事实或判断；不合并无关事实；不编造；不确定则降低 confidence 或返回空数组。
interpreted_statement 用完整陈述句表达，保留“必须/不要/以后/假设/待确认/决策/约束/失败”等有助于下游规则分类的措辞，不要过度抽象到丢失信号，尽量保持在10000字符以内。
keywords 除主题词外，应包含 3-8 个可检索锚点，并覆盖 statement 中的关键语义信号。

字段：
source_type：用户声明=user_declared；用户纠正/确认=user_confirmed；工具失败/输出=tool_output；任务结果=task_result；会话总结=session_summary；文件编辑=file_edit_summary；其他 Agent 总结=agent_summary。可参考 event_type，但不要与正文矛盾。
interpreted_statement：一句可审计重述，保留条件、范围、例外、因果和关键标识。
salient_spans：仅放支撑证据的关键短片段，禁止长段落、完整工具输出、完整 raw_payload_json、完整 diff。
source_ref：仅放定位引用信息，如 producer/capture_method/path/symbol/hash/exit_code，禁止完整原文。
confidence：0-1；用户明确声明/纠正较高；来源不完整、语义不清则降低。

只返回一个符合 theone_evidence schema 的 JSON 对象，不输出其他。

Schema:
{"additionalProperties":false,"properties":{"evidence":{"type":"array","items":{"type":"object","required":["source_type","interpreted_statement","keywords","salient_spans","source_ref","confidence"],"properties":{"source_type":{"type":"string"},"interpreted_statement":{"type":"string"},"keywords":{"type":"array","items":{"type":"string"}},"salient_spans":{"type":"array","items":{"type":"string"}},"source_ref":{"type":"object"},"confidence":{"type":"number"}}}}},"required":["evidence"],"type":"object"}`

const OpenAIGenerateCandidatesPrompt = `根据 evidence 与 raw_event 生成 0-3 条候选记忆。

输入包含：
- raw_event：event_type、正文摘要，以及可选 source_refs（设计复查 checkpoint 元数据）
- evidence：source_type、interpreted_statement、keywords

任务：
1. 判断 evidence 是否值得进入长期记忆候选；不值得则返回 {"candidates":[]}。
2. 值得时输出 1 条主候选（必要时最多 3 条），每条只表达一个记忆单元。
3. content 用完整陈述句，保留条件、范围、因果；不要编造 evidence 未表达的事实。
4. memory_type 与 scope 必须匹配：
   - preference / procedure → 通常 user_global
   - decision / constraint / requirement / assumption / open_issue / project_fact / review_checkpoint → 通常 project_local 或 repo_local
   - temporary_state / session_summary → session
   - failure → repo_local 或 project_local（重复失败、跨会话可复用经验）
5. user.correction 场景：继承 source_refs 中的 target_memory_type / target_memory_scope（若有），candidate_reason 含 user_correction。
6. review_checkpoint：仅当 raw_event.source_refs 含 target_docs、review_intent、conclusion 时输出；填写 review_checkpoint 对象。
7. 无明确设计复查元数据时，不要因为正文含「验收」「复查」就输出 review_checkpoint。

memory_type 枚举：preference, requirement, decision, constraint, assumption, open_issue, failure, project_fact, procedure, temporary_state, session_summary, review_checkpoint
scope 枚举：user_global, project_local, repo_local, session

字段：
- memory_type, scope, content：必填
- title：可选，简短标题；留空由系统生成
- keywords：3-8 个检索锚点，覆盖 content 关键信号
- candidate_reason：1-3 个原因码，如 user_declared, constraint_declared, architecture_decision, session_only_state, design_review_checkpoint, user_correction
- confidence, importance：0-1
- review_checkpoint：仅 review_checkpoint 类型时填写

只返回符合 theone_candidates schema 的 JSON 对象，不输出其他。

Schema:
{"additionalProperties":false,"properties":{"candidates":{"type":"array","items":{"type":"object","required":["memory_type","scope","content","keywords","candidate_reason","confidence","importance"],"properties":{"memory_type":{"type":"string"},"scope":{"type":"string"},"content":{"type":"string"},"title":{"type":"string"},"keywords":{"type":"array","items":{"type":"string"}},"candidate_reason":{"type":"array","items":{"type":"string"}},"confidence":{"type":"number"},"importance":{"type":"number"},"review_checkpoint":{"type":"object"}}}}},"required":["candidates"],"type":"object"}`

const OpenAISemanticEnhancePrompt = `
你需要对输入摘要做语义不变的简化，并提取检索关键词。

你会收到一段输入，其中包含事件类型、来源、行为者、工具名、输入摘要、输出摘要、内容摘要、关键词、关键片段和引用元数据。

操作步骤：
1. 只删除重复、寒暄、过程噪声、低价值元数据和不影响理解的冗余表述。
2. 必须保留事实、结论、决策、约束、失败、偏好、状态、标识符、文件路径、符号名、工具名、数值、条件、范围、例外和因果关系。
3. 不得新增事实，不得推断用户未表达的偏好，不得把一次性状态改写成长期结论。
4. 如果无法确认简化后与原文语义等价，或者输入本身含义不明确，设置 semantic_equivalent=false。

字段填写规则：
- input_summary：简化工具输入或用户输入摘要；没有可保留信息时返回空字符串。
- output_summary：简化工具输出或 agent 输出摘要；没有可保留信息时返回空字符串。
- content_summary：写成结构化索引卡。优先使用这些标签：【事实】、【结论/决策】、【约束】、【失败】、【状态】、【关联】。高价值信息前置。
- keywords：从简化后的语义中提取 3 到 12 个短关键词；去重；删除 hook、trace、memory-context、turn-completed、tool-result 等捕获元数据词。
- salient_spans：只保留能支撑记忆判断的关键短片段，不保存长段落。
- semantic_equivalent：只有确认没有改变语义时才为 true。

禁止事项：
- 不得输出或复制 full_text/full_output/full_diff。
- 不得输出完整原文、完整工具输出、完整 diff 或完整 prompt。
- 不得输出解释、Markdown 或 JSON Schema 之外的字段。

示例：
输入摘要："用户反复强调：以后改 retrieval 逻辑前，要先写覆盖排序和 token_budget 的测试。"
输出 JSON：
{
  "input_summary": "用户要求改 retrieval 逻辑前先写覆盖排序和 token_budget 的测试。",
  "output_summary": "",
  "content_summary": "【约束】修改 retrieval 逻辑前必须先写覆盖排序和 token_budget 的测试。",
  "keywords": ["retrieval", "排序测试", "token_budget"],
  "salient_spans": ["改 retrieval 逻辑前先写覆盖排序和 token_budget 的测试"],
  "semantic_equivalent": true
}

只返回符合 JSON Schema 的 JSON。`
