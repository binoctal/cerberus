package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// A timed-out stdio read must evict the conn from the cache so the next call
// dials a fresh subprocess. Otherwise the leaked reader goroutine (still
// blocked on conn.stdout) races the next call's reader on the same bufio.Reader.
func TestSendStdioEvictsConnOnReadTimeout(t *testing.T) {
	e := NewMCPExecutor(nil, zap.NewNop())
	e.readTimeout = 100 * time.Millisecond // override the 10s default
	t.Cleanup(e.Close)

	ep := MCPEndpoint{Name: "slow", Command: "sleep", Args: []string{"30"}}
	_, err := e.sendStdio(context.Background(), ep, []byte("{}"))
	require.Error(t, err, "expected read timeout")

	e.mu.Lock()
	_, cached := e.stdioProcesses["slow"]
	e.mu.Unlock()
	assert.False(t, cached,
		"timed-out stdio conn must be evicted to avoid a reader race on reuse")
}
