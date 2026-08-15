package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
)

// harness manages real-process actors (fidelity: real-process) for one
// session. For each such actor it runs the declared setup command to
// completion, reads capture values out of a JSON file into the actor's
// runtime PathParams, starts the long-running child in its own process
// group, and waits for a readiness pattern on its combined output. StopAll
// tears every child process group down (SIGTERM, grace, SIGKILL).
//
// cerberus stays SUT-generic: every SUT-specific fact (pairing command,
// config file shape, ready line) lives in the project.yaml process block.
type harness struct {
	log     *zap.Logger
	runtime string // {{runtime.dir}} — the session runtime dir

	mu    sync.Mutex
	procs map[string]*harnessProc
}

type harnessProc struct {
	name   string
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func newHarness(log *zap.Logger, runtimeDir string) *harness {
	return &harness{log: log, runtime: runtimeDir, procs: map[string]*harnessProc{}}
}

// envPlaceholderRe matches {{env.NAME}} — parent-environment passthrough in
// argv and env entries (e.g. prepending a shim dir to PATH, or credentials).
var envPlaceholderRe = regexp.MustCompile(`\{\{env\.([A-Za-z_][A-Za-z0-9_]*)\}\}`)

// tmpl resolves {{runtime.dir}} / {{actor.name}} / {{env.NAME}} in one argv or
// env entry. An unset {{env.NAME}} resolves to the empty string.
func (h *harness) tmpl(s string, actor *project.Actor) string {
	r := strings.ReplaceAll(s, "{{runtime.dir}}", h.runtime)
	r = strings.ReplaceAll(r, "{{actor.name}}", actor.Name)
	return envPlaceholderRe.ReplaceAllStringFunc(r, func(m string) string {
		return os.Getenv(m[len("{{env.") : len(m)-len("}}")])
	})
}

func (h *harness) tmplSlice(ss []string, actor *project.Actor) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = h.tmpl(s, actor)
	}
	return out
}

// childEnv layers the spec's templated env overrides onto the parent env.
func (h *harness) childEnv(spec *project.ProcessSpec, actor *project.Actor) []string {
	if len(spec.Env) == 0 {
		return os.Environ()
	}
	overrides := make(map[string]string, len(spec.Env))
	for k, v := range spec.Env {
		overrides[k] = h.tmpl(v, actor)
	}
	env := os.Environ()
	out := make([]string, 0, len(env)+len(overrides))
	for _, kv := range env {
		key := kv
		if i := strings.IndexByte(kv, '='); i > 0 {
			key = kv[:i]
		}
		if _, overridden := overrides[key]; overridden {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

// readyScanner streams child output, echoes it to the harness log, and closes
// hit once a chunk matches the pattern. Patterns are matched per Write chunk;
// patterns longer than one burst of output are not supported (readiness lines
// are short by convention).
type readyScanner struct {
	name    string
	pattern *regexp.Regexp
	hit     chan struct{}
	once    sync.Once
	log     *zap.Logger
}

func (s *readyScanner) Write(p []byte) (int, error) {
	if len(p) > 0 {
		s.log.Debug("process output", zap.String("actor", s.name), zap.ByteString("line", bytes.TrimRight(p, "\n")))
	}
	if s.pattern != nil && s.pattern.Match(p) {
		s.once.Do(func() { close(s.hit) })
	}
	return len(p), nil
}

// LaunchActor provisions and starts one real-process actor.
func (h *harness) LaunchActor(ctx context.Context, actor *project.Actor) error {
	spec := actor.Process
	if spec == nil || len(spec.Start) == 0 {
		return fmt.Errorf("harness %s: no process.start declared", actor.Name)
	}

	// 1. Setup: one-shot provisioning (e.g. pairing), run to completion.
	if len(spec.Setup) > 0 {
		cmd := exec.CommandContext(ctx, h.tmpl(spec.Setup[0], actor), h.tmplSlice(spec.Setup[1:], actor)...)
		cmd.Env = h.childEnv(spec, actor)
		cmd.Dir = h.tmpl(spec.Workdir, actor)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("harness %s: setup failed: %w: %s", actor.Name, err, bytes.TrimSpace(out))
		}
	}

	// 2. Capture: merge JSON dot-path values into the actor's runtime
	// PathParams so {{role.param}} send-body templating and {param} URL
	// templating can reference them (the WS protocol index is built after
	// session setup, so the values are picked up automatically).
	if spec.CaptureFile != "" && len(spec.CaptureJSON) > 0 {
		if err := h.capture(actor); err != nil {
			return err
		}
	}

	// 3. Start: long-running child in its own process group.
	readyRe, err := compileReady(spec.ReadyPattern)
	if err != nil {
		return fmt.Errorf("harness %s: ready_pattern: %w", actor.Name, err)
	}
	timeout := 30 * time.Second
	if spec.ReadyTimeout != "" {
		timeout, err = time.ParseDuration(spec.ReadyTimeout)
		if err != nil {
			return fmt.Errorf("harness %s: ready_timeout %q: %w", actor.Name, spec.ReadyTimeout, err)
		}
	}

	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, h.tmpl(spec.Start[0], actor), h.tmplSlice(spec.Start[1:], actor)...)
	cmd.Env = h.childEnv(spec, actor)
	cmd.Dir = h.tmpl(spec.Workdir, actor)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own group for group teardown
	scanner := &readyScanner{name: actor.Name, pattern: readyRe, hit: make(chan struct{}), log: h.log}
	cmd.Stdout = scanner
	cmd.Stderr = scanner
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("harness %s: start: %w", actor.Name, err)
	}

	h.mu.Lock()
	h.procs[actor.Name] = &harnessProc{name: actor.Name, cmd: cmd, cancel: cancel}
	h.mu.Unlock()

	// 4. Ready: wait for the pattern (no pattern = no wait).
	if readyRe != nil {
		select {
		case <-scanner.hit:
		case <-time.After(timeout):
			h.stopOne(actor.Name)
			return fmt.Errorf("harness %s: ready pattern %q not seen within %s", actor.Name, spec.ReadyPattern, timeout)
		}
	}
	h.log.Info("real-process actor ready", zap.String("actor", actor.Name), zap.Int("pid", cmd.Process.Pid))
	return nil
}

