package project

func DefaultConfig() Config {
	return Config{
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
}
