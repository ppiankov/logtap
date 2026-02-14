# logtap Work Orders

## Phase 1: Receiver (Week 1)

### WO-01: HTTP receiver with Loki push API

**Goal**: Accept log streams via `POST /loki/api/v1/push` and write to disk.

**CLI interface**:
```
logtap recv --listen :9000 --dir ./capture --max-disk 50GB
```

**Flags**:
| Flag | Default | Description |
|------|---------|-------------|
| `--listen` | `:9000` | Address to bind |
| `--dir` | `./capture` | Output directory |
| `--max-disk` | `50GB` | Hard cap on total disk usage |
| `--max-file` | `1GB` | Rotate file at this size |
| `--compress` | `true` | Compress rotated files with zstd |

**Behavior**:
- Accept `POST /loki/api/v1/push` (Loki push API format)
- Also accept raw newline-delimited JSON on `POST /logtap/raw`
- Parse Loki push payload -> extract log lines with labels
- Write to JSONL: `{"ts":"...","labels":{...},"msg":"..."}`
- Rotate files at `--max-file` size
- On rotation: append entry to `index.jsonl` (file, time range, line count, labels seen)
- Write `metadata.json` on start, update on exit
- Delete oldest files when `--max-disk` exceeded (also prune index entries)
- Never block sender -- return 200 immediately, drop if buffer full
- Expose `/metrics` for own health (prometheus format)

**Internal metrics**:
- `logtap_logs_received_total`
- `logtap_logs_dropped_total`
- `logtap_bytes_written_total`
- `logtap_disk_usage_bytes`
- `logtap_active_connections`
- `logtap_backpressure_events_total`

**Files**:
- `cmd/logtap/main.go` -- cobra root + recv subcommand
- `internal/recv/server.go` -- HTTP server, Loki push handler
- `internal/recv/writer.go` -- buffered JSONL writer
- `internal/rotate/rotator.go` -- file rotation + disk cap enforcement
- `internal/rotate/rotator_test.go`

**Verification**:
```bash
# Start receiver
logtap recv --listen :9000 --dir /tmp/capture --max-disk 1GB

# Send test data
curl -X POST http://localhost:9000/loki/api/v1/push \
  -H 'Content-Type: application/json' \
  -d '{"streams":[{"stream":{"app":"test"},"values":[["1234567890000000000","hello world"]]}]}'

# Verify written
cat /tmp/capture/*.jsonl | jq .

# Stress test
go test -run TestBackpressure -race ./internal/recv/
```

---

### WO-02: Live TUI with data fill

**Goal**: TUI that shows live stats AND fills with arriving log data.

**Display**:
```
logtap v0.1.0 | :9000 | ./capture

 Stats                          Top talkers
 Connections:  3                 api-service       21,412/s
 Logs/sec:     48,232            worker-service    14,087/s
 Bytes/sec:    62 MB/s           gateway            9,201/s
 Disk used:    14.2 / 50.0 GB
 Dropped:      0

 ──────────────────────────────────────────────────────────
 2024-01-15T10:32:01Z [api-service] POST /api/v1/users 200 12ms
 2024-01-15T10:32:01Z [api-service] POST /api/v1/orders 201 45ms
 2024-01-15T10:32:02Z [worker-service] processing job=a1b2c3 duration=230ms
 2024-01-15T10:32:02Z [gateway] upstream timeout host=payments.svc:8080
 2024-01-15T10:32:03Z [api-service] GET /healthz 200 1ms
 ...
```

**Behavior**:
- Top pane: stats + top talkers (update every 1s)
- Bottom pane: scrolling log lines as they arrive (tail -f style)
- Drop counter shown in red if > 0
- On exit: flush buffers, close files

**Keyboard (vim-style)**:
| Key | Action |
|-----|--------|
| `j` / `k` | Scroll down / up one line |
| `d` / `u` | Half-page down / up |
| `G` | Jump to bottom (latest) |
| `gg` | Jump to top (oldest in buffer) |
| `f` | Toggle follow mode (auto-scroll to new lines) |
| `/` | Enter search mode (regex) |
| `n` / `N` | Next / previous search match |
| `q` / Ctrl+C | Quit |

