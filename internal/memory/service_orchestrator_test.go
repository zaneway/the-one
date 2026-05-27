package memory

import (
	"context"
	"testing"

	"github.com/zaneway/theone/internal/config"
)

type stubRetrievalOrchestrator struct {
	searchCalled  bool
	contextCalled bool
}

func (s *stubRetrievalOrchestrator) Search(context.Context, SearchRequest) (SearchResponse, error) {
	s.searchCalled = true
	return SearchResponse{
		Results: []SearchResult{{
			MemoryID:       "mem_p4",
			MemoryType:     TypeDecision,
			Scope:          ScopeProjectLocal,
			Content:        "P4 orchestrator search result",
			Score:          0.91,
			ScoreBreakdown: &ScoreBreakdown{Final: 0.91},
			WhyIncluded:    []string{"task_match"},
		}},
		Diagnostics: SearchDiagnostics{
			RetrievalTraceID: "rt_p4",
			RetrievalMode:    "fts_relation",
			UsedFTS:          true,
			UsedRelation:     true,
		},
	}, nil
}

func (s *stubRetrievalOrchestrator) Context(context.Context, ContextRequest) (ContextResponse, error) {
	s.contextCalled = true
	return ContextResponse{
		ContextPack: ContextPack{
			Summary: "P4 orchestrator context",
			Memories: []ContextMemory{{
				MemoryID:       "mem_p4",
				Type:           TypeDecision,
				Compressed:     "P4 orchestrator context",
				WhyIncluded:    []string{"task_match"},
				ScoreBreakdown: &ScoreBreakdown{Final: 0.88},
			}},
			CodeRefs: []CodeRef{},
		},
		UsedMemoryIDs:    []string{"mem_p4"},
		RetrievalTraceID: "rt_p4_ctx",
		Diagnostics: &ContextDiagnostics{
			RetrievalIntent: "general_search",
			RetrievalMode:   "fts_relation",
		},
	}, nil
}

func TestServiceDelegatesSearchToRetrievalOrchestrator(t *testing.T) {
	orchestrator := &stubRetrievalOrchestrator{}
	service := NewService(config.Default(), nil, WithRetrievalOrchestrator(orchestrator))

	resp, err := service.Search(context.Background(), SearchRequest{Query: "ignored by stub"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if !orchestrator.searchCalled {
		t.Fatal("Search() did not delegate to retrieval orchestrator")
	}
	if resp.Diagnostics.RetrievalTraceID != "rt_p4" || len(resp.Results) != 1 || resp.Results[0].ScoreBreakdown == nil {
		t.Fatalf("Search() response = %+v, want orchestrator response", resp)
	}
}

func TestServiceDelegatesContextToRetrievalOrchestrator(t *testing.T) {
	orchestrator := &stubRetrievalOrchestrator{}
	service := NewService(config.Default(), nil, WithRetrievalOrchestrator(orchestrator))

	resp, err := service.Context(context.Background(), ContextRequest{Task: "ignored by stub"})
	if err != nil {
		t.Fatalf("Context() error = %v", err)
	}
	if !orchestrator.contextCalled {
		t.Fatal("Context() did not delegate to retrieval orchestrator")
	}
	if resp.RetrievalTraceID != "rt_p4_ctx" || len(resp.ContextPack.Memories) != 1 || resp.Diagnostics == nil {
		t.Fatalf("Context() response = %+v, want orchestrator response", resp)
	}
}
