package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHealthWarning(t *testing.T) {
	assert.NotEmpty(t, healthWarning(0, 10*time.Second), "activity but zero tokens → warn")
	assert.Empty(t, healthWarning(100, 10*time.Second), "tokens recorded → no warn")
	assert.Empty(t, healthWarning(0, 1*time.Second), "short session → no warn")
}
