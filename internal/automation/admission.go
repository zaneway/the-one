package automation

import (
	"encoding/json"
	"strings"

	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/processor"
)

const (
	DecisionDrop               = "drop"
	DecisionWriteRawOnly       = "write_raw_only"
	DecisionWriteTemporary     = "write_temporary"
	DecisionWriteProvisional   = "write_provisional"
	DecisionWritePendingReview = "write_pending_review"
	DecisionWriteStable        = "write_stable"
)

type AdmissionController struct{}

type AdmissionInput struct {
	Candidate       processor.MemoryCandidate
	RelatedMemory   []memory.MemoryItem
	TaskSummary     string
	OutcomeSummary  string
	RecentScopeLoad int
}

type AdmissionResult struct {
	Decision         string   `json:"decision"`
	AdmissionScore   float64  `json:"admission_score"`
	MemoryType       string   `json:"memory_type"`
	Scope            string   `json:"scope"`
	InitialState     string   `json:"initial_state"`
	InitialTier      string   `json:"initial_tier"`
	DecayRate        float64  `json:"decay_rate"`
	RequiresReview   bool     `json:"requires_review"`
	UserConfirmed    bool     `json:"user_confirmed"`
	ReasonCodes      []string `json:"reason_codes"`
	ScopeValid       bool     `json:"scope_valid"`
	HighImpact       bool     `json:"high_impact"`
	ConflictRisk     float64  `json:"conflict_risk"`
	InterferenceRisk float64  `json:"interference_risk"`
	DecayRisk        float64  `json:"decay_risk"`
}

func NewAdmissionController() AdmissionController {
	return AdmissionController{}
}

func (AdmissionController) Decide(input AdmissionInput) AdmissionResult {
	candidate := input.Candidate
	reasons := reasonSet{}
	scopeValid := validateCandidateScope(candidate)
	if !scopeValid {
		reasons.add("scope_invalid")
		return result(candidate, DecisionDrop, 0, "", "", false, false, reasons, false, false, 0, 0, 0)
	}
	addScopeReasons(candidate, &reasons)
	addCandidateReasons(candidate, &reasons)

	features := estimateFeatures(input)
	score := clamp(
		0.22*features.futureNeed+
			0.18*features.encodingDepthScore+
			0.16*features.stability+
			0.14*features.taskControlSignal+
			0.12*features.episodicSemanticValue+
			0.08*features.retrievalTrainability-
			0.16*features.interferenceRisk-
			0.12*features.decayRisk-
			0.10*features.conflictRisk,
		0,
		1,
	)

	if features.conflictRisk > 0 {
		reasons.add("conflicts_with_stable_memory")
	}
	highImpact := isHighImpact(candidate, features)
	if highImpact {
		reasons.add("high_impact_requires_review")
	}

	decision, state, tier, requiresReview, userConfirmed := specialDecision(candidate, highImpact, &reasons)
	if decision == "" {
		decision, state, tier, requiresReview, userConfirmed = scoreDecision(candidate, score, &reasons)
	}
	return result(candidate, decision, score, state, tier, requiresReview, userConfirmed, reasons, scopeValid, highImpact, features.conflictRisk, features.interferenceRisk, features.decayRisk)
}

type admissionFeatures struct {
	futureNeed            float64
	encodingDepthScore    float64
	stability             float64
	taskControlSignal     float64
	episodicSemanticValue float64
	retrievalTrainability float64
	interferenceRisk      float64
	decayRisk             float64
	conflictRisk          float64
}

