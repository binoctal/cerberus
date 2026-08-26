.PHONY: build test slowtest lint fmt check clean run coverage e2e integration-openagents

# Put GOPATH/bin (where `go install` places tools like goimports) on PATH so
# fmt/lint work without per-user shell configuration.
export PATH := $(PATH):$(shell go env GOPATH)/bin

build:
	@mkdir -p build
	go build -o build/cerberus ./cmd/cerberus

test:
	# -timeout caps any single package so a deadlocked/hung test can't run
	# unbounded (default go test timeout is 10m per package — too long to
	# notice a deadlock). Use `make slowtest` to also flag slow-but-passing tests.
	go test -v -race -count=1 -timeout 5m ./...

# slowtest runs the suite with JSON output and flags tests exceeding
# SLOW_TEST_THRESHOLD seconds (default 60), so a slow/recursive/deadlocked
# case surfaces explicitly instead of silently dragging. CI uses this.
slowtest:
	go test -race -count=1 -json -timeout 5m ./... | go run ./tools/slowtest

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w -local github.com/binoctal/cerberus .

check: fmt lint test

clean:
	rm -rf build/
	rm -rf runtime/

run: build
	./build/cerberus

coverage:
	@mkdir -p runtime
	go test -race -coverprofile=runtime/cover.out -count=1 ./...
	go tool cover -func=runtime/cover.out | tail -1
	@echo "HTML report: go tool cover -html=runtime/cover.out -o runtime/cover.html"

e2e:
	go test -v -race -tags=e2e ./internal/smoke/ -timeout 5m

# integration-openagents runs the agent and scout packages' //go:build
# integration builds (agent: the live open-agents surface —
# TestVocabularyDriven, relay, lifecycle, auth, orchestrator-callback,
# sender-exclusion probe; scout: the vocab-driven -unauth gate) against a real
# wrangler dev server (sibling ../open-agents repo). The script brings the server
# up (fnm selects Node >=22), runs the suite, and tears the server down — reusing
# an already-running one without killing it. Narrow with TEST=<regex>; point at a
# non-sibling checkout with OPENAGENTS_DIR=/path/to/open-agents/apps/api.
integration-openagents:
	@bash scripts/integration-openagents.sh
