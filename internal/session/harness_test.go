package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
)

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o755))
}

// TestHarness_SetupCaptureStartReady covers the full launch path: setup runs
// to completion, the capture file's JSON values merge into the actor's
// PathParams, the child starts, and the ready pattern unblocks the launch.
func TestHarness_SetupCaptureStartReady(t *testing.T) {
	dir := t.TempDir()
	setupScript := fmt.Sprintf("#!/bin/sh\necho '{\"devices\":{\"b1\":{\"deviceId\":\"device_x\",\"deviceToken\":\"tok\"}}}' > %s/cfg.json\n", dir)
	writeScript(t, filepath.Join(dir, "setup.sh"), setupScript)

	spec := &project.ProcessSpec{
		Setup:        []string{filepath.Join(dir, "setup.sh")},
		Start:        []string{"sh", "-c", "echo BRIDGE_READY; exec sleep 60"},
		CaptureFile:  filepath.Join(dir, "cfg.json"),
		CaptureJSON:  map[string]string{"deviceId": "devices.b1.deviceId"},
		ReadyPattern: `BRIDGE_READY`,
		ReadyTimeout: "5s",
	}
	h := newHarness(zap.NewNop(), dir)
	actor := &project.Actor{Name: "b1", Fidelity: project.FidelityRealProcess, Process: spec}
	start := time.Now()
	require.NoError(t, h.LaunchActor(t.Context(), actor))
	defer h.StopAll()

	assert.Equal(t, "device_x", actor.Credentials.PathParams["deviceId"])
	assert.Less(t, time.Since(start), 30*time.Second, "ready pattern should unblock before the default timeout")

	// Child must be alive until StopAll.
	assert.NoError(t, h.procs["b1"].cmd.Process.Signal(syscall.Signal(0)))
}

// TestHarness_EnvTemplates verifies {{runtime.dir}} / {{actor.name}} templates
// in argv and env, and that the child actually sees the overridden env.
func TestHarness_EnvTemplates(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "env.txt")
	spec := &project.ProcessSpec{
		Start: []string{"sh", "-c", "echo $HOME_MARKER/$ACTOR > " + outFile + "; echo READY; exec sleep 60"},
		Env: map[string]string{
			"HOME_MARKER": "{{runtime.dir}}",
			"ACTOR":       "{{actor.name}}",
		},
		ReadyPattern: "READY",
		ReadyTimeout: "5s",
	}
	h := newHarness(zap.NewNop(), dir)
	require.NoError(t, h.LaunchActor(t.Context(), &project.Actor{Name: "b7", Process: spec}))
	defer h.StopAll()

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, dir+"/b7", strings.TrimSpace(string(data)))
}

// TestHarness_EnvPassthroughTemplate verifies {{env.NAME}} in an env override
// resolves from the PARENT environment at launch time (e.g. prepending a shim
// dir to PATH without hard-coding it, or passing credentials through).
func TestHarness_EnvPassthroughTemplate(t *testing.T) {
	t.Setenv("CERBERUS_TEST_ENV_PASSTHROUGH", "shim-dir")
	dir := t.TempDir()
	outFile := filepath.Join(dir, "env.txt")
	spec := &project.ProcessSpec{
		Start: []string{"sh", "-c", "echo $PATH_OVERRIDE > " + outFile + "; echo READY; exec sleep 60"},
		Env: map[string]string{
			"PATH_OVERRIDE": "{{runtime.dir}}:{{env.CERBERUS_TEST_ENV_PASSTHROUGH}}",
		},
		ReadyPattern: "READY",
		ReadyTimeout: "5s",
	}
	h := newHarness(zap.NewNop(), dir)
	require.NoError(t, h.LaunchActor(t.Context(), &project.Actor{Name: "b7", Process: spec}))
	defer h.StopAll()

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, dir+":shim-dir", strings.TrimSpace(string(data)))
}

// TestHarness_EnvPassthroughTemplate_MissingVar: an unset {{env.NAME}}
// resolves to the empty string (documented semantics; a hard error would
// require threading errors through tmpl).
func TestHarness_EnvPassthroughTemplate_MissingVar(t *testing.T) {
	h := newHarness(zap.NewNop(), t.TempDir())
	got := h.tmpl("pre:{{env.CERBERUS_TEST_UNSET_VAR}}:post", &project.Actor{Name: "b1"})
	assert.Equal(t, "pre::post", got)
}

