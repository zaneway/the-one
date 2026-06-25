package dream

import "github.com/zaneway/theone/internal/memory"

func DefaultVaultConfig(root string) VaultConfig {
	return VaultConfig{
		Root:      root,
		SystemDir: ".theone-data",
		Directories: DirectoryConfig{
			Inbox:     "00-inbox",
			Projects:  "10-projects",
			Knowledge: "20-knowledge",
			Thinking:  "30-thinking",
			Skills:    "40-skills",
			MOC:       "80-moc",
			Archive:   "90-archive",
		},
		UserNotesDir: "99-user-notes",
		MemoryTypeDirs: map[string]string{
			memory.TypeDecision:         "decisions",
			memory.TypeConstraint:       "constraints",
			memory.TypeFailure:          "failures",
			memory.TypeReviewCheckpoint: "reviews",
			memory.TypeProjectFact:      "facts",
			memory.TypeProcedure:        "procedures",
			memory.TypePreference:       "preferences",
			memory.TypeOpenIssue:        "open-issues",
		},
		TopicDirs: map[string]string{
			"memory-system":      "memory-systems",
			"distributed-system": "distributed-systems",
			"pki":                "pki",
			"security":           "security",
			"database":           "databases",
			"ai-agent":           "ai-agents",
		},
	}
}

func DefaultConfig(root string) Config {
	return Config{
		Enabled: true,
		Vault:   DefaultVaultConfig(root),
		Scheduler: SchedulerConfig{
			Enabled:               false,
			IntervalMS:            60 * 60 * 1000,
			InitialDelayMS:        30 * 1000,
			JitterRatio:           0.1,
			MaxRunDurationMS:      5 * 60 * 1000,
			SkipIfPreviousRunning: true,
		},
		Curation: CurationConfig{
			Enabled:                false,
			MaxInputMemories:       50,
			MaxInputChars:          30000,
			TimeoutMS:              60000,
			MinGroupSize:           2,
			RequireSourceMemoryIDs: true,
			FallbackRules:          true,
		},
	}
}
