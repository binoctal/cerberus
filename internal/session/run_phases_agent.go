package session

import (
	"fmt"

	"go.uber.org/zap"

	embedPkg "github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
)

// executeAgentPhase runs the Agent execution phase
func (rp *runPhase) executeAgentPhase() error {
	if rp.plan == nil {
		return fmt.Errorf("no plan to execute")
	}
	fmt.Printf("• Agent: executing %d test cases...\n", len(rp.plan.Cases))

	projectDir := rp.session.ProjectDir
	if projectDir == "" {
		projectDir = "."
	}

	engine := agent.NewRuleEngine(rp.session.Config.Services, rp.session.Config.Actors, projectDir)
	multiExec := agent.BuildMultiExecutor(projectDir, agent.ServiceHeadersMap(rp.session.Config.Services), rp.session.Gate, rp.session.Logger)
	config := agent.DefaultReActConfig()
	emb := embedPkg.NewTrigramProvider(embedPkg.DefaultDimension)
	loop := agent.NewReActLoopWithGateWithConfig(agent.ReActLoopConfig{
		Driver:   rp.session.driverFor(&rp.session.agentDriver),
		Store:    rp.session.Store,
		Engine:   engine,
		Executor: multiExec,
		Config:   config,
		Gate:     rp.session.Gate,
		Logger:   rp.session.Logger,
		Embedder: emb,
		Project:  rp.session.Config.Project.Name,
	})

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
