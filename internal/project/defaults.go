package project

func DefaultConfig() Config {
	cfg := Config{
		Settings: Settings{
			MaxDuration:         "30m",
			ConfidenceThreshold: 0.7,
			AutoFix:             "low_only",
			AIBudget: AIBudget{
				SessionTotalTokens: 200000,
				PerCallLimit:       10000,
				Model:              "claude-sonnet-4-6",
			},
			CostAlerts: CostAlerts{
				WarnAtPct: 80,
				StopAtPct: 100,
			},
		},
	}
	cfg.Settings.Coverage = ResolveCoverage(cfg.Settings.Coverage)
	return cfg
}
