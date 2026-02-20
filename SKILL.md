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

Or download binary:

```bash
curl -LO https://github.com/ppiankov/logtap/releases/latest/download/logtap_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m).tar.gz
tar -xzf logtap_*.tar.gz
sudo mv logtap /usr/local/bin/
```

## Commands

| Command | What it does |
|---------|-------------|
| `logtap recv --dir <dir>` | Start log receiver (local or in-cluster) |
| `logtap tap --deployment <name> --target <addr>` | Inject log-forwarding sidecar |
| `logtap untap --deployment <name>` | Remove sidecar |
| `logtap deploy --namespace <ns>` | Deploy receiver as in-cluster pod + service |
| `logtap open <dir>` | Replay capture in TUI |
| `logtap inspect <dir>` | Show capture summary |
| `logtap inspect <dir> --json` | Machine-readable capture summary |
| `logtap triage <dir>` | Scan for anomalies and produce report |
| `logtap triage <dir> --json` | Machine-readable triage report |
| `logtap grep <pattern> <dir>` | Search capture for matching entries |
| `logtap grep <pattern> <dir> --format text` | Human-readable timeline (sorted) |
| `logtap export <dir> --format jsonl --out <file>` | Export to parquet, CSV, or JSONL |
| `logtap slice <dir> --out <dir>` | Extract time/label subset |
| `logtap merge <dirs...>` | Merge captures |
| `logtap diff <dir1> <dir2>` | Diff captures |
| `logtap watch <dir>` | Tail a live or completed capture |
| `logtap upload <dir>` | Upload capture to S3/GCS |
| `logtap download <url> --out <dir>` | Download capture from S3/GCS |
| `logtap check` | Validate cluster readiness |
| `logtap status` | Show tapped workloads and receiver stats |
| `logtap version --json` | Print version as JSON |

## Key Flags

| Flag | Applies to | Description |
|------|-----------|-------------|
| `--dir` | recv | Output directory for captured logs |
| `--max-disk` | recv | Max total disk usage (e.g., 1GB) |
| `--redact` | recv | Enable PII redaction |
| `--in-cluster` | recv | Deploy receiver as in-cluster pod |
| `--headless` | recv | Disable TUI, log to stderr |
| `--deployment` | tap, untap | Target deployment name |
| `--statefulset` | tap, untap | Target statefulset name |
| `--daemonset` | tap, untap | Target daemonset name |
| `--selector` | tap, untap | Label selector for multi-workload targeting |
| `--target` | tap | Receiver address (supports `https://` for TLS) |
| `--dry-run` | tap, deploy | Show diff without applying |
| `--format` | grep, export | Output format: json, text, jsonl, parquet, csv |
| `--sort` | grep | Sort output chronologically by timestamp |
| `--count` | grep | Count matches per file instead of printing entries |
| `--from`, `--to` | grep, export, slice, open | Time range filter |
| `--label` | grep, export, slice, open | Label filter (repeatable) |
| `--json` | inspect, triage, check, version | JSON output |
| `--html` | triage | Generate HTML report |
| `--jobs` | triage | Parallel scan workers |
| `--namespace`, `-n` | tap, untap, check, deploy | Kubernetes namespace |

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

### Cross-service tracing

```bash
# Trace an item (order number, tracking ID) across all services — human-readable timeline
logtap grep "ORD-12345" ./capture --format text

# Same as JSONL sorted by timestamp
logtap grep "tracking-id-abc123" ./capture --sort

# Filter by label and time window
logtap grep "error" ./capture --label app=gateway --from 10:30 --to 11:00 --sort
```

### JSON Output Structure

#### inspect --json

```json
{
  "dir": "./capture",
  "files": 12,
  "entries": 48230,
  "bytes": 15728640,
  "started": "2026-02-20T10:00:00Z",
  "stopped": "2026-02-20T10:45:00Z",
  "labels": {
    "app": ["gateway", "cart-svc", "payment-svc"],
    "namespace": ["default"]
  }
}
```

#### triage --json

```json
{
  "anomalies": [
    {
      "type": "error_spike",
      "severity": "high",
      "message": "Error rate 45x baseline in cart-svc between 10:32-10:34",
      "file": "2026-02-20T103200-000.jsonl.zst",
      "count": 1247
    }
  ],
  "summary": {
    "total_entries": 48230,
    "anomaly_count": 3,
    "severity_counts": {"high": 1, "medium": 2}
  }
}
```

### Parsing Examples

```bash
# Capture summary
logtap inspect ./capture --json | jq '{files, entries, labels}'

# Triage — high severity anomalies only
logtap triage ./capture --json | jq '.anomalies[] | select(.severity == "high")'

# Search logs
logtap grep "error|panic" ./capture --sort | jq '.message'

# Count errors per file
logtap grep "error" ./capture --count

# Export for external analysis
logtap export ./capture --format parquet --out capture.parquet

# Manual sort with jq (alternative to --sort)
logtap grep "tracking-id" ./capture | jq -s 'sort_by(.ts)[]' -c
```

## Cross-Tool Integration

logtap captures can be wrapped with [chainwatch](https://github.com/ppiankov/chainwatch) for policy enforcement on cluster operations:

```bash
chainwatch exec --profile ops -- logtap tap --deployment api-gateway --target host:3100
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
- Does not replace Loki/OpenSearch/ELK — disposable capture only
