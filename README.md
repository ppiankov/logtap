<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="assets/logo-light.svg">
    <img alt="logtap" src="assets/logo-light.svg" width="200">
  </picture>
</p>

# logtap

[![CI](https://github.com/ppiankov/logtap/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/logtap/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ppiankov/logtap)](https://goreportcard.com/report/github.com/ppiankov/logtap)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![ANCC](https://img.shields.io/badge/ANCC-compliant-brightgreen)](https://ancc.dev)

Ephemeral log mirror for Kubernetes load testing. Part of [SpectreHub](https://github.com/ppiankov/spectrehub).

Annotation-based opt-in. Accepts Loki push API, writes compressed JSONL to disk, shows a minimal TUI. Capture directories are portable — share them with `tar`, `rsync`, or `scp` and replay on any machine.

## What logtap is

- **Receiver** (`recv`) — accepts Loki push API payloads, writes rotated zstd-compressed JSONL with bounded disk usage
- **Live TUI** — real-time stats, top talkers, scrollable log pane with vim-style navigation and regex search
- **Sidecar injection** (`tap`/`untap`) — injects a log-forwarding sidecar into Kubernetes workloads, no logging agent config changes
- **Replay** (`open`) — replays capture directories at original speed or fast-forward with the same TUI
- **Analysis** (`inspect`, `slice`, `export`, `triage`) — summarize, filter, convert to parquet/CSV, or scan for anomalies
- **Cluster safety** (`check`, `status`) — validates readiness, detects orphaned sidecars, shows what's tapped

## What logtap is NOT

- Not a permanent log storage solution — ephemeral by design
- Not a replacement for Loki/Elasticsearch — captures are load-test scoped
- Not a monitoring agent — runs for the duration of a test
- Not a log shipper — receives, does not forward

## Quick start

### Homebrew

```sh
brew tap ppiankov/tap
brew install logtap
```

### From source

```sh
git clone https://github.com/ppiankov/logtap.git
cd logtap
make build
```

### Usage

```sh
logtap tap deploy/my-app && logtap recv --port 3100
```

## CLI commands

| Command | Description |
|---------|-------------|
| `logtap recv` | Start receiver accepting Loki push API payloads |
| `logtap tap` | Inject log-forwarding sidecar into a workload |
| `logtap untap` | Remove sidecar from a workload |
| `logtap open` | Replay a capture directory in the TUI |
| `logtap inspect` | Summarize a capture directory |
| `logtap slice` | Filter capture by time range or label |
| `logtap export` | Convert capture to parquet or CSV |
| `logtap triage` | Scan capture for anomalies |
| `logtap check` | Validate cluster readiness |
| `logtap status` | Show what is currently tapped |

See [CLI Reference](docs/cli-reference.md) for all commands, flags, and exit codes. See [TUI keybindings](docs/tui.md) for keyboard shortcuts.

## Agent integration

logtap follows the [ANCC convention](https://ancc.dev) — single binary, deterministic output, structured JSON, bounded jobs. No plugins or SDKs required.

Agents: read [`docs/SKILL.md`](docs/SKILL.md) for commands, flags, JSON output schemas, exit codes, and parsing examples.

Key patterns for agents:
- `logtap inspect <dir> --json` — capture summary (files, entries, labels, timeline)
- `logtap triage <dir> --json` — anomaly scan results with severity
- `logtap grep <pattern> <dir> --format text` — human-readable cross-service timeline
- `logtap check --json` — cluster readiness and orphan detection
- `logtap upload <dir> --to s3://... --share --json` — upload and return presigned URLs

## SpectreHub integration

logtap feeds load test log capture summaries into [SpectreHub](https://github.com/ppiankov/spectrehub) for unified visibility across your infrastructure.

```sh
spectrehub collect --tool logtap
```

## Philosophy

**Principiis obsta** — resist the beginnings.

- **Bounded by default** — hard disk caps, drop policies, backpressure. Never block the sender.
- **Disposable** — captures are ephemeral artifacts for debugging, not long-term archives.
- **Mirrors, not oracles** — presents evidence and lets you decide. No ML, no probabilistic magic.
- **Reversible** — sidecar injection is clean removal away. No shared config patching.
- **Explicit consent** — production namespaces require `--allow-prod`. PII redaction happens before bytes hit disk.

## Documentation

| Document | Contents |
|----------|----------|
| [Architecture](docs/architecture.md) | System design, capture format, integration modes |
| [CLI Reference](docs/cli-reference.md) | All commands, flags, and exit codes |
| [TUI Keybindings](docs/tui.md) | Keyboard shortcuts for live and replay modes |
| [Security & Safety](docs/security.md) | PII redaction, production guardrails, audit trail |
| [Known Limitations](docs/known-limitations.md) | Current constraints and edge cases |
| [API Stability](docs/api-stability.md) | What is stable across versions |
| [Troubleshooting](docs/troubleshooting.md) | Common failure modes and solutions |
| [Examples](docs/examples/) | Copy-paste workflow scripts |

## License

MIT — see [LICENSE](LICENSE).

---

Built by [Obsta Labs](https://obstalabs.dev)