**Dependencies**: `github.com/charmbracelet/bubbletea` + `lipgloss`

**Files**:
- `internal/recv/tui.go` -- bubbletea model, views
- `internal/recv/stats.go` -- atomic counters for live stats
- `internal/recv/logview.go` -- ring buffer of recent log lines for display

**Verification**: Visual -- run recv with a sender, confirm stats update and logs scroll.

---

### WO-02a: Capture directory and replay

**Goal**: Capture directory IS the portable artifact. Second user opens it with same TUI.

The receiver already writes zstd-compressed rotated JSONL files. Double-compressing
into tar.gz is wasteful and breaks at 20-100GB. Instead, the capture directory itself
is the shareable artifact — just `tar` (no compression), `rsync`, or `scp` to transfer.

**Capture directory layout**:
```
./capture/
  metadata.json                          # written on recv start, updated on exit
  index.jsonl                            # label-to-file index, appended per rotation
  2024-01-15T103201-000.jsonl.zst       # rotated + compressed by WO-01
  2024-01-15T103201-001.jsonl.zst
  2024-01-15T103512-000.jsonl.zst
```

`metadata.json`:
```json
{
  "version": 1,
  "format": "jsonl+zstd",
  "started": "2024-01-15T10:32:01Z",
  "stopped": "2024-01-15T12:45:33Z",
  "total_lines": 14832901,
  "total_bytes": 8432901234,
  "labels_seen": ["api-service", "worker-service", "gateway"]
}
```

`index.jsonl` — one line per rotated file, written at rotation time:
```json
{"file":"2024-01-15T103201-000.jsonl.zst","from":"2024-01-15T10:32:01Z","to":"2024-01-15T10:35:12Z","lines":482901,"bytes":234567890,"labels":{"app":{"api-service":312000,"worker-service":170901},"namespace":{"default":482901}}}
{"file":"2024-01-15T103512-000.jsonl.zst","from":"2024-01-15T10:35:12Z","to":"2024-01-15T10:41:33Z","lines":391002,"bytes":198234567,"labels":{"app":{"api-service":201000,"gateway":190002},"namespace":{"payments":391002}}}
```

`labels` is `map[key]map[value]line_count` — enables `logtap inspect` to show
per-label breakdowns and `logtap slice --label` to skip files without scanning.
At 100GB with 1GB rotation = ~100 index lines. Negligible overhead.

**On recv exit**:
```
Flushing buffers... done
Capture: ./capture (8.4 GB, 14.8M lines)
Transfer: tar cf - ./capture | ssh user@host 'tar xf -'
         or: rsync -av ./capture user@host:~/
```

**Replay**:
```
logtap open ./capture
logtap open ./capture --speed 10x
logtap open ./capture --speed 0                           # instant load
logtap open ./capture --from 10:32 --to 10:45             # time window
logtap open ./capture --label app=api-service             # label filter
logtap open ./capture --from 10:32 --label app=gateway    # combined
```

**Filter flags**:
| Flag | Description |
|------|-------------|
| `--from` | Start time (absolute or relative: `10:32`, `2024-01-15T10:32:00Z`, `-30m`) |
| `--to` | End time (same formats) |
| `--label` | Label filter (`key=value`), repeatable |
| `--grep` | Regex filter on message content |

Filters use `index.jsonl` to skip irrelevant files entirely — at 100GB this means
opening only the files that contain matching time ranges and labels.

**Behavior**:
- Reads JSONL files from directory in timestamp order (decompresses zstd on the fly)
- Renders same TUI as live mode (stats pane + log pane, vim keys)
- Replays at original speed by default
- `--speed 10x` to fast-forward, `--speed 0` for instant load (all lines at once)
- Same vim-style navigation as live mode plus:

| Key | Action |
|-----|--------|
| `space` | Pause / resume replay |
| `]` / `[` | Speed up / slow down (2x steps) |

