package agent

import (
	"context"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// recoverer is the interface for the Recover decision point.
type recoverer interface {
	Recover(ctx context.Context, tc TestCase, result types.ExecutorResult, attempt int) (RecoverDecision, error)
	SetSessionID(string)
	SetProject(string)
}

// RecoverDecision holds the recovery decision output.
type RecoverDecision struct {
	Action types.TypedAction
	Skip   bool
}

// ActorRestarter tears down and re-launches one real-process actor (managed
// by the session harness, outside the agent package). The process_restart
// step delegates here; nil means no harness is attached (step fails with a
// clear error instead of silently passing).
type ActorRestarter interface {
	RestartActor(ctx context.Context, actorName string) error
}

// ReActLoop executes test steps using a Reason-Act-Observe cycle.
type ReActLoop struct {
	driver       *ai.Driver
	store        *store.Store
	engine       *RuleEngine
	executor     TypedExecutor
	wsIdx        *WSProtocolIndex // index for http_request step resolution; nil ⇒ no http triggers
	recovery     recoverer
	config       ReActConfig
	logger       *zap.Logger
	gate         escalation.Gate
	processMgr   *ProcessManager
	progressCh   chan<- ProgressEvent
	projectName  string
	actorRestart ActorRestarter
}

// browserExec returns the loop's browser executor when the playwright plugin
// is available, else nil. Used by the step runner for the screenshot file
// sink (browser_shot steps and failure auto-capture).
func (r *ReActLoop) browserExec() *BrowserExecutor {
	m, ok := r.executor.(*MultiExecutor)
	if !ok {
		return nil
	}
	return m.BrowserExec()
}
