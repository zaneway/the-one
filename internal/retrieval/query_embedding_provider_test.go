package retrieval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIQueryEmbeddingProviderEmbedQuery(t *testing.T) {
	ctx := context.Background()
	var requestBody struct {
		Input          string `json:"input"`
		Model          string `json:"model"`
		EncodingFormat string `json:"encoding_format"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("path = %q, want /embeddings", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object":"list",
			"data":[{"object":"embedding","index":0,"embedding":[0.25,0.5]}],
			"model":"text-embedding-3-small",
			"usage":{"prompt_tokens":1,"total_tokens":1}
		}`))
	}))
	defer server.Close()

	provider, err := NewOpenAIQueryEmbeddingProvider(OpenAIQueryEmbeddingProviderConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "text-embedding-3-small",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewOpenAIQueryEmbeddingProvider() error = %v", err)
	}
	vector, err := provider.EmbedQuery(ctx, "  qkv semantic recall  ")
	if err != nil {
		t.Fatalf("EmbedQuery() error = %v", err)
	}
	if requestBody.Input != "qkv semantic recall" || requestBody.Model != "text-embedding-3-small" || requestBody.EncodingFormat != "float" {
		t.Fatalf("request body = %+v", requestBody)
	}
	if len(vector) != 2 || vector[0] != 0.25 || vector[1] != 0.5 {
		t.Fatalf("vector = %+v, want [0.25 0.5]", vector)
	}
}

func TestCachedQueryEmbeddingProviderReusesQueryVector(t *testing.T) {
	inner := &countingQueryEmbeddingProvider{vector: []float32{0.1, 0.2}}
	provider := NewCachedQueryEmbeddingProvider(inner, 2)

	first, err := provider.EmbedQuery(context.Background(), "same query")
	if err != nil {
		t.Fatalf("first EmbedQuery() error = %v", err)
	}
	first[0] = 9
	second, err := provider.EmbedQuery(context.Background(), "same query")
	if err != nil {
		t.Fatalf("second EmbedQuery() error = %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.calls)
	}
	if second[0] != 0.1 || second[1] != 0.2 {
		t.Fatalf("cached vector was mutated: %+v", second)
	}
}

type countingQueryEmbeddingProvider struct {
	vector []float32
	calls  int
}

func (p *countingQueryEmbeddingProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	p.calls++
	return append([]float32(nil), p.vector...), nil
}
