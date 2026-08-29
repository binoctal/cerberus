package session

import (
	"fmt"

	"go.uber.org/zap"

	embedPkg "github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
)

// buildAgentLoop constructs the Agent execution loop from session config. Shared
// by executeAgentPhase (full plan) and the repair loop (replacement subset).
// The returned index is the loop's live protocol index — the caller that owns
// the long execution window starts the actor-token refresher against it.
func (rp *runPhase) buildAgentLoop() (*agent.ReActLoop, *agent.WSProtocolIndex) {
	projectDir := rp.session.ProjectDir
	if projectDir == "" {
		projectDir = "."
	}
	engine := agent.NewRuleEngine(rp.session.Config.Services, rp.session.Config.Actors, projectDir)
	wsIdx := agent.BuildWSProtocolIndex(rp.session.Config)
	engine.SetWSIndex(wsIdx) // resolve {{role.param}} in rule-engine HTTP case URLs (parity with http_request step path)
	multiExec := agent.BuildMultiExecutor(projectDir, agent.ServiceHeadersMap(rp.session.Config.Services), wsIdx, rp.session.Gate, rp.session.Logger)
	emb := embedPkg.NewTrigramProvider(embedPkg.DefaultDimension)
	loop := agent.NewReActLoopWithGateWithConfig(agent.ReActLoopConfig{
		Driver:   rp.session.driverFor(&rp.session.agentDriver),
		Store:    rp.session.Store,
		Engine:   engine,
		Executor: multiExec,
		WSIdx:    wsIdx,
		Config:   agent.DefaultReActConfig(),
		Gate:     rp.session.Gate,
		Logger:   rp.session.Logger,
		Embedder: emb,
		Project:  rp.session.Config.Project.Name,
		// process_restart steps reach the session harness through here.
		ActorRestart: rp.session,
	})
	return loop, wsIdx
}

// executeAgentPhase runs the Agent execution phase
func (rp *runPhase) executeAgentPhase() error {
	if rp.plan == nil {
		return fmt.Errorf("no plan to execute")
	}
	fmt.Printf("• Agent: executing %d test cases...\n", len(rp.plan.Cases))

	loop, wsIdx := rp.buildAgentLoop()

	// Rotate actor HTTP tokens for the whole execution window: the SUT's
	// access tokens expire in 15 minutes while the sweep runs for hours
	// (run33: 119 "Invalid token" 401 verdicts from the start-of-run token).
	// Repair rounds rebuild their index from the refreshed credentials.
	rp.session.startActorTokenRefresh(rp.ctx, wsIdx)

	rp.session.Logger.Info("executing test plan",
		zap.String("session_id", rp.session.ID),
		zap.Int("cases", len(rp.plan.Cases)),
		zap.Bool("parallel", rp.session.Parallel),
	)

	var err error
	if rp.session.Parallel {
		workers := rp.session.MaxWorkers
		if workers <= 0 {
			workers = 4
		}
		pExec := agent.NewParallelExecutor(loop, agent.ParallelConfig{MaxWorkers: workers}, rp.session.Logger)
		rp.results, err = pExec.ExecutePlan(rp.ctx, rp.plan, rp.session.ID)
	} else {
		rp.results, err = loop.ExecutePlan(rp.ctx, rp.plan, rp.session.ID)
	}

	return err
}
