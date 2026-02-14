# Contributing to logtap

## Prerequisites

- Go 1.22+
- [golangci-lint](https://golangci-lint.run/welcome/install/)
- GNU Make

## Dev setup

```bash
git clone https://github.com/ppiankov/logtap.git
cd logtap
make deps
make build
make test
```

## Testing

Tests are mandatory for all new code.

- **Coverage target:** >85% per package
- **Race detection:** always (`make test` uses `-race`)
- **Deterministic only:** no flaky or time-dependent tests
- **Test location:** alongside source files (`foo_test.go` next to `foo.go`)

Run tests:

```bash
make test                                  # all tests with race detection
go test -cover ./internal/recv/            # single package with coverage
```

## Commit conventions

Format: `type: concise imperative statement`

- Lowercase after colon, no trailing period
- Max 72 characters
- Types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `perf`, `ci`, `build`

Examples:

```
feat: add user authentication
fix: handle nil user in payment checkout
test: add stress tests for file rotator
```

Optional body (separated by blank line) explains **why**, not what.

## Pull requests

- One logical change per PR
- All tests pass (`make test`)
- Lint clean (`make lint`)
- Include tests for new functionality
- Keep PRs small and focused — easier to review, faster to merge

## Code style

- Comments explain "why", not "what"
- No magic numbers — name and document constants
- Early returns over deep nesting
- No decorative comments or unnecessary abstractions
- Run `make fmt` before committing

## Project structure

```
cmd/logtap/          CLI entry point (Cobra subcommands)
cmd/logtap-forwarder/ Sidecar binary
internal/            All business logic (recv, rotate, k8s, forward, etc.)
docs/                Design docs and work orders
```

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
