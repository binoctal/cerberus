# Contributing to Cerberus

Thanks for your interest in contributing!

## Development Setup

```bash
git clone https://github.com/binoctal/cerberus.git
cd cerberus
go mod download
```

## Building

```bash
go build ./...
```

## Running Tests

```bash
go test -race -count=1 ./...
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Run `golangci-lint` before submitting PRs
- Write tests for new functionality
- Keep the zero-external-dependency philosophy (see allowed deps in go.mod)

## Pull Request Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Make your changes with tests
4. Ensure all tests pass (`go test ./...`)
5. Ensure linter passes (`golangci-lint run`)
6. Submit a pull request

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(scope): add new feature
fix(scope): fix a bug
docs: update documentation
test(scope): add tests
refactor(scope): restructure code
```

## Reporting Issues

- Use [GitHub Issues](https://github.com/binoctal/cerberus/issues)
- Include steps to reproduce, expected vs actual behavior
- Include Go version and OS

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
