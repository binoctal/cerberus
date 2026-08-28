package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestNeedsRelogin pins the auth-loss detector: a goto that aimed at an
// authenticated page but landed on the login page means the injected session
// expired or was invalidated (run32: the sweep's dev/setup recreated the dev
// user under the browser's token). Same-page navigation and non-login
// destinations never trigger a re-login.
func TestNeedsRelogin(t *testing.T) {
	require.True(t, needsRelogin("http://x:5183/dashboard/devices", "http://x:5183/login"),
		"redirected to login while aiming at a dashboard page")
	require.True(t, needsRelogin("http://x:5183/dashboard", "http://x:5183/login?next=/dashboard"),
		"login page with query still counts")

	require.False(t, needsRelogin("http://x:5183/login", "http://x:5183/login"),
		"an explicit goto of the login page is intentional")
	require.False(t, needsRelogin("http://x:5183/dashboard/devices", "http://x:5183/dashboard/devices"),
		"staying on the target page needs no re-login")
	require.False(t, needsRelogin("http://x:5183/dashboard", ""),
		"no final URL (goto error path) never triggers")
}

// TestReloginRateLimit: a re-login attempt is allowed only after the minimum
// interval elapses — genuinely bad credentials must not turn every UI case
// into a login round-trip.
func TestReloginRateLimit(t *testing.T) {
	var last reloginLimiter
	require.True(t, last.allow(time.Now()), "first attempt allowed")
	require.False(t, last.allow(time.Now().Add(time.Second)), "immediate second attempt blocked")
	require.True(t, last.allow(time.Now().Add(reloginMinInterval)), "allowed again after the interval")
}
