package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDeclaredReceiveBudget(t *testing.T) {
	// The per-case deadlock guard falls back to the configured PerCaseTimeout
	// unless the case's own ws_receive windows sum higher — a stepped case
	// declares its time expectations and the guard must honor them.
	tests := []struct {
		name  string
		steps []TestStep
		want  time.Duration
	}{
		{
			name:  "no steps at all",
			steps: nil,
			want:  0,
		},
		{
			name: "steps but no ws_receive",
			steps: []TestStep{
				{Action: "ws_connect", ConnectionID: "web"},
				{Action: "ws_send", ConnectionID: "web", Message: "{}"},
			},
			want: 0,
		},
		{
			// Explicit windows only: a receive without Timeout runs on the
			// executor's internal default and contributes nothing here, so the
			// guard stays on the configured PerCaseTimeout.
			name: "receives without explicit timeouts contribute nothing",
			steps: []TestStep{
				{Action: "ws_receive", ConnectionID: "web", Type: "chat:response"},
				{Action: "ws_receive", ConnectionID: "web", Type: "session:error"},
			},
			want: 0,
		},
		{
			name: "declared windows below the old fixed guard still sum",
			steps: []TestStep{
				{Action: "ws_connect", ConnectionID: "web"},
				{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_started", Timeout: 30},
				{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_progress", Timeout: 45},
			},
			want: 75 * time.Second,
		},
		{
			name: "mission-scale windows exceed the old fixed guard",
			steps: []TestStep{
				{Action: "http_request", URL: "http://x/api/missions", Method: "POST"},
				{Action: "ws_connect", ConnectionID: "web", Role: "web"},
				{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_progress", Timeout: 300},
				{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_question", Timeout: 120},
			},
			want: 420 * time.Second,
		},
		{
			name: "non-receive timeouts are ignored",
			steps: []TestStep{
				{Action: "ws_receive", ConnectionID: "web", Type: "chat:response", Timeout: 15},
				{Action: "ws_expect_close", ConnectionID: "web", Timeout: 60},
			},
			want: 15 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, declaredReceiveBudget(&TestCase{Steps: tt.steps}))
		})
	}
}
