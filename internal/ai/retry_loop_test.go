package ai

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestWithJitter_EqualJitterRange verifies equal jitter: the result is always
// in [d/2, d) — at least half the delay is preserved, and the full delay is
// never reached (the whole point of jitter is to avoid every concurrent retry
// waiting the exact same interval and hammering the rate limit again).
func TestWithJitter_EqualJitterRange(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	d := 1000 * time.Millisecond
	for i := 0; i < 200; i++ {
		got := withJitter(d, r)
		assert.GreaterOrEqual(t, int64(got), int64(d/2), "jittered delay below half")
		assert.Less(t, int64(got), int64(d), "jittered delay not below full delay")
	}
}

func TestWithJitter_VariedAcrossSeeds(t *testing.T) {
	d := 1000 * time.Millisecond
	a := withJitter(d, rand.New(rand.NewSource(1)))
	b := withJitter(d, rand.New(rand.NewSource(2)))
	assert.NotEqual(t, a, b, "jitter must vary with seed")
}

func TestWithJitter_NilRandReturnsUntouched(t *testing.T) {
	d := 1000 * time.Millisecond
	assert.Equal(t, d, withJitter(d, nil))
}
