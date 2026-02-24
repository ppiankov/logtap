<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="assets/logo-light.svg">
    <img src="assets/logo-light.svg" alt="logtap" width="128">
  </picture>
</p>

# logtap
[![CI](https://github.com/ppiankov/logtap/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/logtap/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ppiankov/logtap)](https://goreportcard.com/report/github.com/ppiankov/logtap)
[![Go 1.22+](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![ANCC](https://img.shields.io/badge/ANCC-compliant-brightgreen)](https://ancc.dev)

Ephemeral log mirror for Kubernetes load testing.

Annotation-based opt-in. Accepts Loki push API, writes compressed JSONL to disk, shows a minimal TUI. Capture directories are portable — share them with `tar`, `rsync`, or `scp` and replay on any machine.

## What logtap is

- **Receiver** (`recv`) — accepts Loki push API payloads, writes rotated zstd-compressed JSONL with bounded disk usage
- **Live TUI** — real-time stats, top talkers, scrollable log pane with vim-style navigation and regex search
- **Sidecar injection** (`tap`/`untap`) — injects a log-forwarding sidecar into Kubernetes workloads, no logging agent config changes
- **Replay** (`open`) — replays capture directories at original speed or fast-forward with the same TUI
- **Analysis** (`inspect`, `slice`, `export`, `triage`) — summarize, filter, convert to parquet/CSV, or scan for anomalies
- **Cluster safety** (`check`, `status`) — validates readiness, detects orphaned sidecars, shows what's tapped

## What logtap is NOT

- Not a log analytics system
- Not replacing Loki, OpenSearch, or ELK
- Not a trace or metrics collector
- Not an OpenTelemetry collector
- Not long-term storage
- Not production observability

## Philosophy

**Principiis obsta** — resist the beginnings. Address root causes, not symptoms.

- **Bounded by default** — hard disk caps, drop policies, backpressure. Never block the sender.
- **Disposable** — captures are ephemeral artifacts for debugging, not long-term archives.
- **Mirrors, not oracles** — presents evidence and lets you decide. No ML, no probabilistic magic.
- **Reversible** — sidecar injection is clean removal away. No shared config patching.
- **Explicit consent** — production namespaces require `--allow-prod`. PII redaction happens before bytes hit disk.

## Quick start

```bash
# Install with Homebrew (macOS / Linux)
brew install ppiankov/tap/logtap

# Or download binary (macOS Apple Silicon)
curl -LO https://github.com/ppiankov/logtap/releases/latest/download/logtap_darwin_arm64.tar.gz
tar -xzf logtap_darwin_arm64.tar.gz
sudo mv logtap /usr/local/bin/

# Or build from source
git clone https://github.com/ppiankov/logtap.git
cd logtap && make build
./bin/logtap version

# Start receiver
logtap recv --dir ./capture --max-disk 1GB --redact

# Send test data
curl -X POST http://localhost:3100/loki/api/v1/push \
  -H 'Content-Type: application/json' \
  -d '{"streams":[{"stream":{"app":"test"},"values":[["1234567890000000000","hello world"]]}]}'

# Replay
logtap open ./capture
```

### Agent Integration

logtap is designed to be used by autonomous agents without plugins or SDKs. Single binary, deterministic output, structured JSON, bounded jobs.

```bash
# Agent install (no brew needed)
curl -LO https://github.com/ppiankov/logtap/releases/latest/download/logtap_linux_amd64.tar.gz
tar -xzf logtap_linux_amd64.tar.gz && sudo mv logtap /usr/local/bin/
```

Agents: read [`SKILL.md`](SKILL.md) for commands, flags, JSON output structure, and incident workflow.

Key patterns for agents:
- `logtap inspect <dir> --json` — capture summary (files, entries, labels, timeline)
- `logtap triage <dir> --json` — anomaly scan results with severity
- `logtap grep <pattern> <dir> --format text` — human-readable cross-service timeline
- `logtap check --json` — cluster readiness and orphan detection
- `logtap upload <dir> --to s3://... --share --json` — upload and return presigned URLs

### Kubernetes workflow

```bash
logtap check                                     # verify cluster readiness
logtap recv --in-cluster --image ghcr.io/ppiankov/logtap-forwarder:latest --redact
logtap tap --deployment api-gateway              # inject sidecar
# ... watch TUI, investigate ...
logtap untap --deployment api-gateway            # remove sidecar
# Ctrl+C receiver
logtap inspect ./capture                         # see what you got
logtap triage ./capture --out ./triage           # scan for anomalies
```

## Usage

| Command | Description |
|---------|-------------|
| `logtap recv` | Start the log receiver (local, in-cluster, or with TLS) |
| `logtap open <dir>` | Replay a capture directory |
| `logtap inspect <dir>` | Show labels, timeline, and stats of a capture |
| `logtap slice <dir>` | Extract time/label subset to a new capture directory |
| `logtap export <dir>` | Convert capture to parquet, CSV, or JSONL |
| `logtap triage <dir>` | Scan for anomalies and produce a triage report |
| `logtap grep <pattern> <dir>` | Search captures for matching entries |
| `logtap diff <dir1> <dir2>` | Compare two captures (structure or baseline regression) |
| `logtap merge <dirs...>` | Merge multiple captures into one |
| `logtap report <dir>` | Generate incident report (inspect + triage in one artifact) |
| `logtap catalog [dir]` | Discover and list capture directories |
| `logtap watch <dir>` | Tail a live or completed capture |
| `logtap snapshot <dir>` | Pack or extract a capture archive (tar.zst) |
| `logtap upload <dir>` | Upload capture to S3/GCS |
| `logtap download <url>` | Download capture from S3/GCS |
| `logtap deploy` | Deploy receiver as in-cluster pod + service |
| `logtap gc <dir>` | Delete old captures by age or total size |
| `logtap tap` | Inject log-forwarding sidecar into workloads |
| `logtap untap` | Remove sidecar from workloads |
| `logtap check` | Validate cluster readiness and detect leftovers |
| `logtap status` | Show tapped workloads and receiver stats |

### Key flags

```bash
# Receiver
logtap recv --dir ./capture --max-disk 50GB --redact              # localhost:3100 (default)
logtap recv --listen :3100 --dir ./capture                       # all interfaces
logtap recv --headless                           # no TUI, log to stderr
logtap recv --tls-cert cert.pem --tls-key key.pem
logtap recv --in-cluster --image ghcr.io/ppiankov/logtap-forwarder:latest

# Sidecar injection
logtap tap --deployment api-gateway --target host:3100
logtap tap --namespace payments --allow-prod --target host:3100
logtap tap --selector app=worker --target host:3100             # tap by label
logtap tap --namespace payments --all --force --target host:3100 # tap all workloads
logtap untap --deployment api-gateway

# Replay with filters
logtap open ./capture --speed 10x
logtap open ./capture --from 10:32 --to 10:45 --label app=gateway

# Export
logtap export ./capture --format parquet --out capture.parquet
logtap export ./capture --format csv --grep "error|timeout" --out errors.csv

# Grep
logtap grep "error|timeout" ./capture                             # search all files
logtap grep "ORD-12345" ./capture --format text                   # human-readable timeline
logtap grep "tracking-id-abc123" ./capture --sort                 # chronological JSONL
logtap grep "OOMKilled" ./capture --label app=worker --count      # count per file
logtap grep "panic" ./capture -C 3                                # 3 context lines around matches

# Diff and baseline comparison
logtap diff ./before ./after --json                               # structural diff
logtap diff ./baseline ./current --baseline --json                # regression verdict

# Cloud upload / download
logtap upload ./capture s3://bucket/prefix
logtap download s3://bucket/prefix --out ./capture

# Webhook auth
logtap recv --dir ./capture --webhook-url http://hook --webhook-auth bearer:my-token
logtap recv --dir ./capture --webhook-url http://hook --webhook-auth hmac-sha256:secret

# JSON output (available on most commands)
logtap slice ./capture --label app=web --out ./slice --json
logtap merge ./a ./b --out ./merged --json
logtap snapshot ./capture --output capture.tar.zst --json

# Triage
logtap triage ./capture --out ./triage --jobs 8

# PII redaction — all built-in patterns (email, credit_card, jwt, bearer, ip_v4, ssn, phone)
logtap recv --redact --dir ./capture
# Specific patterns only
logtap recv --redact=email,jwt --dir ./capture
# Custom patterns from YAML
logtap recv --redact --redact-patterns ./patterns.yaml --dir ./capture
```

### TUI keybindings

| Key | Action |
|-----|--------|
| `j` / `k` | Scroll down / up |
| `d` / `u` | Half-page down / up |
| `G` / `gg` | Jump to bottom / top |
| `f` | Toggle follow mode |
| `/` | Search (regex); prefix with `!` to negate |
| `l` | Label filter (e.g., `app=gateway`) |
| `n` / `N` | Next / previous match |
| `q` | Quit |

Replay mode adds `space` (pause/resume) and `]`/`[` (speed up/down).

## Architecture

```
                          Kubernetes Cluster
                         ┌─────────────────────────────┐
  logtap tap ──────────► │  workload + logtap-forwarder│
                         │  (sidecar reads pod logs)   │
                         └──────────┬──────────────────┘
                                    │ Loki push API
                                    ▼
  logtap recv ──► HTTP server ──► writer ──► rotator ──► capture/
                   │                                       ├── metadata.json
                   ├── redactor (PII)                      ├── index.jsonl
                   ├── audit logger                        ├── *.jsonl.zst
                   └── TUI (stats + log pane)              └── audit.jsonl

  logtap open <capture/>      replay with TUI
  logtap inspect <capture/>   index-only summary (instant)
  logtap slice <capture/>     filtered copy to new capture
  logtap export <capture/>    parquet / CSV / JSONL
  logtap triage <capture/>    parallel anomaly scan
```

### Capture directory

The capture directory **is** the portable artifact. No double-compression — files are already zstd-compressed by the rotator. Transfer with `tar cf`, `rsync`, or `scp`.

```
capture/
  metadata.json              # written on start, updated on exit
  index.jsonl                # label-to-file index, one line per rotated file
  audit.jsonl                # connection metadata audit trail
  2024-01-15T103201-000.jsonl.zst
  2024-01-15T103512-000.jsonl.zst
```

## Documentation

- [API Stability](docs/api-stability.md) — what is stable across versions
- [Troubleshooting](docs/troubleshooting.md) — common failure modes and solutions
- [Examples](docs/examples/) — copy-paste workflow scripts
  - [Load test workflow](docs/examples/load-test-workflow.sh) — full tap/recv/triage/export pipeline
  - [Multi-namespace tap](docs/examples/multi-namespace.sh) — tapping across namespaces
  - [CI integration](docs/examples/ci-integration.sh) — compare captures in CI
  - [DuckDB analysis](docs/examples/duckdb-analysis.sql) — query parquet exports

## Security and safety

logtap is designed to be safe for production-adjacent use during load testing and incident investigation.

**Data safety:**
- **PII redaction** — `--redact` strips emails, credit card numbers, JWTs, bearer tokens, IPs, SSNs, and phone numbers before bytes hit disk. Custom patterns supported via YAML
- **Audit trail** — every connection, push, and rotation event is logged to `audit.jsonl` inside the capture directory
- **Bounded resources** — `--max-disk` and `--max-file` enforce hard caps. When disk is full, oldest files are rotated out. The receiver never blocks the sender
- **No upstream impact** — sidecar injection is read-only. The forwarder reads existing pod logs; it does not modify application logging or intercept traffic
- **Clean removal** — `logtap untap` removes all injected sidecars. `logtap status` detects orphaned sidecars. `logtap check` validates cluster state

**Network safety:**
- **Localhost by default** — receiver binds to `127.0.0.1:3100`, not `0.0.0.0`
- **TLS support** — `--tls-cert` and `--tls-key` for encrypted transport
- **Webhook auth** — bearer tokens or HMAC-SHA256 signatures for webhook notifications
- **Service mesh aware** — auto-detects Linkerd/Istio and adds sidecar bypass annotations

**File safety:**
- **Restrictive permissions** — capture files are written with `0600`/`0700` permissions
- **Path traversal protection** — cloud download validates all object keys against directory escape
- **No secrets in captures** — redaction happens in the receive pipeline, before the writer

**Production guardrails:**
- **`--allow-prod` required** — tapping production namespaces requires an explicit flag
- **`--force` required** — namespace-wide tap (`--all`) requires explicit confirmation
- **Dry-run support** — `--dry-run` on `tap`, `untap`, and `deploy` shows changes without applying
- **Auto-rollback** — failed sidecar injection automatically rolls back the workload

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Internal error |
| `2` | Invalid arguments |
| `3` | Not found (missing capture, file, or resource) |
| `4` | Permission denied |
| `5` | Network error (recoverable — agent can retry) |
| `6` | Findings detected (triage anomalies or check failures) |

## Project Status

**Status: Stable** · **v1.3.0** · Maintenance mode

| Milestone | Status |
|-----------|--------|
| Core functionality | Complete |
| Test coverage >85% | Complete |
| Security audit | Complete |
| golangci-lint config | Complete |
| CI pipeline (test/lint/scan) | Complete |
| Homebrew distribution | Complete |
| Safety model documented | Complete |
| API stability guarantees | Complete |
| v1.0 release | Complete |

Feature-complete. Bug fixes and security patches only. New features driven by user feedback.

## Known limitations

- **Capture format** — capture directory structure is stable; compression codec may evolve
- **Sidecar resource overhead** — each sidecar adds 16Mi/25m requests by default
- **No browser UI** — TUI only, terminal required
- **No CRDs or operators** — imperative CLI workflow
- **No long-term retention** — bounded disk, oldest files deleted automatically
- **Pod restart on tap** — sidecar injection triggers a pod restart (see below)

### Scanning a live capture

`logtap triage`, `grep`, `slice`, and `export` can safely run against a capture directory that is still receiving logs. File rotation may delete old data files during a long-running scan — these are skipped gracefully. Triage additionally performs a catch-up pass after the main scan to pick up files that were created by rotation during the initial scan. Line counts may differ slightly from the final capture since rotation is concurrent.

### Image availability on tap

`logtap tap` injects a forwarder sidecar into workloads, which triggers a pod restart. During restart, Kubernetes pulls both the application image and the forwarder image (`ghcr.io/ppiankov/logtap-forwarder`). If either image is unavailable — registry down, credentials expired, image deleted, air-gapped cluster — the pod will enter `ImagePullBackOff` and fail to start.

**Before tapping in production-adjacent environments:**
1. Use `--dry-run` first to preview changes without applying
2. Verify the forwarder image is pullable: `kubectl run --rm -it --image ghcr.io/ppiankov/logtap-forwarder:latest --restart=Never test -- /bin/true`
3. Verify the application image is still in your registry — long-running pods may reference images that have since been garbage-collected

If your cluster has image availability risks (stale registries, harbor cleanup policies, air-gapped nodes), consider running [tote](https://github.com/ppiankov/tote) — an emergency operator that detects `ImagePullBackOff` and salvages cached images from other nodes via node-to-node transfer.

## License

[MIT](LICENSE)