func compileReady(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}

func (h *harness) capture(actor *project.Actor) error {
	spec := actor.Process
	raw, err := os.ReadFile(h.tmpl(spec.CaptureFile, actor))
	if err != nil {
		return fmt.Errorf("harness %s: capture file: %w", actor.Name, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("harness %s: capture file %s is not a JSON object: %w", actor.Name, spec.CaptureFile, err)
	}
	if actor.Credentials.PathParams == nil {
		actor.Credentials.PathParams = map[string]string{}
	}
	for param, path := range spec.CaptureJSON {
		v, err := dotPathValue(doc, path)
		if err != nil {
			return fmt.Errorf("harness %s: capture %s: %w", actor.Name, path, err)
		}
		actor.Credentials.PathParams[param] = v
	}
	return nil
}

// dotPathValue walks a dot-separated path into decoded JSON (map keys only,
// same semantics as the authflow token_from capture). Non-string leaves are
// stringified via fmt.
func dotPathValue(doc map[string]any, path string) (string, error) {
	var cur any = doc
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("segment %q: not an object", seg)
		}
		v, ok := m[seg]
		if !ok {
			return "", fmt.Errorf("segment %q not found", seg)
		}
		cur = v
	}
	switch v := cur.(type) {
	case string:
		return v, nil
	case nil:
		return "", fmt.Errorf("value is null")
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// StopAll terminates every managed child process group.
func (h *harness) StopAll() {
	h.mu.Lock()
	names := make([]string, 0, len(h.procs))
	for name := range h.procs {
		names = append(names, name)
	}
	h.mu.Unlock()
	for _, name := range names {
		h.stopOne(name)
	}
}

// stopOne tears down one child: SIGTERM to the process group, a 5s grace,
// then SIGKILL. Safe to call twice (failed launches may already be gone).
func (h *harness) stopOne(name string) {
	h.mu.Lock()
	p := h.procs[name]
	delete(h.procs, name)
	h.mu.Unlock()
	if p == nil || p.cmd.Process == nil {
		return
	}
	pgid := p.cmd.Process.Pid // Setpgid: pid == pgid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _, _ = p.cmd.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	p.cancel()
	h.log.Info("real-process actor stopped", zap.String("actor", name))
}

// io compatibility guard: readyScanner must satisfy io.Writer for cmd.Stdout.
var _ io.Writer = (*readyScanner)(nil)

// launchRealProcessActors provisions and starts every fidelity: real-process
// actor declared in the config. A launch failure fails the run: every case
// that routes to a dead real actor would otherwise fail misleadingly.
func (s *Session) launchRealProcessActors(ctx context.Context) error {
	real := false
	for i := range s.Config.Actors {
		if s.Config.Actors[i].Fidelity == project.FidelityRealProcess {
			real = true
			break
		}
	}
	if !real {
		return nil
	}
	runtimeDir := filepath.Join(s.ProjectDir, ".cerberus", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return fmt.Errorf("harness runtime dir: %w", err)
	}
	s.harness = newHarness(s.Logger, runtimeDir)
	for i := range s.Config.Actors {
		a := &s.Config.Actors[i]
		if a.Fidelity != project.FidelityRealProcess {
			continue
		}
		if err := s.harness.LaunchActor(ctx, a); err != nil {
			s.harness.StopAll()
			s.harness = nil
			return fmt.Errorf("real-process actor %s: %w", a.Name, err)
		}
	}
	return nil
}

// harnessStopAll tears down real-process actors (session finalize path).
func (s *Session) harnessStopAll() {
	if s.harness != nil {
		s.harness.StopAll()
		s.harness = nil
	}
}
