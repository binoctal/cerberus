package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// benchEnv gates real LLM/subprocess work. Unset -> the tool prints a skip line
// and exits 0, so an accidental invocation under `go test` or `make check`
// cannot touch the network. The scoring engine (score.go) has no such gate
// because it is pure.
const benchEnv = "CERBERUS_BENCH"

func main() {
	n := flag.Int("n", 18, "number of infer runs")
	binary := flag.String("binary", "build/cerberus", "path to the cerberus binary")
	healthURL := flag.String("health-url", "http://localhost:8989/health", "target health URL; must return 200")
	workdir := flag.String("workdir", ".", "cwd for the infer call (open-agents repo root)")
	name := flag.String("name", "open-agents", "--name passed to protocol infer")
	from := flag.String("from", "apps/api/src/realtime", "--from passed to protocol infer")
	service := flag.String("service", "api", "--service passed to protocol infer")
	samples := flag.String("samples", "3", "samples hint for the report header (the binary's --samples default)")
	perCall := flag.Duration("per-call-timeout", 120*time.Second, "timeout per infer run")
	flag.Parse()

	if err := runBench(*n, *binary, *healthURL, *workdir, *name, *from, *service, *samples, *perCall); err != nil {
		fmt.Fprintln(os.Stderr, "protoinferbench:", err)
		os.Exit(1)
	}
}

func runBench(n int, binary, healthURL, workdir, name, from, service, samples string, perCall time.Duration) error {
	if os.Getenv(benchEnv) != "1" {
		fmt.Printf("skip (%s unset); set %s=1 to run the benchmark\n", benchEnv, benchEnv)
		return nil
	}
	// LLM creds must be in the environment the binary inherits; fail loud
	// before burning N doomed runs.
	for _, k := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL"} {
		if os.Getenv(k) == "" {
			return fmt.Errorf("%s unset; the cerberus binary needs it for LLM calls", k)
		}
	}
	if err := healthCheck(healthURL); err != nil {
		return fmt.Errorf("health check: %w", err)
	}

	results := make([]runResult, 0, n)
	for i := 1; i <= n; i++ {
		stdout, stderr, code, err := runInfer(binary, workdir, name, from, service, perCall)
		if err != nil {
			// Subprocess could not be started or timed out: count as hard_error.
			fmt.Fprintf(os.Stderr, "run %d/%d: exec failed: %v\n", i, n, err)
			results = append(results, runResult{outcome: outcomeHardError})
			continue
		}
		r := classifyRun(stdout, stderr, code)
		if r.outcome != outcomeDraft {
			// One-line diagnostic on the non-draft tail.
			firstLine(stderr, stdout)
			fmt.Fprintf(os.Stderr, "run %d/%d: %s\n", i, n, r.outcome)
		} else {
			fmt.Fprintf(os.Stderr, "run %d/%d: %s\n", i, n, r.outcome)
		}
		results = append(results, r)
	}
	fmt.Println(formatReport(Aggregate(results, samples)))
	return nil
}

func healthCheck(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func runInfer(binary, workdir, name, from, service string, timeout time.Duration) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary,
		"protocol", "infer",
		"--name", name,
		"--from", from,
		"--service", service,
		"--dry-run",
	)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
		err = nil // non-zero exit is a normal outcome, handled by classifyRun.
	}
	return stdout.String(), stderr.String(), code, err
}

func firstLine(s ...string) {
	for _, t := range s {
		if t == "" {
			continue
		}
		if i := strings.IndexByte(t, '\n'); i >= 0 {
			t = t[:i]
		}
		fmt.Fprintf(os.Stderr, "  stderr/stdout: %s\n", t)
		return
	}
}
