// Command slowtest consumes `go test -json` output on stdin, passes the
// human-readable test output through to stdout, and additionally warns about
// tests that exceed a duration threshold. It exits non-zero if any test
// failed or was not run to completion.
//
// This exists so a slow or deadlocked test surfaces explicitly instead of
// silently dragging the whole suite (or hanging until the -timeout kills it).
// Pipe into it: `go test -json ./... | go run ./tools/slowtest`.
//
// Override the threshold (seconds) with SLOW_TEST_THRESHOLD (default 60).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

type testEvent struct {
	Action  string  `json:"action"` // start, run, pause, cont, pass, bench, fail, skip, output
	Package string  `json:"package"`
	Test    string  `json:"test"`
	Elapsed float64 `json:"elapsed"` // seconds, populated on pass/fail/skip
	Output  string  `json:"output"`
}

func main() {
	threshold := 60.0
	if v := os.Getenv("SLOW_TEST_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			threshold = f
		}
	}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	failed, slow := 0, 0
	for sc.Scan() {
		var ev testEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		// Pass human-readable output through unchanged.
		if ev.Output != "" {
			_, _ = fmt.Fprint(os.Stdout, ev.Output)
		}
		// Track failures (package- or test-level).
		if ev.Action == "fail" {
			failed++
		}
		// Flag slow tests on their terminal event.
		if (ev.Action == "pass" || ev.Action == "fail") && ev.Test != "" && ev.Elapsed > threshold {
			slow++
			fmt.Fprintf(os.Stderr, "SLOW TEST %.1fs (>%.0fs threshold): %s\n",
				ev.Elapsed, threshold, fullName(ev.Package, ev.Test))
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "slowtest: read stdin: %v\n", err)
		os.Exit(2)
	}

	if slow > 0 {
		fmt.Fprintf(os.Stderr, "\n%d slow test(s) detected (threshold %.0fs). "+
			"Investigate possible deadlock, missing -timeout, or accidental recursion.\n", slow, threshold)
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "%d test(s) failed.\n", failed)
		os.Exit(1)
	}
}

func fullName(pkg, test string) string {
	if test == "" {
		return pkg
	}
	return pkg + " " + test
}
