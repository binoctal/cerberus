package session

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/authdiscover"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// resolveActorAuth runs each actor's declarative auth flow once and writes the
// resulting header into that actor's Credentials.Headers. After this runs, the
// existing static-header injection path (authHeadersFor / withActorHeaders)
// carries the dynamic token with no further changes.
//
// When an actor has no auth: block and settings.auth.discover_fallback is on,
// an AuthFlow is discovered in-memory (never persisted) and used for this
// session only. Failures degrade, never abort.
func (s *Session) resolveActorAuth(ctx context.Context) {
	for i := range s.Config.Actors {
		a := &s.Config.Actors[i]
		if a.Auth == nil {
			if !s.Config.Settings.Auth.DiscoverFallback || s.Driver == nil {
				continue
			}
			if err := s.discoverActorAuth(ctx, a); err != nil {
				if errors.Is(err, authdiscover.ErrNoAuthFlow) {
					s.Logger.Info("no auth flow found for actor; staying unauthenticated",
						zap.String("actor", a.Name),
					)
				} else {
					s.Logger.Warn("auth discovery fallback failed; degrading actor to unauthenticated",
						zap.String("actor", a.Name),
						zap.Error(err),
					)
				}
				continue
			}
		}
		svcURL := s.serviceURLForActor(a)
		res, err := agent.ResolveAuthHeader(ctx, svcURL, *a)
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
		a.Credentials.Headers[res.HeaderName] = res.HeaderValue
		a.Credentials.RawToken = res.RawToken
		a.Credentials.RawHTTPToken = res.HTTPToken
		a.Credentials.PathParams = res.PathParams // F3: url-param -> captured value
		s.Logger.Info("auth flow resolved",
			zap.String("actor", a.Name),
			zap.String("header", res.HeaderName),
			zap.Int("value_len", len(res.HeaderValue)),
		)
	}
	// Generated path params are independent of the auth flow (a no-auth actor may
	// still need a client-generated id), so they get their own pass AFTER auth
	// resolution — generated values merge into whatever PathParams already exist.
	s.resolveGeneratedPathParams()
}

// resolveGeneratedPathParams synthesizes runtime values (e.g. uuid) for every
// actor that declares generated_path_params, merging them into
// Credentials.PathParams so the WS connect layer templates {name} placeholders
// in the service URL. Runs for ALL actors regardless of auth. An unknown
// generator should never reach here (config validation guards it); if it does,
// the param is left unresolved and resolveURLParams errors at connect.
func (s *Session) resolveGeneratedPathParams() {
	for i := range s.Config.Actors {
		a := &s.Config.Actors[i]
		if len(a.GeneratedPathParams) == 0 {
			continue
		}
		resolved, err := project.ResolveGeneratedPathParams(a.GeneratedPathParams)
		if err != nil {
			s.Logger.Warn("generated path params failed; degrading",
				zap.String("actor", a.Name),
				zap.Error(err),
			)
			continue
		}
		if a.Credentials.PathParams == nil {
			a.Credentials.PathParams = make(map[string]string)
		}
		for name, val := range resolved {
			a.Credentials.PathParams[name] = val
		}
	}
}

// discoverActorAuth infers an AuthFlow for an actor with no auth: block and
// sets it on the in-memory config (NEVER persisted to project.yaml). On
// success the caller proceeds through ResolveAuthHeader as if the block had
// been configured. Credential values are never placed in the prompt
// (authdiscover guarantee).
func (s *Session) discoverActorAuth(ctx context.Context, a *project.Actor) error {
	svcURL := s.serviceURLForActor(a)
	af, err := authdiscover.Discover(ctx, s.Driver, s.Config, a.Name, svcURL)
	if err != nil {
		return err
	}
	a.Auth = af
	s.Logger.Info("auth discovered for session only; persist with `cerberus auth discover --actor "+a.Name+"`",
		zap.String("actor", a.Name),
	)
	return nil
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
