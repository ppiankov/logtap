# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.3.0] - 2026-02-15

### Added

- Kubernetes operation timeouts (`--timeout` flag on tap, untap, check, status; default 30s)
- Input validation for `--sidecar-memory` and `--sidecar-cpu` at parse time (prevents panics)
- Tap progress output for multi-workload operations (`Tapping deployment/X [3/10]...`)
- Auto-rollback on partial tap failure (with `--no-rollback` escape hatch)
- Receiver version endpoint (`GET /api/version` with `{"version":"...","api":1}`)
- In-cluster receiver TTL via `--ttl` flag (default 4h, uses `activeDeadlineSeconds`)
- Prometheus metrics: push duration histogram, writer queue gauge, rotation counter, rotation errors
- API stability documentation (`docs/api-stability.md`)
- Workflow example scripts (`docs/examples/`)

### Changed

- All k8s commands use context with configurable timeout instead of background context
- Rotation callbacks for metrics (no prometheus dependency in rotate package)

### Testing

- CLI integration tests: filter, command registration, config defaults, validation
- Error path tests: corrupt index, oversized request, invalid regex, corrupt tar, asymmetric diff
- cmd/logtap coverage: 0% -> 32.3%

## [0.2.0] - 2026-02-15

### Added

- Capture grep command (`grep`) for cross-file regex search with index-aware file skipping and count mode
- Capture merge command (`merge`) for combining multiple captures into one
- Capture diff command (`diff`) for comparing two captures (line counts, labels, error patterns, rate comparison)
- Snapshot pack/extract (`snapshot`) for portable tar+zstd capture archives
- Fluent Bit sidecar forwarder variant (`tap --forwarder fluent-bit`) with ConfigMap-based config
- Shell completion command (`completion`) for bash, zsh, fish, powershell
- Configuration file support (`~/.config/logtap/config.yaml`) with env var override
- Health endpoints (`/healthz`, `/readyz`) for receiver with writer backpressure detection
- Health endpoint for forwarder sidecar on port 9091
- PreStop lifecycle hooks on both logtap and Fluent Bit sidecar containers
- Liveness probes on sidecar containers
- Receiver container image (`ghcr.io/ppiankov/logtap`) with multi-arch support
- Homebrew formula template for `brew install ppiankov/tap/logtap`
- Multi-workload tap (`tap --all`) for tapping all workloads matching a label selector
- Troubleshooting guide (`docs/troubleshooting.md`)

### Changed

- CI now generates coverage profiles and uploads as artifacts
- CI runs benchmarks on pull requests with results as artifacts
- Makefile: added `help`, `install`, `coverage`, `all` targets

### Testing

- Performance benchmarks for recv, archive, and rotate packages
- Coverage hardening: k8s 85.9%, sidecar 95.3%, archive 84.6%

## [0.1.0] - 2026-02-15

### Added

- HTTP receiver with Loki push API (`POST /loki/api/v1/push`) and raw JSONL endpoint (`POST /logtap/raw`)
- File rotation with zstd compression and bounded disk usage (hard cap with oldest-file deletion)
- Live TUI with stats, top talkers, scrollable log pane, vim-style navigation, and regex search
- PII redaction engine with 7 built-in patterns (credit card with Luhn, email, JWT, bearer, IPv4, SSN, phone) and custom YAML patterns
- Capture directory as portable artifact with metadata.json, index.jsonl, and zstd-compressed JSONL files
- Replay command (`open`) with speed control, time/label/grep filters, and TUI playback
- Capture inspection (`inspect`) with label breakdown, timeline sparkline, and JSON output
- Filtered capture extraction (`slice`) with index-aware file skipping
- Export to parquet, CSV, and JSONL formats with filter support
- Triage command for parallel anomaly scanning with error signature normalization and incident window detection
- Sidecar injection (`tap`) with session IDs, resource quota pre-checks, and multi-user isolation
- Sidecar removal (`untap`) with session-scoped cleanup
- Cluster readiness validation (`check`) with RBAC checks, orphan detection, and quota analysis
- Tapped workload monitoring (`status`) with pod health and receiver stats
- In-cluster receiver deployment with port-forward tunnel
- Forwarder sidecar binary and multi-arch container image (`ghcr.io/ppiankov/logtap-forwarder`)
- Production namespace protection (`--allow-prod` flag with label-based detection)
- TLS support for receiver (`--tls-cert`, `--tls-key`)
- Audit logging (append-only JSONL with connection metadata)
- TUI redaction status display
- Prometheus metrics endpoint (`/metrics`)
- Headless mode for receiver (`--headless`)

### Testing

- Backpressure and stress tests for receiver pipeline (sustained throughput, concurrent connections, drop counter accuracy)
- Stress tests for file rotator (concurrent writes, tight disk cap, high rotation frequency)
- Coverage tests for all internal packages (archive 86.6%, forward 87.4%, k8s 85.0%, recv 90.2%, rotate 86.6%, sidecar 94.2%)
