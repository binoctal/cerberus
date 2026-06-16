package report

import (
	"fmt"
)

// FormatDuration returns a human-readable duration string.
func FormatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}
