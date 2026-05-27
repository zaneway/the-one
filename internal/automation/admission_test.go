package automation

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/processor"
)

func TestAdmissionClampAndDropByScore(t *testing.T) {
	controller := NewAdmissionController()
	res := controller.Decide(AdmissionInput{
		Candidate: processor.MemoryCandidate{
			MemoryType:    memory.TypeProjectFact,
			Scope:         memory.ScopeProjectLocal,
			WorkspaceID:   "ws",
			ProjectID:     "proj",
			Content:       "背景事实",
			Confidence:    0.1,
			EncodingDepth: 0,
		},
		RecentScopeLoad: 10,
		RelatedMemory: []memory.MemoryItem{{
			MemoryType: memory.TypeProjectFact,
			Scope:      memory.ScopeProjectLocal,
			State:      memory.StatePendingReview,
			Content:    "背景事实",
		}},
	})
	if res.AdmissionScore < 0 || res.AdmissionScore > 1 {
		t.Fatalf("score = %v, want clamped 0..1", res.AdmissionScore)
	}
	if res.Decision != DecisionDrop {
		t.Fatalf("decision = %s, want drop", res.Decision)
	}
	assertReason(t, res, "candidate_dropped_by_score")
}

func TestAdmissionUserPreferenceStableDurable(t *testing.T) {
	res := NewAdmissionController().Decide(AdmissionInput{
		Candidate: processor.MemoryCandidate{
			MemoryType:        memory.TypePreference,
			Scope:             memory.ScopeUserGlobal,
			UserID:            "local_default_user",
			SourceType:        "user_declared",
			Content:           "以后回答技术方案先分析架构边界、风险和工程落地",
			Keywords:          []string{"架构", "风险"},
			RetrievalCues:     []string{"技术方案"},
			Confidence:        0.9,
			Importance:        0.8,
			EncodingDepth:     2,
			SourceEvidenceIDs: []string{"ev_1"},
		},
	})
	if res.Decision != DecisionWriteStable || res.InitialState != memory.StateStable || res.InitialTier != memory.TierDurable {
		t.Fatalf("result = %s/%s/%s, want stable/stable/durable", res.Decision, res.InitialState, res.InitialTier)
	}
	if !res.UserConfirmed {
		t.Fatal("user confirmed = false, want true")
	}
	assertReason(t, res, "user_declared")
}

func TestAdmissionArchitectureDecisionPendingReview(t *testing.T) {
	res := NewAdmissionController().Decide(AdmissionInput{
		Candidate: projectCandidate(memory.TypeDecision, "P3 只实现 rule_based Provider，外部 LLM 放到二期"),
	})
	if res.Decision != DecisionWritePendingReview || !res.RequiresReview {
		t.Fatalf("result = %s review=%v, want pending review", res.Decision, res.RequiresReview)
	}
	assertReason(t, res, "architecture_decision")
	assertReason(t, res, "high_impact_requires_review")
}

func TestAdmissionSecurityConstraintPendingReview(t *testing.T) {
	res := NewAdmissionController().Decide(AdmissionInput{
		Candidate: projectCandidate(memory.TypeConstraint, "P3 不得保存完整工具输出和完整 diff"),
	})
	if res.Decision != DecisionWritePendingReview || res.InitialState != memory.StatePendingReview {
		t.Fatalf("result = %s/%s, want pending_review", res.Decision, res.InitialState)
	}
	assertReason(t, res, "security_constraint")
}

func TestAdmissionOrdinarySuccessOutputDrop(t *testing.T) {
	candidate := sessionCandidate(memory.TypeTemporaryState, "go test 成功")
	candidate.CandidateReason = []string{"ordinary_success_output"}
	res := NewAdmissionController().Decide(AdmissionInput{Candidate: candidate})
	if res.Decision != DecisionDrop {
		t.Fatalf("decision = %s, want drop", res.Decision)
	}
	assertReason(t, res, "ordinary_success_output")
}

func TestAdmissionToolFailureTemporary(t *testing.T) {
	candidate := sessionCandidate(memory.TypeTemporaryState, "auth token expiry boundary test failed")
	candidate.CandidateReason = []string{"session_only_state"}
	res := NewAdmissionController().Decide(AdmissionInput{Candidate: candidate})
	if res.Decision != DecisionWriteTemporary || res.InitialTier != memory.TierTemporary {
		t.Fatalf("result = %s/%s, want temporary", res.Decision, res.InitialTier)
	}
	assertReason(t, res, "session_only_state")
}

func TestAdmissionRepeatedFailureProvisional(t *testing.T) {
	candidate := repoCandidate(memory.TypeFailure, "重复失败集中在 token expiry boundary")
	candidate.CandidateReason = []string{"repeated_failure_signature"}
	candidate.Importance = 0.6
	res := NewAdmissionController().Decide(AdmissionInput{
		Candidate:      candidate,
		TaskSummary:    "修复 auth token expiry",
		OutcomeSummary: "token expiry failure",
		RelatedMemory: []memory.MemoryItem{{
			MemoryType:   memory.TypeFailure,
			Scope:        memory.ScopeRepoLocal,
			KeywordsJSON: jsonArray("token", "expiry"),
			Content:      "token expiry failure",
		}},
	})
	if res.Decision != DecisionWriteProvisional || res.InitialTier != memory.TierShortTerm {
		t.Fatalf("result = %s/%s, want provisional/short_term", res.Decision, res.InitialTier)
	}
	assertReason(t, res, "repeated_failure_signature")
}

