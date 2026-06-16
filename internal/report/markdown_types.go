package report

import (
	"github.com/binoctal/cerberus/internal/store"
)

// FailureInfo holds failure count grouped by reason.
type FailureInfo struct {
	Reason store.FailureReason
	Count  int
}