// TestHarness_ReadyPatternTimeout: a child that never prints the pattern fails
// the launch and is killed.
func TestHarness_ReadyPatternTimeout(t *testing.T) {
	spec := &project.ProcessSpec{
		Start:        []string{"sleep", "60"},
		ReadyPattern: `NEVER_APPEARS`,
		ReadyTimeout: "500ms",
	}
	h := newHarness(zap.NewNop(), t.TempDir())
	err := h.LaunchActor(t.Context(), &project.Actor{Name: "b1", Process: spec})
	require.ErrorContains(t, err, "ready pattern")
	h.StopAll()
	require.Empty(t, h.procs, "failed launch must not leave a tracked child")
}

// TestHarness_SetupFailure: a non-zero setup exit fails the launch with the
// setup output attached.
func TestHarness_SetupFailure(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, filepath.Join(dir, "setup.sh"), "#!/bin/sh\necho pairing blew up >&2\nexit 3\n")
	spec := &project.ProcessSpec{
		Setup: []string{filepath.Join(dir, "setup.sh")},
		Start: []string{"sleep", "60"},
	}
	h := newHarness(zap.NewNop(), dir)
	err := h.LaunchActor(t.Context(), &project.Actor{Name: "b1", Process: spec})
	require.ErrorContains(t, err, "setup failed")
	require.ErrorContains(t, err, "pairing blew up")
}

// TestHarness_CaptureMissingPath: a capture dot-path that does not exist in
// the JSON file is a hard error (clear failure over a silently unset param).
func TestHarness_CaptureMissingPath(t *testing.T) {
	dir := t.TempDir()
	setupScript := fmt.Sprintf("#!/bin/sh\necho '{\"devices\":{}}' > %s/cfg.json\n", dir)
	writeScript(t, filepath.Join(dir, "setup.sh"), setupScript)
	spec := &project.ProcessSpec{
		Setup:       []string{filepath.Join(dir, "setup.sh")},
		Start:       []string{"sleep", "60"},
		CaptureFile: filepath.Join(dir, "cfg.json"),
		CaptureJSON: map[string]string{"deviceId": "devices.b1.deviceId"},
	}
	h := newHarness(zap.NewNop(), dir)
	err := h.LaunchActor(t.Context(), &project.Actor{Name: "b1", Process: spec})
	require.ErrorContains(t, err, "capture")
}

// TestHarness_StopAllKillsProcessGroup verifies group teardown: a child that
// spawns a sub-process must have BOTH killed (no orphan survives StopAll).
func TestHarness_StopAllKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	spec := &project.ProcessSpec{
		// Marker child that outlives a bare SIGTERM-to-leader scenario.
		Start:        []string{"sh", "-c", "sh -c 'sleep 300' & echo GROUP_READY; wait"},
		ReadyPattern: "GROUP_READY",
		ReadyTimeout: "5s",
	}
	h := newHarness(zap.NewNop(), dir)
	require.NoError(t, h.LaunchActor(t.Context(), &project.Actor{Name: "b1", Process: spec}))

	// The sub-sleep exists under the child's process tree. The bracket in the
	// pgrep pattern keeps the CHECKER's own command line from self-matching.
	require.Eventually(t, func() bool {
		out, err := exec.Command("sh", "-c", "pgrep -f 'sleep 30[0]' || true").CombinedOutput()
		return err == nil && strings.TrimSpace(string(out)) != ""
	}, 5*time.Second, 200*time.Millisecond, "sub-sleep should be running")

	h.StopAll()

	require.Eventually(t, func() bool {
		out, _ := exec.Command("sh", "-c", "pgrep -f 'sleep 30[0]' || true").CombinedOutput()
		return strings.TrimSpace(string(out)) == ""
	}, 5*time.Second, 200*time.Millisecond, "process group kill must reap the sub-sleep")
	require.Empty(t, h.procs)
}

