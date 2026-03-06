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

**logtap** — Ephemeral log mirror for Kubernetes load testing. Part of [SpectreHub](https://github.com/ppiankov/spectrehub).

## What it is

- Accepts Loki push API payloads and writes compressed JSONL to disk
- Injects log-forwarding sidecars into Kubernetes workloads via annotation-based opt-in
- Provides a TUI with real-time stats, top talkers, and vim-style log navigation
- Replays capture directories at original speed or fast-forward
- Analyzes captures: summarize, filter, export to parquet/CSV, and triage anomalies

## What it is NOT

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

See [TUI keybindings](docs/tui.md) for the full list of keyboard shortcuts in the live dashboard.

## SpectreHub integration

logtap feeds load test log capture summaries into [SpectreHub](https://github.com/ppiankov/spectrehub) for unified visibility across your infrastructure.

```sh
spectrehub collect --tool logtap
```

## Safety

logtap operates in **read-only mode** against your application. Sidecars are annotation-based opt-in and do not modify application containers.

## License

MIT — see [LICENSE](LICENSE).

---

Built by [Obsta Labs](https://github.com/ppiankov)
