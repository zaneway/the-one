package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
)

func TestNormalizedRankScoreTreatsMoreNegativeBM25AsMoreRelevant(t *testing.T) {
	strong := normalizedRankScore(rankedMemory{Rank: -3.5})
	weak := normalizedRankScore(rankedMemory{Rank: -0.1})

	if strong <= weak {
		t.Fatalf("normalizedRankScore strong=%v weak=%v, want more negative BM25 rank to score higher", strong, weak)
	}
}

func TestBuildFTSQueryDropsPathNoiseTerms(t *testing.T) {
	got := buildFTSQuery("/Users/zaneway/.theone-data 记忆 上下文 注入")

	for _, noisy := range []string{`"Users"`, `"zaneway"`, `"theone"`, `"data"`} {
		if containsText(got, noisy) {
			t.Fatalf("buildFTSQuery() = %q, should drop path noise term %s", got, noisy)
		}
	}
	for _, want := range []string{`"记忆"`, `"上下文"`, `"注入"`} {
		if !containsText(got, want) {
			t.Fatalf("buildFTSQuery() = %q, want useful term %s", got, want)
		}
	}
}

func TestSearchByLikeMatchesOutOfOrderTerms(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = store.db.ExecContext(ctx, `insert into memory_item(
		id, scope, workspace_id, project_id, memory_type, content, search_text,
		state, confidence, importance, decay_rate, tier, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mem_like_terms", memory.ScopeProjectLocal, "ws", "project_like", memory.TypeDecision,
		"PKI 证书生命周期包含签发和吊销。", "签发 证书 生命周期",
		memory.StateStable, 0.9, 0.8, 0.1, memory.TierLongTerm, now, now,
	)
	if err != nil {
		t.Fatalf("insert memory_item: %v", err)
	}

	results, diag, err := store.searchByLike(ctx, memory.SearchRequest{
		Query:       "证书 签发",
		WorkspaceID: "ws",
		ProjectID:   "project_like",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
	}, 10)
	if err != nil {
		t.Fatalf("searchByLike() error = %v", err)
	}
	if len(results) != 1 || results[0].MemoryID != "mem_like_terms" {
		t.Fatalf("results = %#v diag=%+v, want out-of-order LIKE term hit", results, diag)
	}
}

func containsText(value, sub string) bool {
	return len(sub) == 0 || (len(value) >= len(sub) && findText(value, sub) >= 0)
}

func findText(value, sub string) int {
	for i := 0; i+len(sub) <= len(value); i++ {
		if value[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