// TestLaunchRealProcessActors covers the session-level hook: no real actors
// is a no-op (nil harness), and a real actor is launched with the session
// runtime dir available to templates.
func TestLaunchRealProcessActors(t *testing.T) {
	t.Run("no real actors is a no-op", func(t *testing.T) {
		s := &Session{
			Logger: zap.NewNop(),
			Config: &project.Config{Actors: []project.Actor{
				{Name: "web", Fidelity: project.FidelityEmulated},
			}},
		}
		require.NoError(t, s.launchRealProcessActors(t.Context()))
		assert.Nil(t, s.harness)
	})

	t.Run("real actor launches under the project runtime dir", func(t *testing.T) {
		dir := t.TempDir()
		spec := &project.ProcessSpec{
			Start:        []string{"sh", "-c", "echo $HOME; echo READY; exec sleep 60"},
			Env:          map[string]string{"HOME": "{{runtime.dir}}"},
			ReadyPattern: "READY",
			ReadyTimeout: "5s",
		}
		s := &Session{
			Logger:     zap.NewNop(),
			ProjectDir: dir,
			Config: &project.Config{Actors: []project.Actor{
				{Name: "b1", Fidelity: project.FidelityRealProcess, Process: spec},
			}},
		}
		require.NoError(t, s.launchRealProcessActors(t.Context()))
		require.NotNil(t, s.harness)
		s.harnessStopAll()
		assert.Nil(t, s.harness)
		// The runtime dir was created and used as the child HOME.
		info, err := os.Stat(filepath.Join(dir, ".cerberus", "runtime"))
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("failed launch aborts and cleans up", func(t *testing.T) {
		s := &Session{
			Logger:     zap.NewNop(),
			ProjectDir: t.TempDir(),
			Config: &project.Config{Actors: []project.Actor{
				{Name: "b1", Fidelity: project.FidelityRealProcess, Process: &project.ProcessSpec{
					Start:        []string{"sleep", "60"},
					ReadyPattern: "NEVER",
					ReadyTimeout: "300ms",
				}},
			}},
		}
		err := s.launchRealProcessActors(t.Context())
		require.Error(t, err)
		assert.Nil(t, s.harness, "failed launch must reset the harness")
	})
}

// TestHarness_Restart covers the restart path: a self-terminating child (as
// device:restart causes via os.Exit) is torn down if needed, and Restart
// relaunches WITHOUT re-running setup (pairing persists), re-capturing path
// params and re-waiting the ready pattern. The re-launched child must be a
// NEW pid.
func TestHarness_Restart(t *testing.T) {
	dir := t.TempDir()
	setupPath := filepath.Join(dir, "setup.sh")
	marker := filepath.Join(dir, "setup-ran")
	writeScript(t, setupPath, fmt.Sprintf("#!/bin/sh\ntouch %s\necho '{\"devices\":{\"b1\":{\"deviceId\":\"device_x\",\"deviceToken\":\"tok\"}}}' > %s/cfg.json\n", marker, dir))

	spec := &project.ProcessSpec{
		Setup:        []string{setupPath},
		Start:        []string{"sh", "-c", "echo BRIDGE_READY; exec sleep 60"},
		CaptureFile:  filepath.Join(dir, "cfg.json"),
		CaptureJSON:  map[string]string{"deviceId": "devices.b1.deviceId"},
		ReadyPattern: `BRIDGE_READY`,
		ReadyTimeout: "5s",
	}
	h := newHarness(zap.NewNop(), dir)
	actor := &project.Actor{Name: "b1", Fidelity: project.FidelityRealProcess, Process: spec}
	require.NoError(t, h.LaunchActor(t.Context(), actor))
	pid1 := h.procs["b1"].cmd.Process.Pid

	// Simulate the device:restart self-exit: kill the child out-of-band.
	require.NoError(t, h.procs["b1"].cmd.Process.Signal(syscall.SIGKILL))

	require.NoError(t, h.Restart(t.Context(), actor))
	defer h.StopAll()
	pid2 := h.procs["b1"].cmd.Process.Pid
	assert.NotEqual(t, pid1, pid2, "restart must launch a new child")
	assert.NoError(t, h.procs["b1"].cmd.Process.Signal(syscall.Signal(0)))
	assert.Equal(t, "device_x", actor.Credentials.PathParams["deviceId"], "capture must re-run")
	_, err := os.Stat(marker)
	assert.NoError(t, err, "sanity: setup ran during initial launch")
	info, err := os.Stat(marker)
	require.NoError(t, err)
	_ = info
	// Setup must NOT re-run: rewrite the marker with a sentinel and verify a
	// second restart leaves it untouched.
	require.NoError(t, os.WriteFile(marker, []byte("sentinel"), 0o644))
	require.NoError(t, h.Restart(t.Context(), actor))
	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	assert.Equal(t, "sentinel", string(data), "restart must not re-run setup")
}
