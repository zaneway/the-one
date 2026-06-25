package app

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/dream"
	"github.com/zaneway/theone/internal/processor"
)

func dreamRuntimeConfig(cfg config.DreamConfig) dream.Config {
	return dream.Config{
		Enabled: cfg.Enabled,
		Vault: dream.VaultConfig{
			Root:           cfg.Vault.Root,
			SystemDir:      cfg.Vault.SystemDir,
			UserNotesDir:   cfg.Vault.UserNotesDir,
			MemoryTypeDirs: cloneStringMap(cfg.Vault.MemoryTypeDirs),
			TopicDirs:      cloneStringMap(cfg.Vault.TopicDirs),
			Directories: dream.DirectoryConfig{
				Inbox:     cfg.Vault.Directories.Inbox,
				Projects:  cfg.Vault.Directories.Projects,
				Knowledge: cfg.Vault.Directories.Knowledge,
				Thinking:  cfg.Vault.Directories.Thinking,
				Skills:    cfg.Vault.Directories.Skills,
				MOC:       cfg.Vault.Directories.MOC,
				Archive:   cfg.Vault.Directories.Archive,
			},
		},
		Scheduler: dream.SchedulerConfig{
			Enabled:               cfg.Scheduler.Enabled,
			IntervalMS:            cfg.Scheduler.IntervalMS,
			InitialDelayMS:        cfg.Scheduler.InitialDelayMS,
			JitterRatio:           cfg.Scheduler.JitterRatio,
			MaxRunDurationMS:      cfg.Scheduler.MaxRunDurationMS,
			SkipIfPreviousRunning: cfg.Scheduler.SkipIfPreviousRunning,
		},
		Curation: dream.CurationConfig{
			Enabled:                cfg.Curation.Enabled,
			MaxInputMemories:       cfg.Curation.MaxInputMemories,
			MaxInputChars:          cfg.Curation.MaxInputChars,
			TimeoutMS:              cfg.Curation.TimeoutMS,
			MinGroupSize:           cfg.Curation.MinGroupSize,
			RequireSourceMemoryIDs: cfg.Curation.RequireSourceMemoryIDs,
			FallbackRules:          cfg.Curation.FallbackRules,
		},
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func newDreamCurator(cfg config.Config, logger *slog.Logger) (dream.Curator, error) {
	if !cfg.Dream.Curation.Enabled {
		return nil, nil
	}
	if cfg.Processor.Provider != processor.OpenAIProviderName {
		return nil, fmt.Errorf("CONFIG_INVALID: dream curation requires processor.provider=%s", processor.OpenAIProviderName)
	}
	timeout := time.Duration(cfg.Dream.Curation.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Duration(cfg.Processor.OpenAI.TimeoutMS) * time.Millisecond
	}
	curator, err := dream.NewOpenAICurator(dream.OpenAICuratorConfig{
		APIKey:          cfg.Processor.OpenAI.APIKey,
		BaseURL:         cfg.Processor.OpenAI.BaseURL,
		Model:           cfg.Processor.OpenAI.Model,
		Timeout:         timeout,
		MaxOutputTokens: int64(cfg.Processor.OpenAI.MaxOutputTokens),
		Logger:          logger,
	})
	if err != nil {
		return nil, fmt.Errorf("init dream curator: %w", err)
	}
	return curator, nil
}
