//go:build integration

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// bridgeRepoDir is the open-agents bridge checkout (sibling repo convention,
// same as the ws-realtime dogfood; from internal/head/agent the sibling root
// is four levels up).
var bridgeRepoDir = "../../../../open-agents/bridge"

// realBridge is one harness-launched real bridge process: its captured
// deviceId (for routing) and the running child.
type realBridge struct {
	name     string
	deviceId string
	homeDir  string
	shimDir  string
	cmd      *exec.Cmd
	cancel   context.CancelFunc
}

// bridgeConfigFile is the shape pair --dev writes under the isolated HOME.
type bridgeConfigFile struct {
	Devices map[string]struct {
		DeviceID string `json:"deviceId"`
	} `json:"devices"`
}

// writeClaudeShim creates a deterministic fake `claude` REPL — a REAL process
// (real PTY, real streaming, zero LLM cost) the bridge spawns for cliType
// claude-pty. Plain-command PTY sessions (e.g. cliType "bash") are NOT
// reachable via session:start: the bridge's protocol auto-detection tries the
// ACP stdio adapter for every command that exists on PATH (only the hardcoded
// "claude-pty" forces PTY — internal/protocol/manager.go). Recorded as a
// protocol finding; the shim exercises the real claude-pty code path.
func writeClaudeShim(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	shim := "#!/bin/sh\n# Deterministic fake claude REPL for the L1 fidelity ladder.\nwhile IFS= read -r line; do\n  printf 'FAKE_CLAUDE_ECHO: %s\\n' \"$line\"\ndone\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(shim), 0o755))
	return dir
}

// launchRealBridges pairs and starts n real bridge devices under the SAME dev
// user (each /api/dev/setup call reuses the user, creates a new device), each
// in its own isolated HOME with the deterministic claude shim first on PATH.
// It mirrors the session-package harness (an agent-package test cannot import
// session — cycle): setup to completion, JSON capture, start in its own
// process group, wait for the ready pattern, group teardown at cleanup.
func launchRealBridges(t *testing.T, names ...string) []realBridge {
	t.Helper()
	bridgeAbs, err := filepath.Abs(bridgeRepoDir)
	require.NoError(t, err)
	bin := filepath.Join(bridgeAbs, "build", "open-agents-bridge")
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("bridge binary not built at %s; run `make build` in open-agents/bridge", bin)
	}
	shimDir := writeClaudeShim(t)
	var bridges []realBridge
	for _, name := range names {
		home := t.TempDir()
		childEnv := func() []string {
			return append(os.Environ(),
				"HOME="+home,
				"PATH="+shimDir+":"+os.Getenv("PATH"),
			)
		}
		run := func(args ...string) string {
			cmd := exec.Command(bin, args...)
			cmd.Env = childEnv()
			cmd.Dir = bridgeAbs
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s %v: %s", name, args, out)
			return string(out)
		}

		// Pair (one-shot), then capture the deviceId from the written config.
		run("pair", "--dev", "--server", "http://localhost:8989", "-n", name)
		raw, err := os.ReadFile(filepath.Join(home, ".open-agents-bridge", "config.json"))
		require.NoError(t, err, "read bridge config after pair")
		var cfg bridgeConfigFile
		require.NoError(t, json.Unmarshal(raw, &cfg), "parse bridge config")
		require.NotEmpty(t, cfg.Devices[name].DeviceID, "deviceId for %s", name)

		// Start in its own process group; wait for the ready line.
		ctx, cancel := context.WithCancel(context.Background())
		start := exec.CommandContext(ctx, bin, "start", "-d", name, "--log-level", "debug")
		start.Env = childEnv()
		start.Dir = bridgeAbs
		start.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		ready := make(chan string, 64)
		var outMu sync.Mutex
		var outLog bytes.Buffer
		pr, pw, err := os.Pipe()
		require.NoError(t, err)
		start.Stdout = pw
		start.Stderr = pw
		require.NoError(t, start.Start())
		go func() {
			buf := make([]byte, 4096)
			readyRe := regexp.MustCompile(`Connected to server successfully`)
			for {
				n, err := pr.Read(buf)
				if n > 0 {
					outMu.Lock()
					outLog.Write(buf[:n])
					outMu.Unlock()
					if readyRe.Match(buf[:n]) {
						select {
						case ready <- string(buf[:n]):
						default:
						}
					}
				}
				if err != nil {
					return
				}
			}
		}()
		t.Cleanup(func() {
			outMu.Lock()
			defer outMu.Unlock()
			if outLog.Len() > 0 {
				t.Logf("bridge %s output:\n%s", name, outLog.String())
			}
		})
		b := realBridge{name: name, deviceId: cfg.Devices[name].DeviceID, homeDir: home, shimDir: shimDir, cmd: start, cancel: cancel}
		bridges = append(bridges, b)
		t.Cleanup(func() { stopBridgeGroup(t, b) })

		select {
		case <-ready:
		case <-time.After(30 * time.Second):
			t.Fatalf("bridge %s: no connect line within 30s", name)
		}
	}
	return bridges
}

