package sqlite

import "testing"

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
