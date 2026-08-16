//go:build integration

package agent

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// m2BridgeSpec names one real bridge and the shim CLI binaries its RESTRICTED
// PATH exposes. The bridge's capability detection is PATH-based
// (cliDetectMap), so the shim set directly determines the cliEnabled map the
// device registers with.
type m2BridgeSpec struct {
	name string
	clis []string // shim binary names (e.g. "claude", "codex")
}

// launchBridgesWithCLIs starts real bridges whose PATH is RESTRICTED to
// <shimDir>:/usr/bin:/bin — the host PATH (which carries the real claude/codex
// on this dev machine) is excluded so each bridge's detected capabilities are
// exactly its shim set. Disjoint sets across bridges are what M2 needs to
// observe capability-MATCHED (not round-robin) scheduling.
func launchBridgesWithCLIs(t *testing.T, specs []m2BridgeSpec) []realBridge {
	t.Helper()
	bridgeAbs, err := filepath.Abs(bridgeRepoDir)
	require.NoError(t, err)
	bin := filepath.Join(bridgeAbs, "build", "open-agents-bridge")
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("bridge binary not built at %s; run `make build` in open-agents/bridge", bin)
	}
	var bridges []realBridge
	for _, spec := range specs {
		shimDir := t.TempDir()
		for _, cli := range spec.clis {
			writeShimCLI(t, shimDir, cli,
				fmt.Sprintf("#!/bin/sh\n# deterministic fake %s shim (M2 capability fabrication)\nwhile IFS= read -r line; do\n  printf 'FAKE_%s_ECHO: %%s\\n' \"$line\"\ndone\n", cli, cli))
		}
		bridges = append(bridges,
			launchOneRealBridgeRestricted(t, bin, bridgeAbs, spec.name, t.TempDir(), shimDir))
	}
	return bridges
}

// launchOneRealBridgeRestricted is launchOneRealBridge with a REPLACEMENT
// PATH (<shimDir>:/usr/bin:/bin) instead of a prefix — host CLI binaries must
// stay invisible for disjoint capability detection.
func launchOneRealBridgeRestricted(t *testing.T, bin, bridgeAbs, name, home, shimDir string) realBridge {
	t.Helper()
	host := os.Getenv("PATH")
	t.Setenv("PATH", shimDir+":/usr/bin:/bin")
	defer t.Setenv("PATH", host)
	return launchOneRealBridge(t, bin, bridgeAbs, name, home, nil)
}

