package session

import (
	"context"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// resolveActorAuth runs each actor's declarative auth flow once and writes the
// resulting header into that actor's Credentials.Headers. After this runs, the
// existing static-header injection path (authHeadersFor / withActorHeaders)
// carries the dynamic token with no further changes.
//
// Failures degrade, never abort: a failed login, non-2xx response, or missing
// token field logs a warning and leaves the actor unauthenticated so invariants
// that expect rejection can still be exercised.
func (s *Session) resolveActorAuth(ctx context.Context) {
	for i := range s.Config.Actors {
		a := &s.Config.Actors[i]
		if a.Auth == nil {
			continue
		}
		svcURL := s.serviceURLForActor(a)
		name, value, err := agent.ResolveAuthHeader(ctx, svcURL, *a)
		if err != nil {
			s.Logger.Warn("auth flow failed; degrading actor to unauthenticated",
				zap.String("actor", a.Name),
				// Intentionally no token or credential value logged.
				zap.Error(err),
			)
			continue
		}
		if a.Credentials.Headers == nil {
			a.Credentials.Headers = make(map[string]string)
		}
		a.Credentials.Headers[name] = value
		s.Logger.Info("auth flow resolved",
			zap.String("actor", a.Name),
			zap.String("header", name),
			zap.Int("value_len", len(value)),
		)
	}
}

// serviceURLForActor returns the service URL the actor authenticates against:
// the actor's own service if set, else the first configured service.
func (s *Session) serviceURLForActor(a *project.Actor) string {
	if a.Service != "" {
		for _, svc := range s.Config.Services {
			if svc.Name == a.Service {
				return svc.URL
			}
		}
	}
	if len(s.Config.Services) > 0 {
		return s.Config.Services[0].URL
	}
	return ""
}
