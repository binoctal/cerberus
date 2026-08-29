package agent

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// TestWSProtocolIndex_SetActorHTTPToken pins the mid-run token refresh seam:
// the SUT's access tokens expire in 15 minutes while a sweep runs for hours,
// so a background refresher must be able to rotate an actor's HTTP token in
// the live index and every later http_request step must pick up the new
// value (run33: 119 "Invalid token" 401 verdicts from the stale token).
func TestWSProtocolIndex_SetActorHTTPToken(t *testing.T) {
	cfg := &project.Config{
		Services: []project.Service{{Name: "api", URL: "http://localhost:8989",
			Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
				"web": {CredentialRef: "web-actor"},
			}}}},
		Actors: []project.Actor{{Name: "web-actor",
			Credentials: project.CredentialRef{RawHTTPToken: "JWT-1"}}},
	}
	idx := BuildWSProtocolIndex(cfg)
	step := TestStep{Action: "http_request", URL: "http://localhost:8989/x", AuthRole: "web"}

	a, err := resolveHTTPStep(idx, step)
	require.NoError(t, err)
	require.Equal(t, "Bearer JWT-1", a.(types.HTTPAction).Headers["Authorization"])

	// The refresh: same index, rotated token, no rebuild.
	idx.SetActorHTTPToken("web-actor", "JWT-2")
	a, err = resolveHTTPStep(idx, step)
	require.NoError(t, err)
	require.Equal(t, "Bearer JWT-2", a.(types.HTTPAction).Headers["Authorization"],
		"a later step must inject the rotated token")

	// An empty rotation is ignored — a failed re-login must not blank a
	// working token.
	idx.SetActorHTTPToken("web-actor", "")
	a, err = resolveHTTPStep(idx, step)
	require.NoError(t, err)
	require.Equal(t, "Bearer JWT-2", a.(types.HTTPAction).Headers["Authorization"])
}
