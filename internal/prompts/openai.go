package prompts

const OpenAIExtractEvidencePrompt = `你需要从一条已简化的事件记录中，判断是否存在值得以后检索和复用的信息，并把这些信息整理成证据数组。

你会收到一段 JSON 输入，其中可能包含事件摘要、会话摘要、任务摘要、相关历史事件和捕获质量。不要依赖任何项目背景知识，只根据输入内容操作。

操作步骤：
1. 判断输入是否值得保存：
   - 值得保存：用户明确表达的偏好、要求、约束、纠正；已经形成的技术或流程决策；可复用的失败原因；重要假设；开放问题；任务或会话结论。
   - 不值得保存：普通寒暄、仅表示“已完成响应”的消息、无结论的过程描述、纯日志/trace/hook 元数据、无法复用的一次性噪声。
2. 不值得保存时返回空数组，不要强行生成证据。
3. 值得保存时，每条证据只表达一个清晰事实或判断；不要把多个无关事实合并成一条。
4. 不要编造输入中没有的信息；不确定时降低 confidence 或返回空数组。

字段填写规则：
- source_type：说明信息来源。用户明确声明用 user_declared；用户纠正用 user_confirmed；工具失败或输出摘要用 tool_output；任务结果用 task_result；会话总结用 session_summary；文件编辑摘要用 file_edit_summary；其他 agent 总结用 agent_summary。
- interpreted_statement：用一句可审计的话重述证据，保留条件、范围、例外、因果关系和关键标识符。
- keywords：从 interpreted_statement 的语义中提取短关键词，去重，删除 hook、trace、memory-context、turn-completed、tool-result 等捕获元数据词。
- salient_spans：只放支撑该证据的关键短片段，不放长段落、完整工具输出或完整 diff。
- source_ref：只放定位和引用信息，例如 producer、capture_method、path、symbol、hash、exit_code；不要放完整原文。
- confidence：0 到 1。用户明确声明/纠正通常更高；来源不完整、语义不清或捕获质量低时降低。

示例：
输入事件摘要："用户说：以后修改数据库 schema 前必须先写迁移测试。"
输出 JSON：
{
  "evidence": [
    {
      "source_type": "user_declared",
      "interpreted_statement": "用户要求以后修改数据库 schema 前必须先写迁移测试。",
      "keywords": ["数据库 schema", "迁移测试", "约束"],
      "salient_spans": ["修改数据库 schema 前必须先写迁移测试"],
      "source_ref": {"producer": "example"},
      "confidence": 0.95
    }
  ]
}

只返回符合 JSON Schema 的 JSON，不输出解释、Markdown 或额外字段。`

const OpenAIGenerateCandidatesPrompt = `你需要根据输入证据，生成可长期保存、可检索、可审计的记忆候选数组。候选只是“待审核的建议”，不是最终写入结论。

你会收到一段 JSON 输入，其中包含一条证据、对应事件、会话/任务上下文以及相关已有记忆。不要依赖任何项目背景知识，只根据输入内容操作。

操作步骤：
1. 先判断证据是否足以形成候选记忆。
   - 足够：表达稳定偏好、明确约束、需求、决策、可复用失败模式、重要假设、开放问题或有价值的任务/会话结论。
   - 不足：只是一次普通过程状态、缺少结论、语义模糊、不能复用，返回空 candidates 数组。
2. 候选必须严格来自输入证据和事件，不要编造事实。
3. 不要把一次性临时状态提升为长期记忆；只影响当前会话的内容应保持 session 范围。
4. 如果输入体现用户纠正或覆盖旧结论，候选内容应表达新的修正结论，避免与旧结论并存。

字段填写规则：
- 选择 memory_type：
  - preference：用户稳定偏好或工作习惯。
  - constraint：未来执行必须遵守的限制。
  - requirement：明确提出的需求。
  - decision：已经做出的技术、流程或架构决策。
  - failure：可复用的失败模式、根因或规避方式。
  - assumption：后续工作依赖但尚未完全验证的假设。
  - open_issue：尚未解决、需要后续跟进的问题。
  - session_summary：只概括当前会话/任务结果。
  - temporary_state：只在当前会话有效的临时状态。
- 选择 scope：
  - session：只影响当前会话或一次任务过程。
  - project_local：只影响当前项目、仓库、架构、实现或故障模式。
  - user_global：只有用户明确表达为跨项目长期偏好或稳定习惯时才使用；不确定时不要使用。
- title：短标题，概括候选。
- content：完整语义句，保留条件、范围、例外、因果关系和关键标识符。
- keywords、entities、retrieval_cues、tags：从 content 语义中提取，去重，避免捕获元数据词。
- confidence、importance：0 到 1。越明确、越可复用越高。
- encoding_depth：0 到 4。越需要长期抽象和跨场景复用，值越高；临时状态用低值。
- candidate_reason：说明为什么生成候选，例如 explicit_user_preference、architecture_decision、repeated_failure_signature、task_result_summary。
- source_evidence_ids：必须引用输入中真实存在的 evidence ID；不要输出不存在的 ID。

示例：
输入证据：id 为 "ev_1"，内容为 "用户要求以后修改数据库 schema 前必须先写迁移测试。"
输出 JSON：
{
  "candidates": [
    {
      "memory_type": "constraint",
      "scope": "project_local",
      "title": "Schema 修改前先写迁移测试",
      "content": "修改数据库 schema 前必须先写迁移测试。",
      "keywords": ["数据库 schema", "迁移测试"],
      "entities": ["schema"],
      "retrieval_cues": ["修改数据库结构", "数据库迁移"],
      "tags": ["testing", "database"],
      "confidence": 0.92,
      "importance": 0.8,
      "encoding_depth": 3,
      "candidate_reason": ["explicit_user_constraint"],
      "source_evidence_ids": ["ev_1"]
    }
  ]
}

只返回符合 JSON Schema 的 JSON，不输出解释、Markdown 或额外字段。`

const OpenAISemanticEnhancePrompt = `你需要在记忆写入前，对输入摘要做语义不变的简化，并提取检索关键词。

你会收到一段 JSON 输入，其中包含事件类型、来源、行为者、工具名、输入摘要、输出摘要、内容摘要、关键词、关键片段和引用元数据。不要依赖任何项目背景知识，只根据输入内容操作。

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
