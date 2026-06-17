package examiner

import (
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/store"
)

// NewLearner creates a Reflexion learner.
// If embedder is nil, a default TrigramProvider is used.
func NewLearner(driver *ai.Driver, s *store.Store, logger *zap.Logger, embedder embed.Provider) *Learner {
	if embedder == nil {
		embedder = embed.NewTrigramProvider(embed.DefaultDimension)
	}
	return &Learner{driver: driver, store: s, logger: logger, embedder: embedder}
}
