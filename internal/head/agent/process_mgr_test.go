package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestProcessManager_StartStop(t *testing.T) {
	pm := NewProcessManager(zap.NewNop())

	mp := &ManagedProcess{
		Name: "sleeper",
		Cmd:  "sleep",
		Args: []string{"30"},
	}

	err := pm.Start(context.Background(), mp)
	assert.NoError(t, err)
	assert.True(t, mp.running)

	err = pm.Stop("sleeper")
	assert.NoError(t, err)
	assert.False(t, mp.running)
}

func TestProcessManager_StartWithHealth(t *testing.T) {
	// Start a test HTTP server for health checking.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProcessManager(zap.NewNop())

	// Use a process that stays alive while the test runs.
	mp := &ManagedProcess{
		Name:    "health-checked",
		Cmd:     "sleep",
		Args:    []string{"30"},
		Health:  server.URL,
		Timeout: 5 * time.Second,
	}

	err := pm.Start(context.Background(), mp)
	assert.NoError(t, err)

	// Cleanup.
	pm.StopAll()
}

func TestProcessManager_StopAll(t *testing.T) {
	pm := NewProcessManager(zap.NewNop())

	for _, name := range []string{"p1", "p2"} {
		mp := &ManagedProcess{
			Name: name,
			Cmd:  "sleep",
			Args: []string{"30"},
		}
		err := pm.Start(context.Background(), mp)
		assert.NoError(t, err)
	}

	assert.Len(t, pm.processes, 2)
	pm.StopAll()
	assert.Len(t, pm.processes, 0)
}

func TestProcessManager_StopNonExistent(t *testing.T) {
	pm := NewProcessManager(zap.NewNop())
	err := pm.Stop("nonexistent")
	assert.Error(t, err)
}

func TestProcessManager_AlreadyRunning(t *testing.T) {
	pm := NewProcessManager(zap.NewNop())

	mp := &ManagedProcess{Name: "dup", Cmd: "sleep", Args: []string{"30"}}
	err := pm.Start(context.Background(), mp)
	assert.NoError(t, err)

	// Second start should fail.
	err = pm.Start(context.Background(), mp)
	assert.Error(t, err)

	pm.StopAll()
}
