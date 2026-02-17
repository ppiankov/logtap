<p align="center">
  <img src="assets/logo.svg" alt="logtap" width="128">
</p>

# logtap

[![CI](https://github.com/ppiankov/logtap/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/logtap/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ppiankov/logtap)](https://goreportcard.com/report/github.com/ppiankov/logtap)
[![Go 1.22+](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

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
logtap recv --listen :3100 --dir ./capture --max-disk 1GB --redact

# Send test data
curl -X POST http://localhost:3100/loki/api/v1/push \
  -H 'Content-Type: application/json' \
  -d '{"streams":[{"stream":{"app":"test"},"values":[["1234567890000000000","hello world"]]}]}'

# Replay
logtap open ./capture
```

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
| `logtap tap` | Inject log-forwarding sidecar into workloads |
| `logtap untap` | Remove sidecar from workloads |
| `logtap check` | Validate cluster readiness and detect leftovers |
| `logtap status` | Show tapped workloads and receiver stats |

### Key flags

```bash
# Receiver
logtap recv --listen :3100 --dir ./capture --max-disk 50GB --redact
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
| `/` | Search (regex) |
| `n` / `N` | Next / previous match |
| `q` | Quit |

Replay mode adds `space` (pause/resume) and `]`/`[` (speed up/down).

## Architecture

```
                          Kubernetes Cluster
                         ┌─────────────────────────────┐
  logtap tap ──────────► │  workload + logtap-forwarder │
                         │  (sidecar reads pod logs)    │
                         └──────────┬──────────────────┘
                                    │ Loki push API
                                    ▼
  logtap recv ──► HTTP server ──► writer ──► rotator ──► capture/
                   │                                      ├── metadata.json
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

## Known limitations

- **Capture format** — capture directory structure is stable; compression codec may evolve
- **Sidecar resource overhead** — each sidecar adds 16Mi/25m requests by default
- **No browser UI** — TUI only, terminal required
- **No CRDs or operators** — imperative CLI workflow
- **No long-term retention** — bounded disk, oldest files deleted automatically

## License

[MIT](LICENSE)
