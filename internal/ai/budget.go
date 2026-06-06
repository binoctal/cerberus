package ai

import "sync/atomic"

type TokenBudget struct {
	SessionTotal int
	PerCallLimit int
	spent        atomic.Int64
}

func NewTokenBudget(sessionTotal, perCallLimit int) *TokenBudget {
	return &TokenBudget{
		SessionTotal: sessionTotal,
		PerCallLimit: perCallLimit,
	}
}

func (b *TokenBudget) Remaining() int {
	return b.SessionTotal - int(b.spent.Load())
}

func (b *TokenBudget) Record(tokens int) {
	b.spent.Add(int64(tokens))
}

func (b *TokenBudget) CanSpend(tokens int) bool {
	if tokens > b.PerCallLimit {
		return false
	}
	return tokens <= b.Remaining()
}

func (b *TokenBudget) Exhausted() bool {
	return b.Remaining() <= 0
}
