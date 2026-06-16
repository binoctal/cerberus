.PHONY: build test lint fmt check clean run coverage e2e

# Put GOPATH/bin (where `go install` places tools like goimports) on PATH so
# fmt/lint work without per-user shell configuration.
export PATH := $(PATH):$(shell go env GOPATH)/bin

build:
	@mkdir -p build
	go build -o build/cerberus ./cmd/cerberus

test:
	go test -v -race -count=1 ./...

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