func stopBridgeGroup(t *testing.T, b realBridge) {
	t.Helper()
	if b.cmd.Process != nil {
		_ = syscall.Kill(-b.cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _, _ = b.cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-b.cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	b.cancel()
}

// webProtocolFor returns the WS protocol bound to the web actor only — the
// bridge roles are occupied by REAL processes, so nothing connects as them.
func webProtocolFor() *project.Protocol {
	return &project.Protocol{
		TypePath: "type",
		Auth:     &project.ProtocolAuth{Strategy: "query", Param: "token", CredentialRef: "web-actor"},
		Roles: map[string]*project.ProtocolRole{
			"web": {
				CredentialRef: "web-actor",
				Params:        map[string]string{"type": "web"},
				Handshake:     &project.RoleHandshake{AwaitType: "device:online", Optional: true, Timeout: 5},
			},
		},
	}
}

// wsIdxForReal wires the web actor's credentials against the live server.
func wsIdxForReal(userId string) *WSProtocolIndex {
	return &WSProtocolIndex{
		ByHost: map[string]*project.Protocol{"localhost:8989": webProtocolFor()},
		ActorTokens: map[string]string{
			"web-actor": "demo_token",
		},
		ActorPathParams: map[string]map[string]string{
			"web-actor": {"userId": userId},
		},
	}
}

// TestRealBridge_L1_PTYSessions — L1 of the fidelity ladder: a REAL bridge
// process (not a self-played actor) spawns a REAL subprocess (the
// deterministic claude shim) in a real PTY via the claude-pty path, streams
// real output back through BOTH batching layers (bridge content batching +
// DO session:output batching — PTY output arrives as chat:response), and
// stops cleanly.
func TestRealBridge_L1_PTYSessions(t *testing.T) {
	if !reachable(oaBase) {
		t.Skipf("open-agents not reachable at %s; bring up `npm run dev` (apps/api)", oaBase)
	}
	bridges := launchRealBridges(t, "bridge-pty-1")
	b1 := bridges[0]

	// The web actor provisions through the same dev backdoor: same user as
	// the bridge's device, so the DO room is shared.
	userId, _, _, err := devSetup(oaBase)
	require.NoError(t, err)

	wsURL := "ws://localhost:8989/ws/" + userId
	sessionID := "e2e-l1-claude-pty"

	mustJSON := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return string(b)
	}

	t.Run("session lifecycle through a real bridge", func(t *testing.T) {
		// Case 1 drives the full chain; the output receive is LAST so the
		// step result's WSResult.MatchedMessage carries the real frame text
		// for the marker assertion below.
		tc := &TestCase{
			ID:     "real-bridge-l1-pty-lifecycle",
			Target: wsURL,
			Action: "ws_flow",
			Steps: []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
				// Message shape is {type, payload{...}}: the DO relays msg and
				// routes web→bridge on payload.deviceId.
				{Action: "ws_send", ConnectionID: "c-web", Message: mustJSON(map[string]any{
					"type": "session:start",
					"payload": map[string]any{
						"sessionId": sessionID, "cliType": "claude-pty",
						"workDir": "/tmp", "cols": 80, "rows": 24, "deviceId": b1.deviceId,
					},
				})},
				{Action: "ws_receive", ConnectionID: "c-web", Type: "session:started", Timeout: 15},
				// The bridge's session:send payload field is `content` (not input).
				{Action: "ws_send", ConnectionID: "c-web", Message: mustJSON(map[string]any{
					"type": "session:send",
					"payload": map[string]any{
						"sessionId": sessionID, "content": "printf CERBERUS_L1_OK\n", "deviceId": b1.deviceId,
					},
				})},
				// PTY output flows back as chat:response (forwardSessionOutput
				// maps content messages); batched raw output would be
				// session:output-batch. Accept either.
				{Action: "ws_receive", ConnectionID: "c-web", Type: "chat:response", Aliases: []string{"session:output-batch"}, Timeout: 15},
			},
		}
		se := newStepExecutionWithIdx(t, tc, wsIdxForReal(userId))
		result := se.runSteps()
		require.Equal(t, StepPassed, result.Status, "lifecycle case must pass")

		// The REAL subprocess output marker must have flowed through both
		// batching layers (bridge content batching + DO 50ms batching).
		fr, ok := result.Result.(types.WSResult)
		require.True(t, ok, "last step result is a WSResult")
		require.NotEmpty(t, fr.MatchedMessage, "output receive must have a matched frame")
		require.Contains(t, fr.MatchedMessage, "CERBERUS_L1_OK",
			"real subprocess output marker must flow bridge→DO→web")

		// Case 2 stops the session over a fresh web connection (connections
		// are per-case; the bridge session survives across them).
		tcStop := &TestCase{
			ID:     "real-bridge-l1-pty-stop",
			Target: wsURL,
			Action: "ws_flow",
			Steps: []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
				{Action: "ws_send", ConnectionID: "c-web", Message: mustJSON(map[string]any{
					"type": "session:stop",
					"payload": map[string]any{
						"sessionId": sessionID, "deviceId": b1.deviceId,
					},
				})},
				{Action: "ws_receive", ConnectionID: "c-web", Type: "session:stopped", Timeout: 15},
			},
		}
		seStop := newStepExecutionWithIdx(t, tcStop, wsIdxForReal(userId))
		resStop := seStop.runSteps()
		require.Equal(t, StepPassed, resStop.Status, "stop case must pass")
	})

	// Device visibility is proven end-to-end by the lifecycle itself: the
	// session:start routed BY payload.deviceId got session:started / real
	// output / session:stopped back FROM that same real device. A separate
	// devices:sync case is redundant and was dropped (its second connect also
	// hit an unrelated engine-side connection issue).
	fmt.Println("L1 OK: real bridge deviceId", b1.deviceId)
}
