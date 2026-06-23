package app

import (
	"log/slog"
	"testing"

	"github.com/zaneway/theone/internal/config"
)

func TestNewProcessorProviderUsesRuleBasedByDefault(t *testing.T) {
	cfg := config.Default()
	provider, err := newProcessorProvider(cfg, slog.Default())
	if err != nil {
		t.Fatalf("newProcessorProvider() error = %v", err)
	}
	if provider.Name() != "rule_based" {
		t.Fatalf("provider = %q, want rule_based", provider.Name())
	}
}

func TestNewProcessorProviderRejectsOpenAIWithoutKey(t *testing.T) {
	cfg := config.Default()
	cfg.Processor.Provider = "openai"
	_, err := newProcessorProvider(cfg, slog.Default())
	if err == nil {
		t.Fatal("newProcessorProvider() error = nil, want missing API key error")
	}
}

func TestNewProcessorProviderUsesConfiguredOpenAIKey(t *testing.T) {
	cfg := config.Default()
	cfg.Processor.Provider = "openai"
	cfg.Processor.OpenAI.APIKey = "configured-key"
	provider, err := newProcessorProvider(cfg, slog.Default())
	if err != nil {
		t.Fatalf("newProcessorProvider() error = %v", err)
	}
	if provider.Name() != "openai" {
		t.Fatalf("provider = %q, want openai", provider.Name())
	}
}

func TestNewQueryEmbeddingProviderDisabledByDefault(t *testing.T) {
	cfg := config.Default()
	provider, err := newQueryEmbeddingProvider(cfg)
	if err != nil {
		t.Fatalf("newQueryEmbeddingProvider() error = %v", err)
	}
	if provider != nil {
		t.Fatalf("provider = %#v, want nil when online query embedding is disabled", provider)
	}
}

func TestNewQueryEmbeddingProviderRequiresOpenAIKeyWhenEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Embedding.Provider = "openai"
	cfg.Embedding.Model = "text-embedding-3-small"
	cfg.Embedding.OnlineQueryEmbeddingEnabled = true
	_, err := newQueryEmbeddingProvider(cfg)
	if err == nil {
		t.Fatal("newQueryEmbeddingProvider() error = nil, want missing API key error")
	}
}