func TestAdmissionConflictPendingReview(t *testing.T) {
	candidate := projectCandidate(memory.TypeProjectFact, "当前数据库不再使用 MySQL，已经改为 PostgreSQL")
	res := NewAdmissionController().Decide(AdmissionInput{
		Candidate: candidate,
		RelatedMemory: []memory.MemoryItem{{
			MemoryType: memory.TypeProjectFact,
			Scope:      memory.ScopeProjectLocal,
			State:      memory.StateStable,
			Confidence: 0.9,
			Content:    "当前数据库使用 MySQL",
		}},
	})
	if res.Decision != DecisionWritePendingReview {
		t.Fatalf("decision = %s, want pending review", res.Decision)
	}
	assertReason(t, res, "conflicts_with_stable_memory")
	assertReason(t, res, "high_impact_requires_review")
}

func TestAdmissionRequirementAssumptionAndOpenIssue(t *testing.T) {
	controller := NewAdmissionController()

	requirement := projectCandidate(memory.TypeRequirement, "P3 验收必须包含 Admission reason codes")
	reqRes := controller.Decide(AdmissionInput{Candidate: requirement})
	if reqRes.Decision != DecisionWritePendingReview {
		t.Fatalf("requirement decision = %s, want pending review for acceptance-impact requirement", reqRes.Decision)
	}
	assertReason(t, reqRes, "requirement_declared")

	assumption := projectCandidate(memory.TypeAssumption, "前置假设：P2 raw_event 已稳定可用")
	assumption.CandidateReason = []string{"assumption_recorded"}
	assumptionRes := controller.Decide(AdmissionInput{Candidate: assumption})
	if assumptionRes.Decision != DecisionWriteProvisional {
		t.Fatalf("assumption decision = %s, want provisional", assumptionRes.Decision)
	}
	assertReason(t, assumptionRes, "assumption_recorded")

	openIssue := projectCandidate(memory.TypeOpenIssue, "待确认是否需要跨 session 巩固策略")
	openIssue.CandidateReason = []string{"open_issue_recorded"}
	openIssueRes := controller.Decide(AdmissionInput{Candidate: openIssue})
	if openIssueRes.Decision != DecisionWritePendingReview || !openIssueRes.RequiresReview {
		t.Fatalf("open issue result = %s review=%v, want pending review", openIssueRes.Decision, openIssueRes.RequiresReview)
	}
	assertReason(t, openIssueRes, "open_issue_recorded")
}

func TestAdmissionScopeInvalid(t *testing.T) {
	res := NewAdmissionController().Decide(AdmissionInput{
		Candidate: processor.MemoryCandidate{
			MemoryType: memory.TypePreference,
			Scope:      memory.ScopeUserGlobal,
			UserID:     "user",
			ProjectID:  "proj",
			Content:    "非法 scope",
		},
	})
	if res.ScopeValid {
		t.Fatal("scope valid = true, want false")
	}
	if res.Decision != DecisionDrop {
		t.Fatalf("decision = %s, want drop", res.Decision)
	}
	assertReason(t, res, "scope_invalid")
}

func TestAdmissionScoreFormulaStable(t *testing.T) {
	candidate := projectCandidate(memory.TypeProjectFact, "项目使用 SQLite 作为默认本地存储")
	candidate.Keywords = []string{"SQLite", "本地存储"}
	candidate.RetrievalCues = []string{"storage"}
	candidate.Confidence = 0.8
	candidate.EncodingDepth = 2
	res := NewAdmissionController().Decide(AdmissionInput{
		Candidate:   candidate,
		TaskSummary: "分析 SQLite 本地存储",
	})
	want := 0.353
	if math.Abs(res.AdmissionScore-want) > 0.001 {
		t.Fatalf("score = %.3f, want %.3f", res.AdmissionScore, want)
	}
}

func projectCandidate(memoryType string, content string) processor.MemoryCandidate {
	return processor.MemoryCandidate{
		MemoryType:        memoryType,
		Scope:             memory.ScopeProjectLocal,
		WorkspaceID:       "ws",
		ProjectID:         "proj",
		SourceType:        "agent_summary",
		Content:           content,
		Keywords:          []string{"P3", "Admission"},
		RetrievalCues:     []string{"reason"},
		Confidence:        0.8,
		Importance:        0.8,
		EncodingDepth:     2,
		SourceEvidenceIDs: []string{"ev_1"},
	}
}

func repoCandidate(memoryType string, content string) processor.MemoryCandidate {
	candidate := projectCandidate(memoryType, content)
	candidate.Scope = memory.ScopeRepoLocal
	candidate.ProjectID = ""
	candidate.RepoID = "repo"
	return candidate
}

func sessionCandidate(memoryType string, content string) processor.MemoryCandidate {
	return processor.MemoryCandidate{
		MemoryType:        memoryType,
		Scope:             memory.ScopeSession,
		WorkspaceID:       "ws",
		SessionID:         "sess",
		SourceType:        "tool_output",
		Content:           content,
		Confidence:        0.7,
		Importance:        0.4,
		EncodingDepth:     1,
		SourceEvidenceIDs: []string{"ev_1"},
	}
}

func assertReason(t *testing.T, res AdmissionResult, reason string) {
	t.Helper()
	for _, item := range res.ReasonCodes {
		if item == reason {
			return
		}
	}
	t.Fatalf("reason %q not found in %#v", reason, res.ReasonCodes)
}

func jsonArray(values ...string) string {
	data, err := json.Marshal(values)
	if err != nil {
		panic(err)
	}
	return string(data)
}
