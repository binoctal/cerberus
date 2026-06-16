package session

import (
	"go.uber.org/zap"
)

// Close closes the session and logs token usage.
func (s *Session) Close() {
	s.Logger.Info("session closed",
		zap.String("id", s.ID),
		zap.Int("tokens_spent", s.Driver.Budget().SessionTotal-s.Driver.Budget().Remaining()))
}

// resolveBaseURL returns the first service URL from project config, or empty string.
func (s *Session) resolveBaseURL() string {
	if len(s.Config.Services) > 0 {
		return s.Config.Services[0].URL
	}
	return ""
}
