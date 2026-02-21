# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [1.2.0] - 2026-02-21

### Added

- `grep --context N` / `-C N` flag for surrounding context lines around matches
- `diff --baseline` flag for deterministic regression/improvement verdicts
- Cross-service error correlation in `triage` (Pearson cross-correlation with lag detection)
- `--json` flag for slice, export, merge, upload, download, and snapshot subcommands
- `--webhook-auth` flag for recv (bearer token and HMAC-SHA256 signature support)
- Structured exit codes: not_found (3), permission (4), network (5), findings (6)

## [1.1.0] - 2026-02-20

### Added

- `logtap report` command — single-command incident deliverable (inspect + triage → JSON + HTML)
- `logtap catalog` command — discover and list capture directories with `--json` and `--recursive`
- Structured JSON error output when `--json` flag is active (agents can parse error types)
- Severity classification in reports (high/medium/low based on error rate and patterns)
- Suggested commands in report output for follow-up investigation

## [1.0.9] - 2026-02-20

### Added

- TLS support for forwarder push path (`https://` target auto-detection)
- `LOGTAP_TLS_SKIP_VERIFY` env var for self-signed certificates
- Scheme-aware URL construction in `tap` and `check` commands

## [1.0.8] - 2026-02-19

### Added

- `grep --format text` for human-readable timeline output (sorted chronologically)
- `grep --sort` flag for chronological JSONL output across multiple files
- Negate search in TUI (`/!pattern` excludes matching lines)

## [1.0.7] - 2026-02-18

### Fixed

- Replay TUI (`open`) search now matches label values, not just message content
- Replay TUI status bar badge colors render reliably via explicit ANSI256 profile
- `open` shows correct total line count for captures with orphan files (no index)
- `grep` detects reversed arguments and suggests correct usage

## [1.0.6] - 2026-02-18

### Fixed

- `grep` and TUI search (`/`) now match against label values, not just message content
- `inspect` counts lines from orphan files (active capture not yet indexed)
- Slice command test compatibility (`runSlice` wrapper)
- Lint fixes in `slice_test.go` (deprecated `ioutil`, unchecked error returns)

## [1.0.5] - 2026-02-18

### Added

- `--json` flag on `watch` command for machine-readable output

### Fixed

- Consistent error wrapping in `archive` package (`ReadMetadata`, `ReadIndex`, `WriteIndex`)
- `slice` command handles both compressed and uncompressed capture files
- Help text consistency: lowercase flag descriptions, added Long description to `slice`
- Lint errors in `slice.go` and `index.go`

### Testing

- New tests for `buffers`, `contextutil`, and `logtypes` packages (100% coverage)
- All internal packages >= 85% coverage

## [1.0.4] - 2026-02-17

### Fixed

- Wire `logtap_redactions_total` Prometheus counter via onRedact callback
- Fix troubleshooting docs referencing non-existent `--redact-show-patterns` flag
- Add PII redaction examples to README Key flags section

## [1.0.3] - 2026-02-17

### Fixed

- Build static binaries with `CGO_ENABLED=0` for GLIBC compatibility on older Linux

## [1.0.2] - 2026-02-17

### Added

- Auto-detect Linkerd/Istio service mesh and add bypass annotations during `logtap tap`
- `logtap deploy` subcommand for in-cluster receiver (Pod+Service)
- Mesh annotation cleanup on last `logtap untap`

## [1.0.1] - 2026-02-17

### Fixed

- Auto-create RBAC (Role+RoleBinding) for forwarder sidecar service account on tap
- Update Homebrew formula to v1.0.1 with correct SHA256 checksums

## [1.0.0] - 2026-02-17

### Added

- Cloud upload/download for captures to S3 and GCS (`upload`, `download` subcommands)
- Webhook lifecycle notifications on capture start, rotate, and stop
- HTML triage reports (`triage --html`)
- Forwarder retry buffer with ring-buffer backlog and Prometheus metrics
- Garbage collection command (`gc`) with `--max-age` and `--max-total` policies

### Changed

- S3 and GCS backends refactored with injectable interfaces for testability
- README updated to reflect stable release status

### Testing

- Cloud package coverage: 25% -> 89.5% (mock-based S3 and GCS tests)
- cmd/logtap coverage: 32.3% -> 56.2% (GC, version, expanded archive/cmd tests)
- cmd/logtap-forwarder coverage: 62.5% -> 81.9% (flush, multi-container, retry tests)
- 14 benchmarks across recv, archive, rotate packages

## [0.4.0] - 2026-02-16

### Added

- End-to-end log flow tests with real forwarder and receiver images in Kind (WO-42)
- Kind integration tests for k8s package: 13 subtests against real cluster (WO-17)
- Health endpoint unit tests (healthz, readyz, readyz backpressure)
- `terminationGracePeriodSeconds` recommendation in `tap --dry-run` output
- CI jobs: `integration` and `e2e` with Kind clusters
- Makefile targets: `test-integration`, `test-e2e`

### Fixed

- Retry sidecar remove on k8s optimistic concurrency conflict in integration tests
- Kind image loading (use separate `kind load` per image, avoid multi-platform digest errors)
- Integration test namespace isolation for quota checks

### Testing

- rotate coverage: 84.9% -> 86.5% (callback tests for SetOnRotate/SetOnError)
- recv coverage: 88.4% -> 89.9% (healthz/readyz endpoint tests)

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
