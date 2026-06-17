package examiner

import (
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	embedPkg "github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/store"
)

// Learner performs Reflexion: generates reflections from test results
// and stores them as L3 procedural memory.
type Learner struct {
	driver   *ai.Driver
	store    *store.Store
	logger   *zap.Logger
	embedder embedPkg.Provider
}
