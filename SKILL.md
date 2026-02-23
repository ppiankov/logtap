---
name: logtap
description: Ephemeral log capture and incident triage for Kubernetes — tap in, grab logs, triage, tap out
user-invocable: false
metadata: {"requires":{"bins":["logtap"]}}
---

# logtap — Ephemeral Log Capture

Ephemeral log capture and incident triage tool for Kubernetes. Install, capture, triage, uninstall. No permanent footprint.

## Install

```bash
brew install ppiankov/tap/logtap
```

## Commands

### logtap triage

Scan captured logs for anomalies and produce report.

**Flags:**
- `--format json` — output as JSON (use --json flag)
- `--html` — generate HTML report
- `--jobs` — parallel scan workers

**JSON output:**
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

**Exit codes:**
- 0: success
- 1: internal error
- 2: invalid arguments
- 3: not found (missing capture)
- 6: findings (anomalies found)

### logtap report

Single-command incident report (inspect + triage).

**Flags:**
- `--format json` — JSON output
- `--out` — output directory for JSON + HTML artifacts

### logtap recv

Start log receiver (local or in-cluster).

**Flags:**
- `--dir` — output directory for captured logs
- `--max-disk` — max total disk usage
- `--redact` — enable PII redaction
- `--headless` — disable TUI

### logtap tap

Inject log-forwarding sidecar.

**Flags:**
- `--deployment` — target deployment name
- `--target` — receiver address
- `--dry-run` — show diff without applying
- `--namespace` — Kubernetes namespace

### logtap untap

Remove sidecar.

**Flags:**
- `--deployment` — target deployment name

### logtap grep

Search capture for matching entries.

**Flags:**
- `--format json` — output format: json, text
- `--sort` — sort output chronologically

### logtap inspect

Show capture summary.

**Flags:**
- `--format json` — JSON output

### logtap version

Print version.

### logtap init

Not implemented. No config file required — ephemeral by design.

## What this does NOT do

- Does not persist after use — ephemeral by design
- Does not stream to external services — captures locally
- Does not use ML — deterministic anomaly pattern matching
- Does not require persistent cluster access — tap in, capture, tap out

## Parsing examples

```bash
# Incident capture workflow
logtap recv --dir ./capture --max-disk 1GB --redact --headless &
logtap tap --deployment api-gateway --target localhost:3100
logtap report ./capture --format json

# Triage — high severity only
logtap triage ./capture --format json | jq '.anomalies[] | select(.severity == "high")'

# Search logs
logtap grep "error|panic" ./capture --sort | jq '.message'

# Capture summary
logtap inspect ./capture --format json | jq '{files, entries, labels}'
```
