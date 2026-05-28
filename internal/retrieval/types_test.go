package retrieval

import (
	"testing"

	"github.com/zaneway/theone/internal/memory"
)

func TestFromMemorySearchRequestCopiesFields(t *testing.T) {
	req := memory.SearchRequest{
		Query:           "auth cache decision",
		WorkspaceID:     "ws",
		ProjectID:       "project",
		RepoID:          "repo",
		SessionID:       "session",
		Scope:           []string{memory.ScopeProjectLocal, memory.ScopeUserGlobal},
		MemoryTypes:     []string{memory.TypeDecision, memory.TypeFailure},
		Limit:           8,
		IncludeArchived: true,
		IncludeEvidence: true,
	}

	got := FromMemorySearchRequest(req)

	if got.Query != req.Query ||
		got.WorkspaceID != req.WorkspaceID ||
		got.ProjectID != req.ProjectID ||
		got.RepoID != req.RepoID ||
		got.SessionID != req.SessionID ||
		got.Limit != req.Limit ||
		!got.IncludeArchived ||
		!got.IncludeEvidence {
		t.Fatalf("converted search request = %+v, want fields copied from %+v", got, req)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != memory.ScopeProjectLocal || got.Scopes[1] != memory.ScopeUserGlobal {
		t.Fatalf("converted scopes = %#v, want copied scopes", got.Scopes)
	}
	if len(got.MemoryTypes) != 2 || got.MemoryTypes[0] != memory.TypeDecision || got.MemoryTypes[1] != memory.TypeFailure {
		t.Fatalf("converted memory types = %#v, want copied memory types", got.MemoryTypes)
	}

	req.Scope[0] = memory.ScopeSession
	req.MemoryTypes[0] = memory.TypePreference
	if got.Scopes[0] != memory.ScopeProjectLocal || got.MemoryTypes[0] != memory.TypeDecision {
		t.Fatalf("converted request shares slices with source: scopes=%#v memory_types=%#v", got.Scopes, got.MemoryTypes)
	}
}

func TestFromMemoryContextRequestCopiesFields(t *testing.T) {
	req := memory.ContextRequest{
		Task:                   "继续 retrieval 检索设计复查",
		WorkspaceID:            "ws",
		ProjectID:              "project",
		RepoID:                 "repo",
		SessionID:              "session",
		AgentType:              "codex",
		TokenBudget:            1200,
		IncludeCodeRefs:        true,
		IncludeEvidenceSummary: true,
	}

	got := FromMemoryContextRequest(req)

	if got.Task != req.Task ||
		got.WorkspaceID != req.WorkspaceID ||
		got.ProjectID != req.ProjectID ||
		got.RepoID != req.RepoID ||
		got.SessionID != req.SessionID ||
		got.AgentType != req.AgentType ||
		got.TokenBudget != req.TokenBudget ||
		!got.IncludeCodeRefs ||
		!got.IncludeEvidenceSummary {
		t.Fatalf("converted context request = %+v, want fields copied from %+v", got, req)
	}
}
