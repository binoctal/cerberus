package autotest

import "context"

// RequestGate is the gate surface autotest needs: ask (or auto-approve) before a
// destructive write. escalation.Gate is adapted to this in Task 6.
type RequestGate interface {
	Request(ctx context.Context, checkpoint string, files []string, preview string) (bool, error)
}
