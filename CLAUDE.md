# Project: logtap

## New Agent? Start Here
Run `/load-context` to read project context, work orders, and current status before doing anything.

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
- Subcommands: recv, open, inspect, slice, export, triage, grep, merge, snapshot, diff, check, tap, untap, status, completion

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
- Internal packages: short single-word names (recv, k8s, sidecar, rotate, archive, forward)
- Bounded by default: disk caps, drop policies, backpressure
- Transport: Loki push API compatible (POST /loki/api/v1/push)
- Cluster integration: sidecar injection (not agent config patching)

## Anti-Patterns
- NEVER block the sender — always accept or drop, never backpressure upstream
- NEVER build a search engine — index.jsonl is a file-level index only, devs use rg/jq/grep on content
- NEVER store without rotation — hard cap disk usage
- NEVER patch shared logging agent configs — use sidecar injection instead
- NEVER allow PII mirroring without explicit unsafe flag

## Verification
- Run `make test` after code changes (includes -race)
- Run `make lint` before marking complete
- Run `go vet ./...` for suspicious constructs