// TestRealBridge_M2_CapabilityMatchedScheduling — M2 of the fidelity ladder:
// with two real bridges reporting DISJOINT capabilities (claude-only vs
// codex-only, via restricted-PATH shims), the orchestrator must route each
// task to the device whose cliEnabled matches the task's required CLI — not
// round-robin. A third task requiring a CLI NO device has must stay
// unassigned (no fallback dispatch).
func TestRealBridge_M2_CapabilityMatchedScheduling(t *testing.T) {
	if !reachable(oaBase) {
		t.Skipf("open-agents not reachable at %s", oaBase)
	}
	d1 := openAgentsD1(t)
	if d1 == nil {
		t.Skip("local D1 sqlite not found under open-agents/apps/api/.wrangler")
	}

	bridges := launchBridgesWithCLIs(t, []m2BridgeSpec{
		{name: "m2-claude", clis: []string{"claude"}},
		{name: "m2-codex", clis: []string{"codex"}},
	})
	bClaude, bCodex := bridges[0], bridges[1]

	userId, _, _, err := devSetup(oaBase)
	require.NoError(t, err)

	_, err = d1.Exec(`UPDATE users SET plan='pro' WHERE id = ?`, userId)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = d1.Exec(`UPDATE users SET plan='free' WHERE id = ?`, userId)
		_, _ = d1.Exec(`DELETE FROM multiagent_tasks WHERE mission_id = ?`, "e2e-m2-mission")
		_, _ = d1.Exec(`DELETE FROM multiagent_missions WHERE id = ?`, "e2e-m2-mission")
	})
	_, _ = d1.Exec(`DELETE FROM multiagent_tasks WHERE mission_id = 'e2e-m2-mission'`)
	_, _ = d1.Exec(`DELETE FROM multiagent_missions WHERE id = 'e2e-m2-mission'`)
	_, err = d1.Exec(`INSERT INTO multiagent_missions
		(id, user_id, title, status, device_ids, input_text, config, created_at, updated_at)
		VALUES (?, ?, 'e2e m2', 'paused', ?, 'seeded', '{}', datetime('now'), datetime('now'))`,
		"e2e-m2-mission", userId, fmt.Sprintf(`["%s","%s"]`, bClaude.deviceId, bCodex.deviceId))
	require.NoError(t, err)

	// Three tasks, type 'generate' (suits BOTH claude and codex per
	// AGENT_PROFILES, so the suitability-reassignment path stays out of the
	// way): two capability-routed, one impossible.
	//   m2-task-claude → required CLI 'claude' → only bClaude has it
	//   m2-task-codex  → required CLI 'codex'  → only bCodex has it
	//   m2-task-gemini → required CLI 'gemini' → nobody has it ⇒ stays pending
	for i, task := range []struct{ id, agent string }{
		{"e2e-m2-task-claude", "claude"},
		{"e2e-m2-task-codex", "codex"},
		{"e2e-m2-task-gemini", "gemini"},
	} {
		_, err = d1.Exec(`INSERT INTO multiagent_tasks
			(id, mission_id, type, title, status, assigned_agent, dependencies, sort_order, created_at, updated_at)
			VALUES (?, ?, 'generate', ?, 'pending', ?, '[]', ?, datetime('now'), datetime('now'))`,
			task.id, "e2e-m2-mission", task.id, task.agent, i)
		require.NoError(t, err)
	}

	// Trigger scheduling through the PRODUCT path (mission resume; the
	// internal alarm endpoints 500 — recorded open-agents finding, see M1).
	trigger := func() {
		req, err := http.NewRequest(http.MethodPost, oaBase+"/api/missions/e2e-m2-mission/resume", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+jwtForDev(t))
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		require.Equal(t, http.StatusOK, resp.StatusCode, "mission resume: %s", strings.TrimSpace(string(body)))
	}
	trigger()

	// Deterministic routing assertions from the task tables: capability must
	// decide the device, not round-robin.
	assignedDevice := func(t *testing.T) map[string]string {
		t.Helper()
		out := map[string]string{}
		rset, err := d1.Query(`SELECT id, coalesce(assigned_device_id, device_id, '')
			FROM multiagent_tasks WHERE mission_id = 'e2e-m2-mission'`)
		require.NoError(t, err)
		defer rset.Close()
		for rset.Next() {
			var id, dev string
			require.NoError(t, rset.Scan(&id, &dev))
			out[id] = dev
		}
		return out
	}
	var routing map[string]string
	require.Eventually(t, func() bool {
		routing = assignedDevice(t)
		return routing["e2e-m2-task-claude"] != "" && routing["e2e-m2-task-codex"] != ""
	}, 20*time.Second, 500*time.Millisecond, "both capability-routable tasks must be assigned (routing: %+v)", routing)

	require.Equal(t, bClaude.deviceId, routing["e2e-m2-task-claude"],
		"the claude task must land on the ONLY claude-capable device (capability-matched, not round-robin)")
	require.Equal(t, bCodex.deviceId, routing["e2e-m2-task-codex"],
		"the codex task must land on the ONLY codex-capable device")

	// The impossible task must NOT be dispatched to any device.
	require.Empty(t, routing["e2e-m2-task-gemini"],
		"a task whose required CLI no device reports must stay unassigned (routing: %+v)", routing)

	fmt.Println("M2 OK: capability-matched routing claude→", bClaude.deviceId, "codex→", bCodex.deviceId)
}
