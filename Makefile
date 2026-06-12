.PHONY: build test lint fmt check clean run

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