- Read-only -- no receiver, no disk writes

**Files**:
- `internal/archive/metadata.go` -- metadata.json read/write
- `internal/archive/replay.go` -- timestamp-ordered JSONL reader with zstd decompression
- `cmd/logtap/open.go` -- open subcommand

**Verification**:
```bash
# Capture some data
logtap recv --dir /tmp/capture --max-disk 100MB
# (send data, then Ctrl+C)

# Replay locally
logtap open /tmp/capture

# Transfer and replay on another machine
tar cf - /tmp/capture | ssh colleague@host 'tar xf -'
# colleague runs: logtap open ./capture
```

---

### WO-02b: Inspect capture

**Goal**: `logtap inspect` shows what's inside a capture directory without replaying it.

**CLI interface**:
```
logtap inspect ./capture
```

**Output**:
```
Capture: ./capture
Format:  jsonl+zstd (v1)
Period:  2024-01-15 10:32:01 — 12:45:33 (2h 13m)
Size:    8.4 GB (23 files)
Lines:   14,832,901

Labels:
  app:
    api-service          8,412,001 lines   4.2 GB   (56.7%)
    worker-service       4,102,300 lines   2.8 GB   (27.7%)
    gateway              2,318,600 lines   1.4 GB   (15.6%)

  namespace:
    default             12,514,301 lines   7.0 GB   (84.4%)
    payments             2,318,600 lines   1.4 GB   (15.6%)

  container:
    app                 14,832,901 lines   8.4 GB  (100.0%)

Timeline (1-min buckets):
  10:32 ▂▃▅▇▇▇▇▇▇▇▇▇▆▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅
  11:02 ▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅
  11:32 ▅▅▅▅▅▅▅▅▇█████▇▇▅▅▅▅▅▅▅▅▅▅▅▅▅▅  <- spike
  12:02 ▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▃▃▃▂▂▂▂▂▂▂▁▁▁
  12:32 ▁▁▁▁▁▁▁▁▁▁▁▁▁
```

**Behavior**:
- Reads `metadata.json` and `index.jsonl` only — no decompression, instant even at 100GB
- Groups by each label key (app, namespace, container, pod, etc.)
- Shows line count, size, and percentage per label value
- Timeline: sparkline histogram from index time ranges (coarse, not per-line)
- `--json` flag outputs machine-readable JSON for scripting

**Use case**: colleague sends you a 50GB capture. Run `logtap inspect` to see which
services are represented, time window, and where the volume spikes are — before
committing to a full replay or slice.

**Files**:
- `cmd/logtap/inspect.go` -- inspect subcommand
- `internal/archive/inspect.go` -- index-based summary aggregation

**Verification**:
```bash
logtap inspect ./capture
logtap inspect ./capture --json | jq '.labels.app'
```

---

### WO-02c: Slice (extract subset)

**Goal**: Extract a time range and/or label filter into a new smaller capture directory.

**CLI interface**:
```
logtap slice ./capture --from 10:32 --to 10:45 --out ./incident-42
logtap slice ./capture --label app=gateway --out ./gateway-only
logtap slice ./capture --from 10:32 --to 10:45 --label app=gateway --out ./incident-42
logtap slice ./capture --grep "timeout|5xx" --out ./errors-only
```

**Behavior**:
- Reads source capture directory using index.jsonl to skip irrelevant files
- Writes matching lines to new capture directory (same format: JSONL+zstd, new index, new metadata)
- Output is a valid capture directory -- `logtap open` works on it
- Shows progress: `Slicing: 482,901 / 14,832,901 lines (3.2%)`
- Same filter flags as `logtap open`: `--from`, `--to`, `--label`, `--grep`

**Use case**: captured 8 hours at 100GB, the bug window is 10 minutes. Slice to 500MB, send that to colleague.

**Files**:
- `cmd/logtap/slice.go` -- slice subcommand
- `internal/archive/slice.go` -- filtered copy with index-aware file skipping

