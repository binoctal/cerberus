package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCaseServiceFieldRoundTrip(t *testing.T) {
	src := `{
		"id": "tc-001",
		"name": "Test Create User",
		"target": "/api/v1/users",
		"method": "POST",
		"action": "create user",
		"service": "api-gateway"
	}`
	var tc TestCase
	err := json.Unmarshal([]byte(src), &tc)
	require.NoError(t, err)
	require.Equal(t, "api-gateway", tc.Service)
}
