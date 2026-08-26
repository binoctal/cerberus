package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
)

// authStorageBlob builds the zustand persist blob the web app hydrates from
// (localStorage key "auth-storage"). Shape contract:
// apps/web/src/stores/authStore.ts partialize — user/token/refreshToken/
// expiresIn/tokenExpiry at state.*, version 0. refreshToken is REQUIRED for
// the WS provider's ensureValidToken gate; without it the app never opens its
// WebSocket and connection-status assertions cannot pass.
func authStorageBlob(token, refreshToken, userID string, expiryMs int64) string {
	blob := map[string]any{
		"state": map[string]any{
			"user":         map[string]any{"id": userID, "email": "dev@openagents.local", "name": "Dev User"},
			"token":        token,
			"refreshToken": refreshToken,
			"expiresIn":    900,
			"tokenExpiry":  expiryMs,
		},
		"version": 0,
	}
	b, _ := json.Marshal(blob)
	return string(b)
}

// InitBrowserSession establishes the run-level logged-in browser session
// (spec §5): resolve the UI actor's credentials, run the UI login when the
// actor carries email/password (the pair response yields token + refreshToken
// + user id), else fall back to the actor's http_login credential (refresh
// token empty — WS-dependent assertions will fail loudly). One bare goto
// bootstraps the origin, then the auth blob + i18n locale key are written
// once; pages opened later share the browser context and thus localStorage.
func (e *BrowserExecutor) InitBrowserSession(ctx context.Context, ui *project.VocabUI, actor project.Actor, apiBaseURL string) error {
	token, refresh := "", ""
	userID := "user_unknown"
	if actor.Credentials.Email != "" && actor.Credentials.Password != "" {
		loginPath := ui.LoginPath
		if loginPath == "" {
			loginPath = "/api/auth/login"
		}
		decoded, err := sendLogin(ctx, apiBaseURL, project.AuthLogin{
			Method:  "POST",
			Path:    loginPath,
			Body:    map[string]string{"email": "{email}", "password": "{password}"},
			Headers: map[string]string{"Origin": apiBaseURL},
		}, map[string]string{
			"{email}":    actor.Credentials.Email,
			"{password}": actor.Credentials.Password,
		})
		if err != nil {
			return fmt.Errorf("ui session login: %w", err)
		}
		if t, err := extractByDotPath(decoded, "token"); err == nil {
			token = t
		}
		if r, err := extractByDotPath(decoded, "refreshToken"); err == nil {
			refresh = r
		}
		if u, err := extractByDotPath(decoded, "user.id"); err == nil && u != "" {
			userID = u
		}
	} else if ar, err := ResolveAuthHeader(ctx, apiBaseURL, actor); err == nil && ar != nil {
		token = ar.HTTPToken // no refresh token on this path
	}
	if token == "" {
		return fmt.Errorf("ui session: no token resolved for actor %q", actor.Name)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.page.Goto(ui.BaseURL); err != nil {
		return fmt.Errorf("session bootstrap goto: %w", err)
	}
	expiry := time.Now().Add(15 * time.Minute).UnixMilli()
	script := fmt.Sprintf(
		`localStorage.setItem('auth-storage', %q); localStorage.setItem('i18nextLng', %q); undefined`,
		authStorageBlob(token, refresh, userID, expiry), ui.Locale)
	if _, err := e.page.Evaluate(script); err != nil {
		return fmt.Errorf("session injection: %w", err)
	}
	e.logger.Info("browser session injected", zap.String("base_url", ui.BaseURL),
		zap.String("locale", ui.Locale), zap.Bool("refresh_token", refresh != ""))
	return nil
}
