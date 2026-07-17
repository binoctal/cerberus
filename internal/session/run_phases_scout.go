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
	fmt.Println("• Scout: analyzing & planning...")
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

	// Build coverage contract after Analyze, before Plan
	depth := project.ResolveCoverage(rp.session.Config.Settings.Coverage).Depth
	if depth != "off" {
		rp.session.Contract, err = scoutHead.BuildCoverageContract(rp.ctx, rp.session.Goal, model, depth)
		if err != nil {
			rp.session.Logger.Warn("coverage contract build failed; proceeding without", zap.Error(err))
		}
		if rp.session.Contract != nil && depth != "smoke" {
			if notes, nerr := scoutHead.SelfAssessContract(rp.ctx, rp.session.Contract); nerr == nil {
				rp.session.Logger.Info("contract self-assessment notes", zap.Strings("notes", notes))
			}
		}
		// Persist contract so resume (which skips Scout) can still assess coverage.
		if rp.session.Contract != nil {
			if saveErr := rp.session.Store.SaveContract(rp.ctx, rp.session.ID, rp.session.Contract); saveErr != nil {
				rp.session.Logger.Warn("failed to save contract", zap.Error(saveErr))
			}
		}
	} else {
		rp.session.Logger.Info("coverage disabled - skipping contract build")
	}

	plan, err := scoutHead.Plan(rp.ctx, rp.session.Goal, model)
	if err != nil {
		return nil, fmt.Errorf("scout plan: %w", err)
	}
	if flagged := scoutHead.ValidateTargets(plan, rp.session.ProjectDir); flagged > 0 {
		rp.session.Logger.Info("scout deprioritized invalid targets", zap.Int("flagged", flagged))
	}
	rp.plan = plan

	// Persist plan for potential resumption.
	if saveErr := rp.session.Store.SavePlan(rp.ctx, rp.session.ID, plan); saveErr != nil {
		rp.session.Logger.Warn("failed to save plan", zap.Error(saveErr))
	}

	return model, nil
}