**Verification**:
```bash
logtap slice ./capture --from 10:32 --to 10:45 --out /tmp/incident
ls /tmp/incident/
# metadata.json index.jsonl *.jsonl.zst
logtap open /tmp/incident
```

---

### WO-02d: Export to external formats

**Goal**: Convert capture data to parquet/CSV for ingestion into analytics systems.

**CLI interface**:
```
logtap export ./capture --format parquet --out ./capture.parquet
logtap export ./capture --format csv --out ./capture.csv
logtap export ./capture --format parquet --from 10:32 --to 10:45 --out ./incident.parquet
```

**Formats**:
| Format | Use case |
|--------|----------|
| `parquet` | DuckDB, BigQuery, Clickhouse, Spark, pandas |
| `csv` | Excel, any tool |
| `jsonl` | Already native format, useful with filters to get uncompressed plaintext |

**Parquet schema**:
```
ts:       TIMESTAMP (nanosecond)
labels:   MAP<STRING, STRING>
msg:      STRING
```

**Behavior**:
- Same filter flags as open/slice: `--from`, `--to`, `--label`, `--grep`
- Parquet: single file output, row-group per source JSONL file
- CSV: `ts,labels,msg` columns, labels as `key=val;key=val`
- Shows progress: `Exporting: 482,901 lines -> capture.parquet (124 MB)`

**Dependencies**: `github.com/parquet-go/parquet-go` (pure Go, no C deps)

**Files**:
- `cmd/logtap/export.go` -- export subcommand
- `internal/archive/parquet.go` -- parquet writer
- `internal/archive/csv.go` -- csv writer

**Verification**:
```bash
logtap export ./capture --format parquet --out /tmp/capture.parquet
duckdb -c "SELECT count(*) FROM '/tmp/capture.parquet'"
duckdb -c "SELECT labels['app'], count(*) FROM '/tmp/capture.parquet' GROUP BY 1"
```

---

## Phase 2: Cluster Integration (Week 2)

### WO-03: Sidecar injection

**Goal**: `logtap tap` injects a log-forwarding sidecar into target workloads.

Replaces the previous config-patching approach. Instead of modifying Alloy/Vector
configs (which conflicts with Helm/GitOps/ArgoCD), logtap injects an ephemeral
sidecar container that reads the pod's log stream and forwards to the receiver.
Same pattern as Linkerd proxy injection -- plug and play, no agent dependency.

**CLI interface**:
```
logtap tap --deployment api-gateway --target logtap.logtap:9000
logtap tap --namespace payments --target logtap.logtap:9000
logtap tap --selector app=worker --target logtap.logtap:9000
logtap tap --dry-run
```

**How it works**:
1. Patch target deployment/statefulset/daemonset spec:
   - Add annotation `logtap.dev/tapped: "true"`
   - Add sidecar container `logtap-forwarder` to pod template
   - Sidecar shares the pod's log volumes via `emptyDir` or reads from
     `/var/log/containers` (hostPath, same as logging agents)
2. Sidecar container:
   - Minimal image: `ghcr.io/ppiankov/logtap-forwarder:latest`
   - Reads container stdout/stderr via shared volume or Kubernetes API log endpoint
   - Forwards to receiver via Loki push API (`POST /loki/api/v1/push`)
   - Labels: namespace, pod, container, app (from pod labels)
   - No buffering beyond 1MB -- drop if receiver unreachable
   - Resource limits: 32Mi memory, 50m CPU
3. Triggers rolling restart (new pod spec = new pods)
4. Show diff before applying (`--dry-run` is default for first run)

**Why sidecar over config patching**:
- Works with ANY logging agent (Alloy, Vector, Fluent Bit, none)
- No Helm/GitOps/ArgoCD conflicts -- doesn't touch shared configmaps
- Per-workload isolation -- only tapped pods get the sidecar
- Clean removal -- just remove the sidecar container from spec
- No agent-specific config syntax to maintain