func estimateFeatures(input AdmissionInput) admissionFeatures {
	candidate := input.Candidate
	repeatedTopic := repeatedTopicScore(candidate, input.RelatedMemory)
	taskRelevance := taskRelevanceScore(candidate, input.TaskSummary+" "+input.OutcomeSummary)
	projectScope := projectScopeRelevance(candidate)
	userPreference := 0.0
	if candidate.MemoryType == memory.TypePreference || candidate.SourceType == "user_declared" {
		userPreference = 1.0
	}
	futureNeed := clamp(0.4*repeatedTopic+0.3*taskRelevance+0.2*projectScope+0.1*userPreference, 0, 1)

	sourceCount := clamp(float64(len(candidate.SourceEvidenceIDs))/2.0, 0, 1)
	confirmation := 0.0
	if candidate.SourceType == "user_declared" || candidate.SourceType == "user_confirmed" {
		confirmation = 1.0
	}
	stability := clamp(0.35*sourceCount+0.25*0.2+0.25*confirmation+0.15*clamp(candidate.Confidence, 0, 1), 0, 1)

	interference := 0.0
	if input.RecentScopeLoad >= 5 {
		interference += 0.4
	}
	if hasReason(candidate, "ambiguous") || hasSignal(candidate.Content, "可能", "也许", "不确定", "ambiguous") {
		interference += 0.3
	}
	for _, item := range input.RelatedMemory {
		if item.State == memory.StatePendingReview || item.State == memory.StateProvisional {
			interference += 0.3
			break
		}
	}

	conflict := 0.0
	for _, item := range input.RelatedMemory {
		if item.State == memory.StateStable && isLikelyConflict(candidate, item) {
			conflict += 0.4
		}
		if item.Confidence > 0 && item.Confidence < 0.5 {
			conflict += 0.2
		}
	}

	return admissionFeatures{
		futureNeed:            futureNeed,
		encodingDepthScore:    clamp(float64(candidate.EncodingDepth)/4.0, 0, 1),
		stability:             stability,
		taskControlSignal:     taskControlSignal(candidate),
		episodicSemanticValue: semanticValue(candidate.MemoryType),
		retrievalTrainability: clamp(0.2*float64(len(candidate.RetrievalCues))+0.1*float64(len(candidate.Keywords)), 0, 1),
		interferenceRisk:      clamp(interference, 0, 1),
		decayRisk:             decayRisk(candidate),
		conflictRisk:          clamp(conflict, 0, 1),
	}
}

func specialDecision(candidate processor.MemoryCandidate, highImpact bool, reasons *reasonSet) (string, string, string, bool, bool) {
	if hasReason(candidate, "ordinary_success_output") {
		reasons.add("ordinary_success_output")
		return DecisionDrop, "", "", false, false
	}
	if candidate.SourceType == "user_confirmed" || hasReason(candidate, "user_correction") {
		reasons.add("user_correction")
		return DecisionWriteStable, memory.StateStable, tierForStable(candidate, highImpact), false, true
	}
	switch candidate.MemoryType {
	case memory.TypeDecision:
		reasons.add("architecture_decision")
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	case memory.TypeConstraint:
		reasons.add("security_constraint")
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	case memory.TypeReviewCheckpoint:
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	case memory.TypeFailure:
		if highImpact || candidate.Importance >= 0.8 {
			reasons.add("high_impact_failure")
			return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
		}
		if hasReason(candidate, "repeated_failure_signature") {
			reasons.add("repeated_failure_signature")
			return DecisionWriteProvisional, memory.StateProvisional, memory.TierShortTerm, false, false
		}
	case memory.TypeRequirement:
		reasons.add("requirement_declared")
		if highImpact {
			return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
		}
		return DecisionWriteStable, memory.StateStable, memory.TierLongTerm, false, candidate.SourceType == "user_declared"
	case memory.TypeAssumption:
		reasons.add("assumption_recorded")
		if highImpact {
			return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
		}
		return DecisionWriteProvisional, memory.StateProvisional, memory.TierShortTerm, false, false
	case memory.TypeOpenIssue:
		reasons.add("open_issue_recorded")
		if candidate.Scope == memory.ScopeSession {
			return DecisionWriteTemporary, memory.StateProvisional, memory.TierTemporary, false, false
		}
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	case memory.TypeTemporaryState, memory.TypeSessionSummary:
		reasons.add("session_only_state")
		return DecisionWriteTemporary, memory.StateProvisional, memory.TierTemporary, false, false
	case memory.TypePreference:
		if candidate.SourceType == "user_declared" && !highImpact {
			reasons.add("user_declared")
			return DecisionWriteStable, memory.StateStable, memory.TierDurable, false, true
		}
	}
	if highImpact {
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	}
	return "", "", "", false, false
}

