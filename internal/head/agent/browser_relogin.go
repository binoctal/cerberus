package agent

import (
	"context"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
)

// loginPageMarker is the SPA's unauthenticated redirect target. The web app
// bounces any protected route to /login when localStorage auth is missing or
// rejected.
const loginPageMarker = "/login"

// reloginMinInterval bounds how often a re-login is attempted: with genuinely
// bad credentials every UI case's goto would otherwise become a login
// round-trip.
const reloginMinInterval = 5 * time.Minute

// needsRelogin reports a goto that aimed at an authenticated page but landed
// on the login page — the injected session expired (15-minute token) or was
// invalidated server-side (run32: the sweep's POST /api/dev/setup recreated
// the dev user under the browser's token and every UI assert then timed out
// staring at the login page).
func needsRelogin(targetURL, finalURL string) bool {
	if finalURL == "" {
		return false
	}
	t, terr := url.Parse(targetURL)
	f, ferr := url.Parse(finalURL)
	if terr != nil || ferr != nil {
		return false
	}
	return strings.Contains(f.Path, loginPageMarker) && !strings.Contains(t.Path, loginPageMarker)
}

// reloginLimiter is the once-per-interval gate for re-login attempts. The
// zero value allows the first attempt immediately.
type reloginLimiter struct {
	last time.Time
}

func (r *reloginLimiter) allow(now time.Time) bool {
	if !r.last.IsZero() && now.Sub(r.last) < reloginMinInterval {
		return false
	}
	r.last = now
	return true
}

// browserSessionRecipe stores what a re-login needs: the vocab UI block, the
// auth actor, and the API base the login POST targets.
type browserSessionRecipe struct {
	ui      project.VocabUI
	actor   project.Actor
	apiBase string
}

// maybeRelogin checks the page's final URL after a goto and, when the app
// bounced an authenticated target to the login page, transparently re-runs
// the login + localStorage injection and retries the goto once. Caller must
// hold e.mu.
func (e *BrowserExecutor) maybeRelogin(ctx context.Context, targetURL string) {
	if e.session == nil {
		return // no recipe: InitBrowserSession never succeeded
	}
	final := e.page.URL()
	if !needsRelogin(targetURL, final) {
		return
	}
	if !e.reloginGate.allow(time.Now()) {
		return
	}
	e.logger.Info("browser session lost — re-login",
		zap.String("target", targetURL), zap.String("landed_on", final),
		zap.String("actor", e.session.actor.Name))
	if err := e.injectSessionLocked(ctx, &e.session.ui, e.session.actor, e.session.apiBase); err != nil {
		e.logger.Warn("browser re-login failed", zap.Error(err))
		return
	}
	if _, err := e.page.Goto(targetURL); err != nil {
		e.logger.Warn("browser re-login goto retry failed", zap.Error(err))
	}
}
