package session

import (
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
)

// SetupHeadDrivers creates per-head LLM drivers from model config.
// Model resolution uses the Phase 1 priority chain (see config.PickModel):
// explicit settings.models > tier from the host CLI > global ai_budget.model.
// Heads with no resolved model fall back to the shared Driver.
func (s *Session) SetupHeadDrivers(apiKey, baseURL string, authScheme llm.AuthScheme, tiers config.TierModels) {
	s.tiers = tiers
	globalModel := s.Config.Settings.AIBudget.Model
	models := s.Config.Settings.Models

	type headEntry struct {
		head     config.Head
		explicit string
		field    **ai.Driver
	}
	heads := []headEntry{
		{config.HeadScout, models.Scout, &s.scoutDriver},
		{config.HeadAgent, models.Agent, &s.agentDriver},
		{config.HeadExaminer, models.Examiner, &s.examinerDriver},
		{config.HeadCritic, models.Critic, &s.criticDriver},
	}

	for _, h := range heads {
		m := config.PickModel(h.head, h.explicit, tiers, globalModel)
		if m == "" {
			continue // will fall back to shared Driver
		}

		// Use injected clientFactory if available, otherwise default to llm.NewClientWithConfig
		var client llm.Client
		var err error
		if s.clientFactory != nil {
			client, err = s.clientFactory(llm.ClientConfig{
				Model:      m,
				APIKey:     apiKey,
				BaseURL:    baseURL,
				AuthScheme: authScheme,
			})
		} else {
			client, err = llm.NewClientWithConfig(llm.ClientConfig{
				Model:      m,
				APIKey:     apiKey,
				BaseURL:    baseURL,
				AuthScheme: authScheme,
			})
		}
		if err != nil {
			s.Logger.Warn("failed to create head driver, using shared",
				zap.String("head", string(h.head)),
				zap.String("model", m), zap.Error(err))
			continue
		}
		// Heads share the session's single budget so token consumption is
		// accounted globally (ai_budget.session_total_tokens covers the whole
		// session, not per-head). Fall back to an isolated budget only when no
		// shared driver exists (e.g. a unit test constructing Session directly).
		var budget *ai.TokenBudget
		if s.Driver != nil {
			budget = s.Driver.Budget()
		}
		if budget == nil {
			budget = ai.NewTokenBudget(
				s.Config.Settings.AIBudget.SessionTotalTokens,
				s.Config.Settings.AIBudget.PerCallLimit,
			)
		}
		*h.field = ai.NewDriver(client, budget)
		s.Logger.Info("head driver configured",
			zap.String("head", string(h.head)),
			zap.String("model", m))
	}
}

// SetClientFactory injects a clientFactory for creating per-head LLM drivers.
// Used by tests to provide mock clients. If nil, SetupHeadDrivers uses llm.NewClientWithConfig.
func (s *Session) SetClientFactory(factory func(llm.ClientConfig) (llm.Client, error)) {
	s.clientFactory = factory
}

// driverFor returns the per-head driver if set, otherwise the shared Driver.
func (s *Session) driverFor(head **ai.Driver) *ai.Driver {
	if head != nil && *head != nil {
		return *head
	}
	return s.Driver
}