**Reusable code**:
- `trustwatch/internal/tunnel/relay.go` -- ephemeral pod lifecycle management
- `kubenow/internal/util/portforward.go` -- port-forward state machine

**Files**:
- `internal/sidecar/inject.go` -- sidecar container spec, patch generation
- `internal/sidecar/image.go` -- forwarder image reference + version
- `internal/k8s/patch.go` -- strategic merge patch apply + diff
- `internal/k8s/discover.go` -- find workloads by selector/name/namespace
- `cmd/logtap/tap.go` -- tap subcommand

**Sidecar forwarder** (separate minimal binary):
- `cmd/logtap-forwarder/main.go` -- reads logs, pushes to receiver
- `internal/forward/reader.go` -- container log reader
- `internal/forward/push.go` -- Loki push API client

**Verification**:
```bash
logtap tap --deployment api-gateway --target logtap.logtap:9000 --dry-run
logtap tap --deployment api-gateway --target logtap.logtap:9000
kubectl get deploy api-gateway -o jsonpath='{.spec.template.spec.containers[*].name}'
# should include: api-gateway logtap-forwarder
```

---

### WO-04: Untap (clean removal)

**Goal**: `logtap untap` removes sidecar from target workloads.

**CLI interface**:
```
logtap untap --deployment api-gateway
logtap untap --namespace payments
logtap untap --all
logtap untap --dry-run
```

**Behavior**:
- Find all workloads with `logtap.dev/tapped: "true"` annotation
- Remove `logtap-forwarder` container from pod spec
- Remove `logtap.dev/tapped` annotation
- Remove any shared volumes added by tap
- Triggers rolling restart
- Show diff before applying
- `--all` removes from every tapped workload in cluster

**Files**:
- `internal/sidecar/remove.go` -- sidecar removal patch
- `cmd/logtap/untap.go` -- untap subcommand

**Verification**:
```bash
logtap untap --deployment api-gateway --dry-run
logtap untap --deployment api-gateway
kubectl get deploy api-gateway -o jsonpath='{.spec.template.spec.containers[*].name}'
# should NOT include: logtap-forwarder
```

---

### WO-05: Network connectivity

**Goal**: Make logtap recv reachable from inside the cluster.

**Modes**:

Mode 1 -- In-cluster receiver (simplest):
```
logtap recv --in-cluster
```
- Deploys logtap as a temporary Pod + Service (`logtap` namespace)
- Dev connects via `kubectl port-forward svc/logtap 9000:9000`
- Logs written to emptyDir (ephemeral)
- `logtap untap --all` + deleting the namespace cleans up

Mode 2 -- Reverse tunnel (laptop receives):
```
logtap recv --listen :9000 --tunnel
```
- Starts local receiver on laptop
- Creates temporary in-cluster Pod + Service
- Tunnels traffic from cluster to laptop via kubectl port-forward reverse
- Sidecar containers send to `logtap.logtap:9000` inside cluster -> tunneled to laptop

Mode 3 -- Direct IP (requires network access):
```
logtap recv --listen :9000
logtap tap --deployment api-gateway --target 10.0.0.5:9000
```
- Only works if cluster pods can reach dev IP (VPN, same network)

**Reusable code**:
- `trustwatch/internal/tunnel/relay.go` -- ephemeral pod deployment + lifecycle
- `kubenow/internal/util/portforward.go` -- port-forward manager with reconnection

**Files**:
- `internal/k8s/tunnel.go` -- port-forward management
- `internal/k8s/deploy.go` -- temporary pod/service deployment
- `cmd/logtap/recv.go` -- `--in-cluster` and `--tunnel` flags

---

### WO-06: Check command

**Goal**: `logtap check` validates cluster readiness before tapping.

**CLI interface**:
```
logtap check
```

**Output**:
```
logtap check

Cluster:       aks-dev-westeurope (v1.29.2)
Namespace:     default
RBAC:          ok (can patch deployments, create pods)
Connectivity:  ok (can reach logtap.logtap:9000)

Candidate workloads:
  deployment/api-gateway       3 replicas   ready
  deployment/payments          2 replicas   ready
  deployment/worker            5 replicas   ready
  statefulset/postgres         1 replica    ready

Already tapped:
  (none)

Ready to tap. Run: logtap tap --deployment api-gateway --target logtap.logtap:9000
```