func scoreDecision(candidate processor.MemoryCandidate, score float64, reasons *reasonSet) (string, string, string, bool, bool) {
	switch {
	case score < 0.30:
		reasons.add("candidate_dropped_by_score")
		return DecisionDrop, "", "", false, false
	case score < 0.50:
		if candidate.Scope == memory.ScopeSession {
			reasons.add("session_only_state")
			return DecisionWriteTemporary, memory.StateProvisional, memory.TierTemporary, false, false
		}
		return DecisionWriteRawOnly, "", "", false, false
	case score < 0.70:
		return DecisionWriteProvisional, memory.StateProvisional, memory.TierShortTerm, false, false
	case score < 0.85:
		if candidate.SourceType == "user_declared" && candidate.MemoryType == memory.TypePreference {
			reasons.add("user_declared")
			return DecisionWriteStable, memory.StateStable, memory.TierDurable, false, true
		}
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	default:
		if candidate.SourceType == "user_declared" {
			reasons.add("user_declared")
			return DecisionWriteStable, memory.StateStable, tierForStable(candidate, false), false, true
		}
		return DecisionWriteStable, memory.StateStable, memory.TierLongTerm, false, false
	}
}

func result(candidate processor.MemoryCandidate, decision string, score float64, state string, tier string, requiresReview bool, userConfirmed bool, reasons reasonSet, scopeValid bool, highImpact bool, conflictRisk float64, interferenceRisk float64, decayRiskValue float64) AdmissionResult {
	return AdmissionResult{
		Decision:         decision,
		AdmissionScore:   score,
		MemoryType:       candidate.MemoryType,
		Scope:            candidate.Scope,
		InitialState:     state,
		InitialTier:      tier,
		DecayRate:        defaultDecayRate(candidate.MemoryType),
		RequiresReview:   requiresReview,
		UserConfirmed:    userConfirmed,
		ReasonCodes:      reasons.list(),
		ScopeValid:       scopeValid,
		HighImpact:       highImpact,
		ConflictRisk:     conflictRisk,
		InterferenceRisk: interferenceRisk,
		DecayRisk:        decayRiskValue,
	}
}

func validateCandidateScope(candidate processor.MemoryCandidate) bool {
	switch candidate.Scope {
	case memory.ScopeUserGlobal:
		return candidate.UserID != "" && candidate.ProjectID == "" && candidate.RepoID == ""
	case memory.ScopeProjectLocal:
		return candidate.WorkspaceID != "" && candidate.ProjectID != ""
	case memory.ScopeRepoLocal:
		return candidate.WorkspaceID != "" && candidate.RepoID != ""
	case memory.ScopeSession:
		return candidate.WorkspaceID != "" && candidate.SessionID != ""
	default:
		return false
	}
}

func isHighImpact(candidate processor.MemoryCandidate, features admissionFeatures) bool {
	if features.conflictRisk > 0 {
		return true
	}
	switch candidate.MemoryType {
	case memory.TypeDecision, memory.TypeConstraint, memory.TypeReviewCheckpoint:
		return true
	case memory.TypeRequirement:
		return hasSignal(candidate.Content, "验收", "阶段", "边界", "必须", "acceptance", "phase", "boundary")
	case memory.TypeFailure:
		return candidate.Importance >= 0.8
	}
	return false
}

func taskControlSignal(candidate processor.MemoryCandidate) float64 {
	content := candidate.Content
	if candidate.SourceType == "user_declared" && hasSignal(content, "以后", "记住", "不要", "必须") {
		return 1.0
	}
	switch candidate.MemoryType {
	case memory.TypeRequirement:
		return 0.9
	case memory.TypeDecision, memory.TypeConstraint:
		return 0.8
	case memory.TypeOpenIssue:
		return 0.7
	case memory.TypeTemporaryState, memory.TypeSessionSummary:
		return 0.6
	default:
		return 0.3
	}
}

func semanticValue(memoryType string) float64 {
	switch memoryType {
	case memory.TypeDecision, memory.TypeConstraint, memory.TypeRequirement, memory.TypeReviewCheckpoint:
		return 0.9
	case memory.TypePreference, memory.TypeFailure, memory.TypeProcedure:
		return 0.8
	case memory.TypeAssumption, memory.TypeOpenIssue:
		return 0.7
	case memory.TypeProjectFact:
		return 0.6
	case memory.TypeSessionSummary:
		return 0.4
	case memory.TypeTemporaryState:
		return 0.3
	default:
		return 0.5
	}
}

func decayRisk(candidate processor.MemoryCandidate) float64 {
	if candidate.Scope == memory.ScopeSession {
		return 0.6
	}
	switch candidate.MemoryType {
	case memory.TypeTemporaryState, memory.TypeSessionSummary:
		return 0.8
	case memory.TypeProjectFact:
		return 0.4
	case memory.TypePreference, memory.TypeDecision, memory.TypeFailure, memory.TypeConstraint, memory.TypeRequirement:
		return 0.2
	default:
		return 0.3
	}
}

