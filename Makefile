.PHONY: build test lint fmt check clean run coverage e2e

build:
	go build -o bin/cerberus ./cmd/cerberus

test:
	go test -v -race -count=1 ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w -local github.com/binoctal/cerberus .

check: fmt lint test

clean:
	rm -rf bin/

run: build
	./bin/cerberus

coverage:
	go test -race -coverprofile=cover.out -count=1 ./...
	go tool cover -func=cover.out | tail -1
	@echo "HTML report: go tool cover -html=cover.out -o cover.html"

e2e:
	go test -v -race -tags=e2e ./internal/smoke/ -timeout 5m
