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
}

// RecoverDecision holds the recovery decision output.
type RecoverDecision struct {
	Action types.TypedAction
	Skip   bool
}

// ReActLoop executes test steps using a Reason-Act-Observe cycle.
type ReActLoop struct {
	driver     *ai.Driver
	store      *store.Store
	engine     *RuleEngine
	executor   TypedExecutor
	recovery   recoverer
	config     ReActConfig
	logger     *zap.Logger
	gate       escalation.Gate
	processMgr *ProcessManager
	progressCh chan<- ProgressEvent
}
