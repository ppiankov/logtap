---
name: logtap
description: Ephemeral log capture and incident triage for Kubernetes — tap in, grab logs, triage, tap out
user-invocable: false
metadata: {"requires":{"bins":["logtap"]}}
---

# logtap — Ephemeral Log Capture

You have access to `logtap`, an ephemeral log capture and incident triage tool for Kubernetes. Install, capture, triage, uninstall. No permanent footprint.

## Install

```bash
brew install ppiankov/tap/logtap
```

## Commands

| Command | What it does |
|---------|-------------|
| `logtap recv --dir <dir>` | Start log receiver (local or in-cluster) |
| `logtap tap --deployment <name> --target <addr>` | Inject log-forwarding sidecar |
| `logtap untap --deployment <name>` | Remove sidecar |
| `logtap open <dir>` | Replay capture in TUI |
| `logtap inspect <dir>` | Show capture summary |
| `logtap inspect <dir> --json` | Machine-readable capture summary |
| `logtap triage <dir>` | Scan for anomalies and produce report |
| `logtap triage <dir> --json` | Machine-readable triage report |
| `logtap grep <dir> <pattern>` | Search capture for matching entries |
| `logtap export <dir> --format jsonl --out <file>` | Export to parquet, CSV, or JSONL |
| `logtap slice <dir> --out <dir>` | Extract time/label subset |
| `logtap merge <dirs...>` | Merge captures |
| `logtap diff <dir1> <dir2>` | Diff captures |
| `logtap check` | Validate cluster readiness |
| `logtap status` | Show tapped workloads and receiver stats |
| `logtap version` | Print version |

## Key Flags

| Flag | Applies to | Description |
|------|-----------|-------------|
| `--dir` | recv | Output directory for captured logs |
| `--max-disk` | recv | Max total disk usage (e.g., 1GB) |
| `--redact` | recv | Enable PII redaction |
| `--in-cluster` | recv | Deploy receiver as in-cluster pod |
| `--headless` | recv | Disable TUI, log to stderr |
| `--deployment` | tap, untap | Target deployment name |
| `--target` | tap | Receiver address |
| `--dry-run` | tap | Show diff without applying |
| `--format` | grep, export | Output format: json, text, jsonl, parquet, csv |
| `--from`, `--to` | grep, export, slice, open | Time range filter |
| `--label` | grep, export, slice, open | Label filter (repeatable) |
| `--json` | inspect, triage, version | JSON output |
| `--html` | triage | Generate HTML report |
| `--jobs` | triage | Parallel scan workers |

## Agent Usage Pattern

### Incident capture workflow

```bash
logtap check                                   # verify cluster readiness
logtap recv --dir ./capture --max-disk 1GB --redact --headless &
logtap tap --deployment api-gateway --target localhost:3100
# ... wait for logs ...
logtap untap --deployment api-gateway
logtap inspect ./capture --json                # what did we get?
logtap triage ./capture --json                 # anomaly scan
```

### Parsing Examples

```bash
# Capture summary
logtap inspect ./capture --json | jq '{files, entries, labels}'

# Triage results
logtap triage ./capture --json | jq '.anomalies[] | {type, severity, message}'

# Search logs
logtap grep ./capture "error|panic" --format json | jq '.message'

# Export for external analysis
logtap export ./capture --format jsonl --out logs.jsonl
```

## Exit Codes

- `0` — success
- `1` — error
- `2` — findings (triage anomalies found, or check failures)

## What logtap Does NOT Do

- Does not persist after use — ephemeral by design
- Does not stream to external services — captures locally
- Does not use ML — deterministic anomaly pattern matching
- Does not require persistent cluster access — tap in, capture, tap out
