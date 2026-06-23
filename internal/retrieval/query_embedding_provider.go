package retrieval

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// OpenAIQueryEmbeddingProviderConfig 描述 OpenAI-compatible embeddings API 配置。
// APIKey/BaseURL 通常复用 processor.openai 配置，Model 使用 embedding.model。
type OpenAIQueryEmbeddingProviderConfig struct {
	APIKey     string
	BaseURL    string
	Model      string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type OpenAIQueryEmbeddingProvider struct {
	client  openai.Client
	model   string
	timeout time.Duration
}

func NewOpenAIQueryEmbeddingProvider(cfg OpenAIQueryEmbeddingProviderConfig) (*OpenAIQueryEmbeddingProvider, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("CONFIG_INVALID: embedding openai api key is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, fmt.Errorf("CONFIG_INVALID: embedding model is required")
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("CONFIG_INVALID: embedding timeout must be positive")
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL := strings.TrimSpace(cfg.BaseURL); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	return &OpenAIQueryEmbeddingProvider{
		client:  openai.NewClient(opts...),
		model:   model,
		timeout: cfg.Timeout,
	}, nil
}

// EmbedQuery 调用 embeddings API 生成查询向量。
// 失败由上层 Orchestrator 转换为 query_embedding_failed 降级，不阻断 FTS 主链路。
func (p *OpenAIQueryEmbeddingProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if p == nil {
		return nil, fmt.Errorf("PROVIDER_NOT_FOUND: query embedding provider is nil")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: query is required")
	}
	if p.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}
	resp, err := p.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: openai.String(query),
		},
		Model:          openai.EmbeddingModel(p.model),
		EncodingFormat: openai.EmbeddingNewParamsEncodingFormatFloat,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Data) == 0 || len(resp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("PROVIDER_INVALID_OUTPUT: empty embedding response")
	}
	vector := make([]float32, len(resp.Data[0].Embedding))
	for i, value := range resp.Data[0].Embedding {
		vector[i] = float32(value)
	}
	return vector, nil
}

type cachedQueryEmbeddingProvider struct {
	inner QueryEmbeddingProvider
	max   int
	mu    sync.Mutex
	cache map[string][]float32
	order []string
}

func NewCachedQueryEmbeddingProvider(inner QueryEmbeddingProvider, max int) QueryEmbeddingProvider {
	if inner == nil || max <= 0 {
		return inner
	}
	return &cachedQueryEmbeddingProvider{
		inner: inner,
		max:   max,
		cache: make(map[string][]float32, max),
		order: make([]string, 0, max),
	}
}

func (p *cachedQueryEmbeddingProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	key := strings.TrimSpace(query)
	p.mu.Lock()
	if vector, ok := p.cache[key]; ok {
		out := append([]float32(nil), vector...)
		p.mu.Unlock()
		return out, nil
	}
	p.mu.Unlock()

	vector, err := p.inner.EmbedQuery(ctx, query)
	if err != nil || len(vector) == 0 {
		return vector, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.cache[key]; !exists {
		if len(p.order) >= p.max {
			evicted := p.order[0]
			p.order = p.order[1:]
			delete(p.cache, evicted)
		}
		p.order = append(p.order, key)
	}
	p.cache[key] = append([]float32(nil), vector...)
	return append([]float32(nil), vector...), nil
}
