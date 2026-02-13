# Project: logtap

## Commands
- `make build` — Build binary
- `make test` — Run tests with race detection
- `make lint` — Run golangci-lint
- `make fmt` — Format with gofmt/goimports
- `make clean` — Clean build artifacts

## Architecture
- Entry: cmd/logtap/main.go (minimal, delegates to internal/)
- Internal packages: internal/
- CLI framework: Cobra (spf13/cobra)
- Subcommands: recv, install, enable, disable, uninstall

## What logtap Is
Ephemeral log mirror for load testing. Annotation-based opt-in. Disposable.
Accepts Loki push API, writes compressed JSONL to disk, shows minimal TUI.

## What logtap Is NOT
- Not a log analytics system
- Not replacing Loki/OpenSearch/ELK
- Not a trace or metrics collector
- Not an OTel collector
- Not long-term storage
- Not prod observability

## Conventions
- Minimal main.go — single Execute() call
- Internal packages: short single-word names (recv, k8s, patch, rotate)
- Bounded by default: disk caps, drop policies, backpressure
- Transport: Loki push API compatible (POST /loki/api/v1/push)
- Supported agents: Alloy, Vector (v1), Fluent Bit (v1.1)

## Anti-Patterns
- NEVER block the sender — always accept or drop, never backpressure upstream
- NEVER index or query — devs use rg/jq/grep on local files
- NEVER store without rotation — hard cap disk usage
- NEVER modify logging pipeline without marker blocks for clean uninstall
- NEVER allow PII mirroring without explicit unsafe flag

## Verification
- Run `make test` after code changes (includes -race)
- Run `make lint` before marking complete
- Run `go vet ./...` for suspicious constructs