func tierForStable(candidate processor.MemoryCandidate, highImpact bool) string {
	if candidate.MemoryType == memory.TypePreference && candidate.Scope == memory.ScopeUserGlobal && !highImpact {
		return memory.TierDurable
	}
	if candidate.MemoryType == memory.TypeProcedure && candidate.Scope == memory.ScopeUserGlobal && !highImpact {
		return memory.TierDurable
	}
	return memory.TierLongTerm
}

func defaultDecayRate(memoryType string) float64 {
	switch memoryType {
	case memory.TypeDecision, memory.TypeConstraint, memory.TypePreference, memory.TypeRequirement:
		return 0.3
	case memory.TypeFailure, memory.TypeProcedure, memory.TypeAssumption, memory.TypeOpenIssue:
		return 0.45
	case memory.TypeTemporaryState, memory.TypeSessionSummary:
		return 1.2
	default:
		return 0.8
	}
}

func repeatedTopicScore(candidate processor.MemoryCandidate, related []memory.MemoryItem) float64 {
	if len(related) == 0 || len(candidate.Keywords) == 0 {
		return 0
	}
	for _, item := range related {
		if item.MemoryType == candidate.MemoryType && overlapKeywords(candidate.Keywords, item) {
			return 1
		}
	}
	return 0
}

func taskRelevanceScore(candidate processor.MemoryCandidate, text string) float64 {
	if len(candidate.Keywords) == 0 {
		return 0
	}
	matches := 0
	text = strings.ToLower(text)
	for _, keyword := range candidate.Keywords {
		if keyword != "" && strings.Contains(text, strings.ToLower(keyword)) {
			matches++
		}
	}
	return clamp(float64(matches)/float64(len(candidate.Keywords)), 0, 1)
}

func projectScopeRelevance(candidate processor.MemoryCandidate) float64 {
	switch candidate.Scope {
	case memory.ScopeProjectLocal:
		if candidate.ProjectID != "" {
			return 1
		}
	case memory.ScopeRepoLocal:
		if candidate.RepoID != "" {
			return 1
		}
	}
	return 0
}

func isLikelyConflict(candidate processor.MemoryCandidate, item memory.MemoryItem) bool {
	if item.MemoryType != candidate.MemoryType || item.Scope != candidate.Scope {
		return false
	}
	text := strings.ToLower(candidate.Content + " " + item.Content)
	return strings.Contains(text, "改为") ||
		strings.Contains(text, "不再") ||
		strings.Contains(text, "instead") ||
		strings.Contains(text, "deprecated")
}

func overlapKeywords(keywords []string, item memory.MemoryItem) bool {
	combined := strings.ToLower(item.Title + " " + item.Content + " " + strings.Join(decodeStringSlice(item.KeywordsJSON), " "))
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(combined, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func decodeStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return values
}

func addScopeReasons(candidate processor.MemoryCandidate, reasons *reasonSet) {
	switch candidate.Scope {
	case memory.ScopeProjectLocal:
		reasons.add("project_scoped")
	case memory.ScopeRepoLocal:
		reasons.add("repo_scoped")
	case memory.ScopeSession:
		reasons.add("session_only_state")
	}
}

func addCandidateReasons(candidate processor.MemoryCandidate, reasons *reasonSet) {
	for _, reason := range candidate.CandidateReason {
		reasons.add(reason)
	}
	if candidate.SourceType == "user_declared" {
		reasons.add("user_declared")
	}
	if candidate.SourceType == "user_confirmed" {
		reasons.add("user_correction")
	}
}

func hasReason(candidate processor.MemoryCandidate, reason string) bool {
	for _, item := range candidate.CandidateReason {
		if item == reason {
			return true
		}
	}
	return false
}

func hasSignal(text string, signals ...string) bool {
	text = strings.ToLower(text)
	for _, signal := range signals {
		if strings.Contains(text, strings.ToLower(signal)) {
			return true
		}
	}
	return false
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

type reasonSet struct {
	items []string
	seen  map[string]bool
}

func (s *reasonSet) add(reason string) {
	if reason == "" {
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[reason] {
		return
	}
	s.seen[reason] = true
	s.items = append(s.items, reason)
}

func (s reasonSet) list() []string {
	return append([]string(nil), s.items...)
}
