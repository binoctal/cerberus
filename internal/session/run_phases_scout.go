package session

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// executeScoutPhase runs the Scout analysis and planning phase
func (rp *runPhase) executeScoutPhase() (*project.ProjectModel, error) {
	scoutHead := scout.NewScout(rp.session.driverFor(&rp.session.scoutDriver), rp.session.Store, rp.session.Config, rp.session.Logger)

	// Scale ToT/Reflexion depth to the Scout model's context window
	scoutModel := rp.session.tiers[config.HeadScout]
	scoutCtx := 0
	if scoutModel != "" {
		scoutCtx = llm.ContextWindow(scoutModel)
	}
	scoutHead.SetReflexion(config.ResolveReflexionConfig(rp.session.Config.Settings, scoutCtx))

	if rp.session.DeepPlan {
		scoutHead.SetDeepPlan(
			config.ResolveToTConfig(rp.session.Config.Settings, scoutCtx),
			rp.session.driverFor(&rp.session.scoutDriver),
			rp.session.driverFor(&rp.session.agentDriver),
		)
	}

	model, err := scoutHead.Analyze(rp.ctx, scout.TargetInfo{
		URL:  rp.session.resolveBaseURL(),
		Goal: rp.session.Goal,
	})
	if err != nil {
		return nil, fmt.Errorf("scout analyze: %w", err)
	}

	plan, err := scoutHead.Plan(rp.ctx, rp.session.Goal, model)
	if err != nil {
		return nil, fmt.Errorf("scout plan: %w", err)
	}
	rp.plan = plan

	// Persist plan for potential resumption.
	if saveErr := rp.session.Store.SavePlan(rp.ctx, rp.session.ID, plan); saveErr != nil {
		rp.session.Logger.Warn("failed to save plan", zap.Error(saveErr))
	}

	return model, nil
}
