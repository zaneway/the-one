package app

import (
	"testing"

	"github.com/zaneway/theone/internal/config"
)

func TestNewProcessorProviderUsesRuleBasedByDefault(t *testing.T) {
	cfg := config.Default()
	provider, err := newProcessorProvider(cfg)
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
	cfg.Processor.OpenAI.APIKeyEnv = "THEONE_TEST_MISSING_OPENAI_KEY"
	t.Setenv("THEONE_TEST_MISSING_OPENAI_KEY", "")
	_, err := newProcessorProvider(cfg)
	if err == nil {
		t.Fatal("newProcessorProvider() error = nil, want missing API key error")
	}
}
