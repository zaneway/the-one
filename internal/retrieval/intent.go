package retrieval

import "strings"

var intentKeywordRules = []struct {
	intent   RetrievalIntent
	keywords []string
}{
	{IntentArchitectureReview, []string{"复查", "架构评审", "详细设计", "逻辑缺失", "checkpoint", "review", "architecture review", "design review"}},
	{IntentTaskContinuation, []string{"继续", "上次", "当前任务", "接着", "延续", "continuation", "continue"}},
	{IntentFailureRecall, []string{"失败", "报错", "踩坑", "错误", "为什么又", "failure", "error", "incident"}},
	{IntentUserPreference, []string{"偏好", "习惯", "以后", "我的要求", "preference"}},
	{IntentCodeTask, []string{".go", ".java", ".py", ".rs", ".ts", ".js", "函数", "方法", "模块", "文件", "symbol", "stack", "代码", "调用关系"}},
}

// DetectSearchIntent 根据 P4 search 请求识别检索意图。
// 如果调用方显式传入 IntentHint，优先使用 hint，避免覆盖上游已经确定的业务场景。
func DetectSearchIntent(req SearchRequest) RetrievalIntent {
	if req.IntentHint != "" {
		return req.IntentHint
	}
	return DetectIntent(req.Query, req.Task)
}

// DetectContextIntent 根据 P4 context 请求识别检索意图。
// context 场景通常以 task 为主，但仍复用统一规则，保证 search/context 行为一致。
func DetectContextIntent(req ContextRequest) RetrievalIntent {
	if req.IntentHint != "" {
		return req.IntentHint
	}
	return DetectIntent("", req.Task)
}

// DetectIntent 使用规则优先的轻量意图识别。
// P4-D2 不引入 LLM 分类器，所有规则必须确定、可测试、可解释。
func DetectIntent(query, task string) RetrievalIntent {
	text := strings.ToLower(strings.TrimSpace(query + " " + task))
	if text == "" {
		return IntentGeneralSearch
	}
	for _, rule := range intentKeywordRules {
		for _, keyword := range rule.keywords {
			if strings.Contains(text, strings.ToLower(keyword)) {
				return rule.intent
			}
		}
	}
	return IntentGeneralSearch
}