**Checks**:
- Cluster reachable (kubectl context valid)
- RBAC permissions: can get/patch deployments, can create pods (for tunnel)
- If receiver running: connectivity test (HTTP GET to receiver /metrics)
- List candidate workloads (deployments, statefulsets, daemonsets)
- Show already-tapped workloads
- Warn if prod-labeled namespace without `--allow-prod`

**Files**:
- `cmd/logtap/check.go` -- check subcommand
- `internal/k8s/check.go` -- permission and connectivity checks

**Verification**:
```bash
logtap check
# should show cluster info, RBAC ok, list workloads
```

---

### WO-07: Status command

**Goal**: Show what's currently tapped.

**CLI interface**:
```
logtap status
```

**Output**:
```
Receiver:    logtap.logtap:9000 (in-cluster)
Uptime:      2h 14m

Tapped workloads:
  deployment/api-gateway    (default)     3/3 pods forwarding
  deployment/payments       (default)     2/2 pods forwarding

Total throughput:  48,232 logs/sec  (62 MB/s)
Disk used:         14.2 / 50.0 GB
Dropped:           0
```

**Behavior**:
- List all workloads with `logtap.dev/tapped` annotation
- For each: check if sidecar container is running in all pods
- If receiver is reachable: show throughput and disk stats from `/metrics`
- If receiver unreachable: show "receiver: not reachable"

**Files**:
- `cmd/logtap/status.go` -- status subcommand
- `internal/k8s/status.go` -- tapped workload discovery + pod health

**Verification**:
```bash
logtap tap --deployment api-gateway --target logtap.logtap:9000
logtap status
# should show api-gateway as tapped with pod counts
```

---

## Phase 3: Hardening (Week 3)

### WO-08: Backpressure and stress testing

**Goal**: Verify receiver handles 100+ MB/s without blocking senders.

**Tests**:
- 100 MB/s sustained write for 5 minutes
- Disk full scenario (verify oldest files deleted, counter incremented)
- Sender faster than writer (verify drop counter, never blocks)
- Connection storm (100 concurrent connections)
- Graceful shutdown (Ctrl+C flushes all buffers)
- Sidecar resource limits: verify forwarder stays within 32Mi/50m

**Files**:
- `internal/recv/stress_test.go`
- `internal/rotate/rotate_test.go`

---

### WO-09: Security guardrails

**Goal**: Prevent PII/PCI leaks by default.

**Behavior**:
- Default: refuse to tap prod-labeled namespaces
- Flag `--allow-prod` overrides (with warning banner in TUI)
- Optional `--redact` flag with regex patterns for PII
- TLS support via `--tls-cert` and `--tls-key`
- All traffic logged to audit file (connection source, bytes, duration)

**Files**:
- `internal/recv/security.go`
- `internal/recv/redact.go`

---

## Command Summary

| Command | Description |
|---------|-------------|
| `logtap recv` | Start receiver (local, in-cluster, or tunnel) |
| `logtap open` | Open and replay a capture directory |
| `logtap inspect` | Show labels, timeline, and stats of a capture |
| `logtap slice` | Extract time/label subset to new capture directory |
| `logtap export` | Convert capture to parquet/CSV |
| `logtap check` | Validate cluster readiness |
| `logtap tap` | Inject sidecar into workloads |
| `logtap untap` | Remove sidecar from workloads |
| `logtap status` | Show tapped workloads and stats |

## Deliberate Non-Goals (v1)

- No metrics ingestion
- No trace ingestion
- No full-text search engine (file-level index only, content search via rg/jq/grep)
- No browser UI
- No CRDs or operators
- No long-term retention management
- No OpenTelemetry collector role
- No logging agent config patching (sidecar instead)
- No Fluent Bit support (v1.1)
