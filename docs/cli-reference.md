# CLI Reference

## Commands

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

## Key flags

### Receiver

```bash
logtap recv --dir ./capture --max-disk 50GB --redact              # localhost:3100 (default)
logtap recv --listen :3100 --dir ./capture                       # all interfaces
logtap recv --headless                           # no TUI, log to stderr
logtap recv --tls-cert cert.pem --tls-key key.pem
logtap recv --in-cluster --image ghcr.io/ppiankov/logtap-forwarder:latest
```

### Sidecar injection

```bash
logtap tap --deployment api-gateway --target host:3100
logtap tap --namespace payments --allow-prod --target host:3100
logtap tap --selector app=worker --target host:3100             # tap by label
logtap tap --namespace payments --all --force --target host:3100 # tap all workloads
logtap untap --deployment api-gateway
```

### Replay

```bash
logtap open ./capture --speed 10x
logtap open ./capture --from 10:32 --to 10:45 --label app=gateway
```

### Export

```bash
logtap export ./capture --format parquet --out capture.parquet
logtap export ./capture --format csv --grep "error|timeout" --out errors.csv
```

### Grep

```bash
logtap grep "error|timeout" ./capture                             # search all files
logtap grep "ORD-12345" ./capture --format text                   # human-readable timeline
logtap grep "tracking-id-abc123" ./capture --sort                 # chronological JSONL
logtap grep "OOMKilled" ./capture --label app=worker --count      # count per file
logtap grep "panic" ./capture -C 3                                # 3 context lines around matches
```

### Diff and baseline comparison

```bash
logtap diff ./before ./after --json                               # structural diff
logtap diff ./baseline ./current --baseline --json                # regression verdict
```

### Cloud upload / download

```bash
logtap upload ./capture s3://bucket/prefix
logtap download s3://bucket/prefix --out ./capture
```

### Webhook auth

```bash
logtap recv --dir ./capture --webhook http://hook --webhook-auth bearer:my-token
logtap recv --dir ./capture --webhook http://hook --webhook-auth hmac-sha256:secret
```

### PII redaction

```bash
# All built-in patterns (email, credit_card, jwt, bearer, ip_v4, ssn, phone)
logtap recv --redact --dir ./capture
# Specific patterns only
logtap recv --redact=email,jwt --dir ./capture
# Custom patterns from YAML
logtap recv --redact --redact-patterns ./patterns.yaml --dir ./capture
```

### JSON output

Available on most commands:

```bash
logtap slice ./capture --label app=web --out ./slice --json
logtap merge ./a ./b --out ./merged --json
logtap snapshot ./capture --output capture.tar.zst --json
```

### Triage

```bash
logtap triage ./capture --out ./triage --jobs 8
```

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
