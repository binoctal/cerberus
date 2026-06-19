package scout

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
)

// isURLTarget reports whether a target is an HTTP target (absolute URL or
// server-relative path), which is not statically validated.
func isURLTarget(t string) bool {
	return strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") || strings.HasPrefix(t, "/")
}

// invalidReason returns a non-empty reason when a test target is clearly
// invalid (too broad, command not in PATH, path not found). Empty when the
// target is valid or unverifiable (URL — the live service may just be down;
// relative path without a project dir).
func invalidReason(target, projectDir string) string {
	t := strings.TrimSpace(target)
	if t == "" || t == "." {
		return "target is empty or too broad"
	}
	if isURLTarget(t) {
		return "" // URL: live service may be down; don't fail planning
	}
	if strings.Contains(t, " ") {
		parts := strings.Fields(t)
		if _, err := exec.LookPath(parts[0]); err != nil {
			return fmt.Sprintf("command %q not found in PATH", parts[0])
		}
		return ""
	}
	path := t
	if !filepath.IsAbs(path) && projectDir != "" {
		path = filepath.Join(projectDir, t)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Sprintf("path %q not found", t)
	}
	return ""
}

// ValidateTargets checks each case's target and deprioritizes (priority 0)
// clearly invalid ones, logging the reason. Called after plan generation to
// avoid wasting Agent effort on bogus targets. Returns the count flagged.
func (s *Scout) ValidateTargets(plan *agent.TestPlan, projectDir string) int {
	if plan == nil {
		return 0
	}
	flagged := 0
	for i := range plan.Cases {
		if reason := invalidReason(plan.Cases[i].Target, projectDir); reason != "" {
			plan.Cases[i].Priority = 0
			flagged++
			s.logger.Warn("case target invalid, deprioritized",
				zap.String("case", plan.Cases[i].ID),
				zap.String("target", plan.Cases[i].Target),
				zap.String("reason", reason))
		}
	}
	return flagged
}
