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
| `--redact` | `false` | Enable PII redaction before writing (see WO-09) |
| `--redact-patterns` | built-in | Path to custom redaction patterns file |

**Behavior**:
- Accept `POST /loki/api/v1/push` (Loki push API format)
- Also accept raw newline-delimited JSON on `POST /logtap/raw`
- Parse Loki push payload -> extract log lines with labels
- If `--redact`: apply redaction patterns to message BEFORE writing (PII never hits disk)
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
  "labels_seen": ["api-service", "worker-service", "gateway"],
  "redaction": {"enabled": true, "patterns": ["credit_card", "email", "jwt", "custom:ssn"]}
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

### WO-02e: Triage (background pre-scan)

**Goal**: `logtap triage` scans a capture directory for anomalies and produces a summary report before manual investigation.

**CLI interface**:
```
logtap triage ./capture --out ./triage
logtap triage ./capture --out ./triage --jobs 8
```

**Use case**: somebody sends you a 500GB capture dump with no context. You don't know what's in it, what went wrong, or where to look. Triage runs in background and produces a "here's what looks wrong" report in 30 seconds to 5 minutes depending on size.

**Output directory** (`./triage/`):
| File | Content |
|------|---------|
| `summary.md` | Human-readable triage report |
| `timeline.csv` | `minute,total_lines,error_lines` histogram |
| `top_errors.txt` | Top 50 normalized error signatures with counts |
| `top_talkers.txt` | Lines per label value (service/pod/namespace), sorted by volume |
| `windows.json` | Recommended time windows: peak error, incident start, steady state |

**Two-pass approach**:

Pass 1 — streaming signal extraction (parallel, one pass over data):
1. **Time histogram** — lines/min and errors/min, find spikes and cliff edges
2. **Top error signatures** — normalize messages (strip UUIDs, IPs, durations, hex), count patterns, output top 50
3. **Top talkers** — volume per label value (app, namespace, pod), plus which produce most errors
4. **First occurrences** — first time each error signature appears (locate incident start)
5. **Severe signals** — OOMKilled, CrashLoopBackOff, panic, segfault, x509 errors, connection refused, context deadline exceeded

Pass 2 — focus windows (derived from Pass 1):
1. Identify "peak error window" — highest error density
2. Identify "incident start window" — where new error signatures cluster
3. Identify "steady state window" — baseline for comparison
4. Write `windows.json` with recommended `--from`/`--to` ranges for `logtap open` or `logtap slice`

**Flags**:
| Flag | Default | Description |
|------|---------|-------------|
| `--out` | `./triage` | Output directory for triage artifacts |
| `--jobs` | `runtime.NumCPU()` | Parallel decompression/scan workers |
| `--window` | `1m` | Histogram bucket width |
| `--top` | `50` | Number of top error signatures |

**Behavior**:
- Reads JSONL+zstd files in parallel (one goroutine per file)
- Normalizes log messages for signature extraction (replace UUIDs, IPs, timestamps, hex with `<UUID>`, `<IP>`, `<TS>`, `<HEX>`)
- Uses `index.jsonl` to skip files outside time range if `--from`/`--to` specified
- Progress bar: `Triage: 14,832,901 / 14,832,901 lines (scanning errors.jsonl.zst)`
- Produces `summary.md` that a human can read in 30 seconds

**summary.md example**:
```
# Triage: ./capture

Period:  2024-01-15 10:32 — 12:45 (2h 13m)
Size:    8.4 GB (23 files, 14.8M lines)

## Incident Signal
Peak error rate: 10:44 — 10:52 (3,400 errors/min vs 12 baseline)
First new error:  10:43:17 "connection refused to payments-service:8080"

## Top Errors (of 23,401 total)
  1. connection refused to payments-service:8080     8,412  (35.9%)
  2. context deadline exceeded                       4,201  (17.9%)
  3. upstream connect error or disconnect/reset      3,891  (16.6%)
  4. 503 Service Unavailable                         2,102  (8.9%)

## Top Talkers
  api-service      8.4M lines  (56.7%)  ← 78% of errors
  worker-service   4.1M lines  (27.7%)
  gateway          2.3M lines  (15.6%)

## Recommended Slices
  logtap slice ./capture --from 10:43 --to 10:55 --out ./incident
  logtap slice ./capture --label app=api-service --grep "refused|timeout" --out ./api-errors
```

**Files**:
- `cmd/logtap/triage.go` — triage subcommand
- `internal/archive/triage.go` — parallel scanner, signature normalizer, histogram builder
- `internal/archive/normalize.go` — message normalization (strip UUIDs, IPs, etc.)

**Verification**:
```bash
logtap triage ./capture --out /tmp/triage
cat /tmp/triage/summary.md
cat /tmp/triage/top_errors.txt | head -10
logtap slice ./capture --from $(jq -r '.peak_error.from' /tmp/triage/windows.json) \
  --to $(jq -r '.peak_error.to' /tmp/triage/windows.json) --out /tmp/incident
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
1. Pre-check: HTTP GET `--target` /metrics — warn if receiver unreachable, require `--force` to proceed
2. Resource check: verify namespace quota headroom and node capacity (see below)
3. Generate session ID (short random, e.g. `lt-a3f9`) stored in annotations for multi-user isolation
4. Patch target deployment/statefulset/daemonset spec:
   - Add annotation `logtap.dev/tapped: "<session-id>"`
   - Add annotation `logtap.dev/target: "<target-address>"`
   - Add sidecar container `logtap-forwarder-<session-id>` to pod template
   - Sidecar shares the pod's log volumes via `emptyDir` or reads from
     `/var/log/containers` (hostPath, same as logging agents)
5. Sidecar container:
   - Minimal image: `ghcr.io/ppiankov/logtap-forwarder:latest`
   - Reads container stdout/stderr via shared volume or Kubernetes API log endpoint
   - Forwards to receiver via Loki push API (`POST /loki/api/v1/push`)
   - Labels: namespace, pod, container, app (from pod labels)
   - No buffering beyond 1MB -- drop if receiver unreachable
   - Resource defaults: requests 16Mi/25m, limits 32Mi/50m
   - Override: `--sidecar-memory 64Mi` `--sidecar-cpu 100m`
6. Triggers rolling restart (new pod spec = new pods)
7. Show diff before applying (`--dry-run` is default for first run)

**Resource pre-checks** (step 2):
Injecting a sidecar adds resource requests to every pod. logtap checks before patching:

| Check | What | Action |
|-------|------|--------|
| ResourceQuota | Namespace has quota, sidecar * replicas exceeds remaining | Warn, show math, require `--force` |
| LimitRange | Namespace default limits < sidecar requests | Warn, suggest `--sidecar-memory`/`--sidecar-cpu` |
| Node capacity | Nodes near memory/CPU limit, sidecar may cause eviction | Warn |

Example warning:
```
! Namespace "default" has ResourceQuota: memory used 3.8Gi / 4.0Gi
  Adding logtap-forwarder (16Mi x 3 replicas = 48Mi) would exceed quota by 48Mi
  Options:
    - Ask cluster admin to increase quota
    - Use --sidecar-memory 8Mi to reduce footprint
    - Use --force to proceed anyway (pods may fail to schedule)
```

logtap never modifies quotas or limits — it warns and lets the user decide.
The defaults (16Mi/25m requests, 32Mi/50m limits) are deliberately small. For
high-throughput workloads (>10k logs/sec per pod) consider `--sidecar-memory 64Mi`.

**Multi-user safety**:
- Each `logtap tap` generates a unique session ID (e.g. `lt-a3f9`)
- Annotation stores session ID: `logtap.dev/tapped: "lt-a3f9"` (not just `"true"`)
- `logtap untap` only removes YOUR session's sidecar (matches by session ID)
- `logtap untap --all` removes all sessions (requires confirmation)
- `logtap status` shows all active sessions with their targets
- Two devs can tap the same deployment — each gets their own sidecar + target

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
- Default: removes only YOUR session's sidecar (matches by session ID from last `tap`)
- `--session lt-a3f9` to remove a specific session
- `--all` removes ALL sessions from matching workloads (requires confirmation)
- Find workloads by `logtap.dev/tapped` annotation matching session ID
- Remove `logtap-forwarder-<session-id>` container from pod spec
- Remove `logtap.dev/tapped` and `logtap.dev/target` annotations
- Remove any shared volumes added by tap
- Triggers rolling restart
- Show diff before applying

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
- All created resources labeled `app.kubernetes.io/managed-by: logtap` (for orphan detection)
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

### WO-05a: Forwarder container image

**Goal**: Build and publish the `logtap-forwarder` sidecar image to GHCR.

**Image**: `ghcr.io/ppiankov/logtap-forwarder:<version>`

**Requirements**:
- Scratch-based or distroless (no shell, no package manager)
- Single static binary: `cmd/logtap-forwarder/main.go`
- Multi-arch: linux/amd64, linux/arm64
- Image size target: < 10MB
- Tags: `latest`, semver (`v0.1.0`), git SHA

**Dockerfile**:
```dockerfile
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /logtap-forwarder ./cmd/logtap-forwarder

FROM scratch
COPY --from=build /logtap-forwarder /logtap-forwarder
ENTRYPOINT ["/logtap-forwarder"]
```

**CI**: Update `.github/workflows/release.yml` to build and push container image on tag.

**Files**:
- `Dockerfile.forwarder` -- multi-stage build for sidecar image
- `.github/workflows/release.yml` -- add container image build + push step

**Verification**:
```bash
docker build -f Dockerfile.forwarder -t logtap-forwarder:test .
docker run --rm logtap-forwarder:test --help
docker images logtap-forwarder:test --format '{{.Size}}'
# should be < 10MB
```

---

### WO-06: Check command

**Goal**: `logtap check` validates cluster readiness AND detects leftovers from previous sessions.

Two modes: pre-tap readiness check and post-session orphan detection.

**CLI interface**:
```
logtap check
```

**Output (clean cluster)**:
```
logtap check

Cluster:       aks-dev-westeurope (v1.29.2)
Namespace:     default
RBAC:          ok (can patch deployments, create pods)
Quota:         ok (memory 1.2Gi / 4.0Gi available)

Leftovers:     none

Candidate workloads:
  deployment/api-gateway       3 replicas   ready     +48Mi for sidecar
  deployment/payments          2 replicas   ready     +32Mi for sidecar
  deployment/worker            5 replicas   ready     +80Mi for sidecar
  statefulset/postgres         1 replica    ready     +16Mi for sidecar

Ready to tap. Run: logtap tap --deployment api-gateway --target logtap.logtap:9000
```

**Output (leftovers found)**:
```
logtap check

Cluster:       aks-dev-westeurope (v1.29.2)
Namespace:     default
RBAC:          ok (can patch deployments, create pods)

Leftovers:
  ! deployment/api-gateway      has logtap-forwarder sidecar (receiver unreachable)
  ! deployment/payments          has logtap-forwarder sidecar (receiver unreachable)
  ! pod/logtap-receiver-7f8b9    orphaned tunnel pod in namespace logtap
  ! service/logtap               orphaned tunnel service in namespace logtap

  Run: logtap untap --all          to remove sidecars
  Run: kubectl delete ns logtap    to remove tunnel artifacts

Candidate workloads:
  deployment/worker            5 replicas   ready
  statefulset/postgres         1 replica    ready
```

**Checks**:
- Cluster reachable (kubectl context valid)
- RBAC permissions: can get/patch deployments, can create pods (for tunnel)
- **Resource quotas**: show namespace quota headroom, calculate sidecar cost per workload (replicas * 16Mi)
- **Orphaned sidecars**: workloads with `logtap.dev/tapped` annotation or `logtap-forwarder` container
  - For each: test if the sidecar's target receiver is reachable
  - If unreachable: flag as orphan, suggest `logtap untap --all`
- **Orphaned tunnel resources**: pods/services in `logtap` namespace left behind by `--in-cluster` or `--tunnel`
  - Detect by label `app.kubernetes.io/managed-by: logtap`
  - Suggest namespace deletion
- **Stale annotations**: resources with `logtap.dev/tapped` but no sidecar container (partial cleanup)
- List candidate workloads (not already tapped)
- Warn if prod-labeled namespace without `--allow-prod`

**Files**:
- `cmd/logtap/check.go` -- check subcommand
- `internal/k8s/check.go` -- permission checks, workload discovery
- `internal/k8s/orphan.go` -- orphan detection (sidecars, tunnel pods, stale annotations)

**Verification**:
```bash
# Clean cluster
logtap check
# should show "Leftovers: none"

# After tapping then killing receiver without untapping
logtap tap --deployment api-gateway --target logtap.logtap:9000
# (kill receiver, don't untap)
logtap check
# should show api-gateway as orphaned sidecar with "receiver unreachable"

# After cleanup
logtap untap --all
logtap check
# should show "Leftovers: none" again
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

### WO-09: Security guardrails and PII redaction

**Goal**: Prevent PII/PCI leaks structurally — redact on write, not after the fact.

**The problem**: logs contain PII (emails, credit cards, tokens, SSNs). If logtap
captures them as-is and someone sends the capture to a colleague, PII leaks. The
redaction must happen BEFORE writing to disk — once it's in a JSONL file, it's too late.

**PII redaction** (`--redact` flag on `logtap recv`):

Built-in patterns (always available):
| Pattern | Matches | Replacement |
|---------|---------|-------------|
| `credit_card` | 13-19 digit card numbers (Luhn-validated) | `[REDACTED:cc]` |
| `email` | RFC 5322 email addresses | `[REDACTED:email]` |
| `jwt` | `eyJ...` base64 JWT tokens | `[REDACTED:jwt]` |
| `bearer` | `Bearer ...` / `Authorization: ...` headers | `[REDACTED:bearer]` |
| `ip_v4` | IPv4 addresses | `[REDACTED:ip]` |
| `ssn` | US Social Security Numbers (XXX-XX-XXXX) | `[REDACTED:ssn]` |
| `phone` | Phone numbers (international formats) | `[REDACTED:phone]` |

Custom patterns (`--redact-patterns patterns.yaml`):
```yaml
patterns:
  - name: internal_id
    regex: 'CUST-[A-Z0-9]{8}'
    replacement: '[REDACTED:internal_id]'
  - name: api_key
    regex: 'sk_live_[a-zA-Z0-9]{24}'
    replacement: '[REDACTED:api_key]'
```

**Behavior**:
- `--redact` enables all built-in patterns
- `--redact=credit_card,email` enables only specified patterns
- `--redact-patterns file.yaml` adds custom patterns
- Redaction happens in the writer pipeline BEFORE bytes hit disk
- `metadata.json` records which patterns were active (so recipient knows data is redacted)
- TUI shows redaction status: `Redact: on (6 patterns)` or `Redact: OFF` (yellow warning)
- `logtap_redactions_total` metric with `pattern` label

**TUI warning** (when `--redact` is NOT set):
```
! PII redaction is OFF — captured logs may contain sensitive data
  Use --redact to enable. See: logtap recv --help
```

**Prod namespace protection**:
- Default: `logtap tap` refuses to tap prod-labeled namespaces
- Labels checked: `env=prod`, `environment=production`, `logtap.dev/prod=true`
- Flag `--allow-prod` overrides (with permanent warning banner in TUI)
- If `--allow-prod` without `--redact`: extra warning — "tapping prod without redaction"

**TLS support**:
- `--tls-cert` and `--tls-key` for receiver
- If omitted: plaintext (fine for local/tunnel, warn for direct IP mode)

**Audit log**:
- Written to `<capture-dir>/audit.jsonl`
- Records: connection source IP, bytes received, duration, session ID
- Not redacted (contains only connection metadata, no log content)

**Files**:
- `internal/recv/redact.go` -- redaction engine, pattern registry, pipeline stage
- `internal/recv/redact_test.go` -- deterministic tests for each built-in pattern
- `internal/recv/security.go` -- prod namespace check, TLS setup
- `internal/recv/audit.go` -- audit log writer

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

## Session Lifecycle

```
logtap check                                    # 1. verify cluster + no leftovers
logtap recv --tunnel --redact                   # 2. start receiver with PII redaction
logtap tap --deployment api-gateway \
  --target logtap.logtap:9000                   # 3. inject sidecar (session lt-a3f9)
# ... watch TUI, investigate ...
logtap untap --deployment api-gateway           # 4. remove YOUR sidecar
# Ctrl+C receiver                               # 5. stop receiver, capture dir ready
logtap inspect ./capture                        # 6. see what you got
logtap check                                    # 7. verify cluster is clean
```

**Important**: always `untap` BEFORE stopping the receiver. If you kill the receiver
first, the sidecars keep running and dropping logs until you clean up. `logtap check`
will detect these as orphans.

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

---

## Post-WO: Coverage Gaps

### WO-10: Push recv and rotate coverage to 85% ✅

**Goal:** Close coverage gaps — recv at 82.9%, rotate at 79.0%. Both below 85% target.

**Results:**
- recv: 82.9% → 90.2% (metadata_test.go, writer_test.go, coverage_test.go)
- rotate: 79.0% → 86.6% (coverage_test.go)

---

### WO-11: First Release (v0.1.0)

**Goal:** Cut the first tagged release. The release workflow (`.github/workflows/release.yml`) and Dockerfile already exist. This WO is about preparing the content and cutting the tag.

**Steps:**
1. Update `CHANGELOG.md` — set `[0.1.0]` date, verify entries match implemented features
2. Verify `README.md` has: badges, one-line description, quick start, usage, architecture, known limitations, roadmap, license
3. Verify CI passes on main: `go test -race ./...` all green
4. Tag: `git tag v0.1.0 && git push origin v0.1.0`
5. Verify release workflow produces:
   - GitHub release with binaries (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64)
   - checksums.txt
   - `ghcr.io/ppiankov/logtap-forwarder:v0.1.0` container image (multi-arch)
6. Smoke test: download binary from release, run `logtap recv --help`

**Acceptance:**
- GitHub release page shows v0.1.0 with all platform binaries
- `docker pull ghcr.io/ppiankov/logtap-forwarder:v0.1.0` succeeds
- Downloaded binary runs on macOS and Linux

---

## Phase 4: Adoption Features (v0.2.x)

---

### WO-12: Capture Grep ✅

**Goal:** Cross-file regex search on a capture directory. Currently users must decompress and pipe through `rg` manually.

**Details:**
- `logtap grep <pattern> <capture/>` — search across all compressed JSONL files
- Uses index to skip files outside time range (if `--since`/`--until` given)
- Streams decompressed content through regex, prints matching lines with file + line number
- Supports `--label` filter (same as `slice`) to narrow by service
- Supports `--count` to show match counts per file instead of lines
- Output: JSONL lines that match, preserving original structure

**Files:**
- `cmd/logtap/grep.go` — Cobra command
- `internal/archive/grep.go` + `grep_test.go` — search engine

**Verification:** `make test` passes, grep finds known patterns across compressed files.

**Results:**
- archive coverage: 86.7% (up from 86.6%)
- 9 tests: basic, no-matches, count-mode, time-filter, label-filter, compressed, multi-file, empty, progress

---

### WO-13: Capture Merge ✅

**Goal:** Combine multiple capture directories into one. Common scenario: two load test runs that should be analyzed together.

**Details:**
- `logtap merge <capture1/> <capture2/> -o <output/>` — merge by timestamp
- Rebuilds unified index.jsonl from both sources
- Handles overlapping time ranges (interleave by timestamp)
- Handles label collisions (both captures may have same service names — that's fine, they merge)
- Copies compressed files, does NOT decompress+recompress

**Files:**
- `cmd/logtap/merge.go` — Cobra command
- `internal/archive/merge.go` + `merge_test.go` — merge engine

**Verification:** `make test` passes, merged capture opens correctly in `logtap open`.

**Results:**
- archive coverage: 86.2%
- 8 tests: basic, name-collision, compressed, overlapping-time, labels-merge, too-few-sources, progress, three-sources

---

### WO-14: Shell Completion ✅

**Goal:** Tab completion for bash, zsh, and fish.

**Steps:**
1. Add `logtap completion bash|zsh|fish` command (Cobra built-in)
2. Add install instructions to README (one-liner per shell)
3. Capture directory arguments should complete from filesystem
4. `--format` flags should complete with valid values

**Files:**
- `cmd/logtap/completion.go`
- `README.md` — add completion section

**Verification:** Tab completion works in bash and zsh.

**Results:**
- `logtap completion bash|zsh|fish` generates valid completion scripts
- Long help text includes install instructions for all 3 shells

---

### WO-15: Config File Support ✅

**Goal:** Persistent defaults via `~/.logtap/config.yaml` so operators don't repeat flags.

**Config schema:**
```yaml
recv:
  addr: ":3100"
  dir: "./captures"
  disk_cap: "5GB"
  redact: true
  redact_patterns: /path/to/patterns.yaml
tap:
  namespace: default
  cpu: "25m"
  memory: "16Mi"
defaults:
  timeout: 30s
  verbose: false
```

**Steps:**
1. Create `internal/config/` package — load from `~/.logtap/config.yaml` then CWD `.logtap.yaml`
2. CLI flags override config values
3. Env vars override config: `LOGTAP_RECV_ADDR`, `LOGTAP_RECV_DIR`, etc.
4. Precedence: flags > env > config file > defaults

**Files:**
- `internal/config/config.go` + `config_test.go`
- `cmd/logtap/root.go` — integrate config loading

**Verification:** `make test` passes, config file defaults apply when flags not set.

**Results:**
- config coverage: 100%
- 7 tests: load-from-file, missing-file, empty-load, env-overrides, verbose-env, all-env-vars, partial-config
- Integrated via `applyConfigDefaults` PreRunE on recv and tap commands

---

### WO-16: JSON Output for All Commands ✅

**Goal:** Machine-readable output for scripting. Currently `inspect` and `triage` have text-only output.

**Steps:**
1. Add `--output json` flag to `inspect`, `triage`, `status`, `check`
2. `inspect --output json` emits the stats struct as JSON
3. `triage --output json` emits findings array as NDJSON
4. `status --output json` emits tapped workloads as JSON
5. `check --output json` emits readiness report as JSON

**Files:**
- Update `cmd/logtap/{inspect,triage,status,check}.go`
- Add JSON formatters to `internal/archive/inspect.go`, `internal/archive/triage.go`
- Add JSON formatters to `internal/k8s/status.go`, `internal/k8s/check.go`

**Verification:** `make test` passes, JSON output is valid and parseable.

**Results:**
- inspect: already had --json (pre-existing)
- triage: added --json flag + WriteJSON method, --out no longer required when --json used
- status: added --json flag, outputs TappedStatus array
- check: added --json flag, outputs checkResult struct with cluster/rbac/quotas/orphans/candidates
- Added JSON tags to all k8s types: Workload, TappedStatus, PodStatus, ClusterInfo, RBACCheck, RBACResult, QuotaSummary, ResourceWarning, OrphanResult, OrphanedSidecar, StaleAnnotation, OrphanedReceiver
- Added JSON tags to triage types: TriageResult, TriageBucket, ErrorSignature, TalkerEntry

---

### WO-17: Integration Tests with Kind [DONE]

**Goal:** End-to-end tests against a real Kubernetes cluster using Kind (Kubernetes in Docker).

**Problem:**
The k8s package (2,300+ lines) is tested with mocks. Real cluster interactions — patch application, sidecar injection, orphan detection — are untested. `client.go` NewClient path has zero coverage.

**Result:**
- `internal/k8s/integration_test.go` — 13 subtests against real Kind cluster
- Tests: ClusterInfo, Namespace, DeployReceiver, DeleteReceiver, DeploymentPatch, DiscoverByName, DiscoverBySelector, DiscoverTapped, FindOrphans, RemovePatch, DiscoverTappedEmpty, ProdNamespace, QuotaCheck, NodeCapacity, RBAC, SidecarInjectRemove
- CI: `integration` job in ci.yml using `helm/kind-action@v1` with pre-pulled images
- Guard: `LOGTAP_INTEGRATION=1` env var (not build tag, per Go best practice)
- `make test-integration` target added
- `make test` unaffected (tests skip without LOGTAP_INTEGRATION)
- Things Kind can't test moved to WO-42

---

### WO-18: Performance Benchmarks ✅

**Goal:** Formal benchmarks for the receiver pipeline, archive reader, triage scanner.

**Benchmarks:**
- `BenchmarkRecvPush` — Loki push API handler throughput (entries/sec, bytes/sec)
- `BenchmarkWriter` — Buffered JSONL write throughput
- `BenchmarkRotator` — Rotation + zstd compression overhead
- `BenchmarkReader` — Decompression + timestamp-ordered read
- `BenchmarkTriage` — Parallel scan with error normalization
- `BenchmarkFilter` — Time/label/grep filter evaluation
- `BenchmarkRedact` — PII redaction throughput per pattern count

**Steps:**
1. Add `Benchmark*` functions to recv, archive, rotate test files
2. Add `make bench` target: `go test -bench=. -benchmem ./internal/...`
3. Use realistic data sizes (10k entries, 1MB files)
4. Document baseline numbers in `docs/benchmarks.md`

**Acceptance:**
- `make bench` runs all benchmarks
- Results include allocations (`-benchmem`)
- No flaky timing assertions

**Result:** 14 benchmarks across 3 packages. `make bench` target added. Baseline numbers (Apple M2 Max): FilterSkipFile 4ns/op 0 alloc, FilterMatchEntry 58ns/op 0 alloc, ReaderScan 10ms/10k entries, RotatorWrite 1.6μs/op, Writer 3.8ns/op, Redact 10-144μs depending on message size.

---

### WO-19: Multi-Workload Tap ✅

**Goal:** Tap multiple workloads in one command using label selectors.

**Problem:**
Currently `logtap tap` targets a single deployment. Load tests involve many services — tapping each one individually is tedious.

**Details:**
- `logtap tap --selector app.kubernetes.io/part-of=myapp` — tap all matching deployments
- `logtap tap --namespace loadtest --all` — tap everything in namespace
- Single session ID shared across all tapped workloads
- `logtap untap --session lt-XXXX` removes all sidecars for that session
- Progress output: shows each workload being tapped

**Steps:**
1. Extend `cmd/logtap/tap.go` with `--selector` and `--all` flags
2. Extend `internal/k8s/discover.go` to return multiple workloads from selector
3. Loop tap with shared session ID
4. Extend `cmd/logtap/untap.go` to support `--session` flag
5. Extend `internal/k8s/status.go` to group by session

**Acceptance:**
- `logtap tap --selector app=myapp` taps all matching deployments
- `logtap untap --session lt-XXXX` removes all sidecars from that session
- `make test && make lint` clean

**Result:** Most features already existed from Phase 2. Added `--all` flag to tap (requires `--force`, filters out already-tapped workloads). `--selector`, shared session ID, `untap --session`, and session display in status were already implemented.

---

### WO-20: Snapshot Export ✅

**Goal:** Package a capture directory into a single compressed file for transfer.

**Details:**
- `logtap snapshot <capture/> -o capture.tar.zst` — tar + zstd the entire directory
- `logtap snapshot --extract capture.tar.zst -o <dir/>` — reverse
- Preserves directory structure (metadata.json, index.jsonl, *.jsonl.zst, audit.jsonl)
- Single-file artifact for sharing between teams or uploading to storage

**Steps:**
1. `cmd/logtap/snapshot.go` — Cobra command
2. `internal/archive/snapshot.go` + `snapshot_test.go` — tar+zstd pack/unpack
3. Validate on extract: check metadata.json exists, index.jsonl parseable

**Acceptance:**
- Round-trip: snapshot → extract → `logtap open` works
- `make test` passes

**Result:** Pack/Unpack functions in `internal/archive/snapshot.go`. Path traversal protection on extract. Validates metadata.json + index.jsonl on unpack. 6 tests including round-trip with Reader.Scan verification. archive coverage 85.3%.

---

### WO-21: Fluent Bit Sidecar Variant ✅

**Goal:** Support Fluent Bit as an alternative sidecar forwarder for environments that already run Fluent Bit.

**Details:**
- `logtap tap --forwarder fluent-bit` — inject Fluent Bit sidecar instead of logtap-forwarder
- ConfigMap with Fluent Bit config: tail input → http output (Loki push API to receiver)
- Same receiver endpoint — Fluent Bit speaks Loki push API natively
- Same session management, same untap behavior

**Steps:**
1. Create `internal/sidecar/fluentbit.go` — Fluent Bit container spec + ConfigMap
2. Extend `cmd/logtap/tap.go` with `--forwarder` flag (default: `logtap`, alternative: `fluent-bit`)
3. Extend `internal/sidecar/remove.go` to handle Fluent Bit ConfigMap cleanup
4. Document Fluent Bit image version pinning

**Constraints:**
- Fluent Bit image must be specified explicitly (no default — user controls their image)
- ConfigMap is session-scoped (cleaned up on untap)

**Acceptance:**
- `logtap tap --forwarder fluent-bit` injects working Fluent Bit sidecar
- Logs arrive at receiver in same Loki push format
- `logtap untap` removes sidecar + ConfigMap
- `make test && make lint` clean

**Result:** `internal/sidecar/fluentbit.go` with config generation, container spec, ConfigMap create/delete. Extended PatchSpec/RemovePatchSpec with volume support. `--forwarder` flag on tap (requires --image for fluent-bit). Remove/RemoveAll clean up ConfigMaps and volumes. 5 unit tests. sidecar coverage 79.7%.

---

## Phase 5: Hardening & Distribution

Phase 5 closes operational gaps, improves distribution, and adds one high-value analysis feature. After Phase 5 the tool is ready for v0.2.0.

---

### WO-22: Health & Readiness Endpoints [DONE]

**Goal:** Add Kubernetes-native health probes so the in-cluster receiver works with pod readiness gates and load balancers.

**Problem:**
The receiver exposes `/metrics` but has no `/healthz` or `/readyz`. In-cluster deployments can't distinguish "starting up" from "ready to accept logs". Port-forward and sidecar targeting wait for pod Ready, but there's no application-level readiness signal.

**Details:**
- `GET /healthz` — returns 200 if server is listening (liveness)
- `GET /readyz` — returns 200 if server is listening AND writer is not in backpressure (readiness)
- Both return JSON: `{"status":"ok"}` or `{"status":"not_ready","reason":"..."}`
- Wire into in-cluster receiver pod spec as liveness/readiness probes

**Steps:**
1. Add `/healthz` and `/readyz` handlers to `internal/recv/server.go`
2. Expose writer backpressure state via `Writer.Healthy() bool`
3. Update `internal/k8s/deploy.go` to include probe specs on receiver pod
4. Add tests

**Acceptance:**
- `curl /healthz` returns 200 when server is running
- `curl /readyz` returns 503 when writer channel is full
- `make test && make lint` clean

---

### WO-23: Sidecar Lifecycle Hooks [DONE]

**Goal:** Add PreStop hooks to forwarder sidecars so in-flight logs drain before pod termination.

**Problem:**
When a workload scales down or redeploys, the forwarder sidecar is killed immediately. Logs buffered in the forwarder's channel are lost. A PreStop hook gives the forwarder time to flush.

**Details:**
- Add `preStop` lifecycle hook to sidecar container spec: `sleep 5` (allow drain)
- Add `terminationGracePeriodSeconds` annotation recommendation in dry-run output
- Add liveness probe to forwarder sidecar (`/healthz` on logtap-forwarder, HTTP check on Fluent Bit)
- Update `cmd/logtap-forwarder/main.go` to expose a `/healthz` endpoint

**Steps:**
1. Add HTTP health endpoint to `cmd/logtap-forwarder/main.go` (minimal goroutine, port 9091)
2. Update `internal/sidecar/spec.go` `BuildContainer` to include lifecycle hook + liveness probe
3. Update `internal/sidecar/fluentbit.go` to include lifecycle hook
4. Add/update tests

**Acceptance:**
- Sidecar container spec includes `preStop` hook
- Forwarder exposes `/healthz` on port 9091
- `make test && make lint` clean

---

### WO-24: Coverage Hardening [DONE]

**Goal:** Bring k8s and sidecar packages to 85%+ coverage. Close the gap from Phase 4.

**Problem:**
k8s is at 83.9% and sidecar at 79.7% — both below the 85% project target. Fluent Bit code paths and volume patch logic are untested.

**Steps:**
1. `internal/k8s/patch_test.go` — add tests for volume append in ApplyPatch, filterVolumes in RemovePatch
2. `internal/sidecar/inject_test.go` — add test for Fluent Bit injection path (Forwarder field)
3. `internal/sidecar/remove_test.go` — add test for Fluent Bit removal (VolumeNames, ConfigMap delete)
4. `internal/sidecar/fluentbit_test.go` — add ConfigMap create/delete tests (with fake clientset)

**Acceptance:**
- `go test -cover ./internal/k8s/` reports ≥85%
- `go test -cover ./internal/sidecar/` reports ≥85%
- `make test && make lint` clean

---

### WO-25: Capture Diff [DONE]

**Goal:** Compare two captures to surface behavioral differences between load test runs.

**Problem:**
Teams run load tests repeatedly. Currently they must manually inspect each capture. A diff tool shows what changed: new error patterns, throughput shifts, label distribution changes.

**Details:**
- `logtap diff <capture-a/> <capture-b/>` — compare two captures
- Output sections:
  - **Time range:** start/stop/duration for each
  - **Volume:** total lines, lines/sec for each
  - **Labels:** label keys present in A but not B, and vice versa
  - **Error patterns:** triage-style top errors unique to each capture
  - **Rate comparison:** log rate per minute (shows spikes/dips between runs)
- No ML — deterministic string comparison and counting
- `--json` flag for structured output

**Steps:**
1. `internal/archive/diff.go` — DiffResult struct, Diff function
2. `internal/archive/diff_test.go` — tests
3. `cmd/logtap/diff.go` — Cobra command
4. Register in `cmd/logtap/main.go`

**Acceptance:**
- `logtap diff capA/ capB/` shows meaningful comparison
- `--json` produces structured output
- `make test && make lint` clean

---

### WO-26: Receiver Container Image [DONE]

**Goal:** Publish a container image for the logtap receiver binary (not just the forwarder).

**Problem:**
`--in-cluster` requires `--image` but there's no published receiver image. Users must build their own. The forwarder has `ghcr.io/ppiankov/logtap-forwarder` but there's no `ghcr.io/ppiankov/logtap`.

**Details:**
- `Dockerfile.receiver` — multi-stage build, scratch base, logtap binary
- Extend `.github/workflows/release.yml` to build and push `ghcr.io/ppiankov/logtap:<version>`
- Multi-arch: linux/amd64 + linux/arm64
- Default `--in-cluster --image` can reference this image

**Steps:**
1. Create `Dockerfile.receiver` (multi-stage: Go builder → scratch)
2. Add `build-receiver-image` target to Makefile (local build)
3. Extend `release.yml` with receiver image build job
4. Update README with receiver image reference

**Acceptance:**
- `docker build -f Dockerfile.receiver .` produces working image
- `release.yml` pushes both forwarder and receiver images on tag
- Image runs `logtap recv --headless` correctly

---

### WO-27: Homebrew Tap [DONE]

**Goal:** Install logtap via `brew install ppiankov/tap/logtap`.

**Details:**
- Create `homebrew-tap` repository at `github.com/ppiankov/homebrew-tap`
- Formula downloads release binary for darwin-amd64/arm64
- `release.yml` auto-updates formula on new tag (using `gh` or manual template)
- Include shell completion installation in formula

**Steps:**
1. Create formula template (`logtap.rb`)
2. Add formula update step to `release.yml` (after binaries are uploaded)
3. Test: `brew install ppiankov/tap/logtap && logtap --version`

**Acceptance:**
- `brew install ppiankov/tap/logtap` installs working binary
- `logtap completion zsh` works after install
- Formula auto-updates on release

---

### WO-28: CI Enhancements [DONE]

**Goal:** Add coverage reporting and benchmark regression detection to CI.

**Problem:**
CI runs tests but doesn't track coverage trends or detect performance regressions. Coverage drops go unnoticed until manual review.

**Details:**
- Coverage: upload to Codecov (or store as artifact), fail if any package drops below 80%
- Benchmarks: run on PR, compare against main, comment if >10% regression
- Add `make test-integration` target (placeholder for Kind tests, skip in CI for now)

**Steps:**
1. Update `.github/workflows/ci.yml` to generate coverage profile and upload
2. Add benchmark comparison step (use `benchstat` or `gobenchdata`)
3. Add coverage threshold check (script or tool)
4. Add `test-integration` Makefile target (no-op until Kind is available)

**Acceptance:**
- PR checks show coverage percentages
- >10% benchmark regression fails or warns on PR
- `make test && make lint` clean

---

### WO-29: Makefile & Developer Experience [DONE]

**Goal:** Improve local developer workflow with missing Makefile targets and help.

**Details:**
- `make help` — auto-generated target descriptions
- `make install` — build + copy to `$GOPATH/bin`
- `make coverage` — generate HTML coverage report, open in browser
- `make all` — deps + fmt + lint + test + build
- Update `.PHONY` to include all targets

**Steps:**
1. Add targets to Makefile
2. Add `help` target using grep + awk on `##` comments
3. Verify all targets work

**Acceptance:**
- `make help` lists all targets with descriptions
- `make install && logtap --version` works
- `make coverage` opens HTML report

---

### WO-30: Troubleshooting Guide [DONE]

**Goal:** Document common failure modes and solutions for operators.

**Details:**
Add `docs/troubleshooting.md` covering:
- Receiver won't start (port in use, dir permissions)
- Sidecar not forwarding (image pull errors, network policy blocking)
- `logtap check` failures (RBAC missing, quota exceeded)
- Disk full / rotation not working
- Capture can't be opened (corrupt metadata, missing index)
- Port-forward drops (tunnel instability, pod restart)
- Redaction not working (pattern not matching, custom file format)

**Steps:**
1. Create `docs/troubleshooting.md`
2. Add link from README.md

**Acceptance:**
- Guide covers at least 7 scenarios
- Each scenario has: symptom, cause, solution
- README links to it

---

### WO-31: v0.2.0 Release [DONE]

**Goal:** Tag and publish v0.2.0 with Phase 4 + Phase 5 features.

**Depends on:** WO-22 through WO-30

**Steps:**
1. Update CHANGELOG.md with v0.2.0 section
2. Update README.md known limitations (remove items addressed in Phase 4/5)
3. Run full verification: `make all`
4. Tag `v0.2.0`, push tag
5. Verify release workflow completes (binaries, images, Homebrew formula)
6. Update Homebrew formula if not auto-updated

**Acceptance:**
- GitHub release page shows v0.2.0 with binaries and changelog
- `ghcr.io/ppiankov/logtap:v0.2.0` and `ghcr.io/ppiankov/logtap-forwarder:v0.2.0` exist
- `brew upgrade logtap` gets v0.2.0
- CI green on main

---

## Phase 6: Hardening & Operational Robustness (WO-32 through WO-41)

Focus: Input validation, timeout safety, error recovery, CLI test coverage, and operational reliability. Phase 6 closes the gap between "works on happy path" and "ready for production load tests."

---

### WO-32: Kubernetes Operation Timeouts [DONE]

**Goal:** Prevent indefinite hangs on unresponsive clusters.

**Problem:**
All k8s operations use `context.Background()` with no timeout. If the cluster API is slow or unreachable, `tap`, `untap`, `check`, and `status` hang indefinitely with no feedback.

**Details:**
- Add `--timeout` flag to `tap`, `untap`, `check`, `status` (default 30s)
- Wrap all k8s context calls with `context.WithTimeout`
- Respect config file `timeout` field
- On timeout, print clear error: "timed out after 30s waiting for cluster response"

**Steps:**
1. Add `timeout` field to `internal/config/config.go`
2. Add `--timeout` flag to tap, untap, check, status commands
3. Replace `context.Background()` with timeout context in all k8s call sites
4. Add test: mock client with delayed response, verify timeout fires

**Acceptance:**
- `logtap tap --timeout 1s` on unreachable cluster exits in ~1s
- `logtap check --timeout 5s` respects deadline
- `make test && make lint` clean

---

### WO-33: Input Validation Hardening [DONE]

**Goal:** Fail fast on invalid user input with clear error messages.

**Problem:**
Several flags accept invalid values that cause confusing errors downstream:
- `--label` accepts `key=value=extra` without error
- `--sidecar-memory` and `--sidecar-cpu` aren't validated until the k8s API rejects them
- `--from`/`--to` time filters accept nonsense like "not-a-time"
- `--forwarder` accepts any string, not just "logtap" or "fluent-bit"

**Details:**
- Validate `--label` format (exactly one `=`, non-empty key and value)
- Validate resource quantities (`--sidecar-memory`, `--sidecar-cpu`) using `resource.ParseQuantity()` at parse time
- Validate `--from`/`--to` parse correctly before starting scan
- Validate `--forwarder` is one of the known values
- Validate `--cap` byte size format
- All validation errors should be actionable: "invalid --label format 'foo': expected key=value"

**Steps:**
1. Add `validateLabel()`, `validateQuantity()`, `validateForwarder()` helpers in `cmd/logtap/validate.go`
2. Wire validators into `RunE` / `PreRunE` of relevant commands
3. Add test for each validator with valid/invalid inputs

**Acceptance:**
- `logtap tap --sidecar-memory invalid` fails immediately with clear message
- `logtap grep --label "badformat" pattern dir/` fails with helpful error
- `make test && make lint` clean

---

### WO-34: CLI Integration Tests [DONE]

**Goal:** Bring cmd/ packages above 0% coverage with flag parsing and validation tests.

**Problem:**
`cmd/logtap` and `cmd/logtap-forwarder` are at 0% coverage. Flag parsing, validation, and command wiring are completely untested. Regressions in CLI behavior go undetected.

**Details:**
- Test flag parsing for all subcommands (not execution — no k8s or receiver needed)
- Test `buildFilter()` with all flag combinations (from, to, label, grep)
- Test `parseSpeed()` edge cases
- Test `applyConfigDefaults()` with various config states
- Test command registration (all subcommands present on root)
- Use `cmd.Execute()` with `--help` to verify no panics

**Steps:**
1. Create `cmd/logtap/filter_test.go` — test buildFilter, parseSpeed
2. Create `cmd/logtap/validate_test.go` — test input validators (from WO-33)
3. Create `cmd/logtap/cmd_test.go` — test command tree registration
4. Target: cmd/logtap coverage ≥ 40%

**Acceptance:**
- `go test ./cmd/logtap/` passes
- Coverage for cmd/logtap ≥ 40%
- `make test && make lint` clean

---

### WO-35: Tap Progress and Auto-Rollback [DONE]

**Goal:** Show progress during multi-workload tap and rollback on partial failure.

**Problem:**
When tapping multiple workloads (`--all`), users see no output until all operations complete. If tap fails partway through (e.g., workload 7 of 10), the first 6 are left tapped with no cleanup.

**Details:**
- Print progress to stderr: `Tapping deployment/api-gw [3/10]...`
- On error, automatically untap all workloads that were successfully tapped
- Print rollback status: `Rolling back: untapping deployment/api-gw...`
- Add `--no-rollback` flag to disable auto-rollback (for debugging)
- Same behavior for `untap --all`

**Steps:**
1. Add progress callback to `runTap()` and `runUntap()` in tap.go / untap.go
2. Track successful operations; on error, iterate and undo
3. Add test: mock k8s that fails on Nth workload, verify rollback

**Acceptance:**
- `logtap tap --all -l app=web` prints per-workload progress
- Partial failure triggers rollback with clear output
- `--no-rollback` skips cleanup
- `make test && make lint` clean

---

### WO-36: Receiver Version Endpoint [DONE]

**Goal:** Enable forwarder-receiver compatibility checks.

**Problem:**
Forwarder and receiver images version independently. If the push API changes, the forwarder silently fails. No way to detect version mismatch early.

**Details:**
- Add `GET /api/version` endpoint to receiver returning `{"version":"0.2.0","api":1}`
- `api` field is an integer that increments on breaking push API changes
- Forwarder checks `/api/version` on startup; warns if api version doesn't match
- `logtap check` verifies receiver version is compatible
- `logtap tap` can optionally check receiver version before injecting sidecars

**Steps:**
1. Add `/api/version` handler in `internal/recv/server.go`
2. Add version check in `cmd/logtap-forwarder/main.go` startup
3. Add version check in `logtap check` and `logtap tap`
4. Add tests for version endpoint and compatibility check

**Acceptance:**
- `curl localhost:9000/api/version` returns JSON with version and api fields
- Forwarder logs warning on version mismatch
- `make test && make lint` clean

---

### WO-37: In-Cluster Receiver TTL and Cleanup [DONE]

**Goal:** Auto-delete in-cluster receiver pods to prevent resource leaks.

**Problem:**
`logtap recv --in-cluster` creates a pod and service that persist after the CLI exits (Ctrl+C kills port-forward but not the pod). Users must manually `kubectl delete` the pod. Over time, abandoned receivers accumulate.

**Details:**
- Add `--ttl` flag (default: 4h) — receiver pod auto-deletes after TTL via `activeDeadlineSeconds`
- Add `--cleanup-on-exit` flag (default: true) — delete pod/service when CLI exits
- Register cleanup in signal handler (SIGTERM, SIGINT)
- If cleanup fails, print kubectl command for manual cleanup
- `logtap status` shows receiver pod age and remaining TTL

**Steps:**
1. Add `activeDeadlineSeconds` to receiver pod spec based on `--ttl`
2. Add defer cleanup in `runRecvInCluster()` signal handler
3. Add fallback message with kubectl commands if cleanup fails
4. Test: create pod with TTL, verify `activeDeadlineSeconds` is set

**Acceptance:**
- `logtap recv --in-cluster --ttl 1h` sets 3600s deadline on pod
- Ctrl+C deletes pod and service
- `make test && make lint` clean

---

### WO-38: Error Path Tests [DONE]

**Goal:** Cover critical failure modes that are currently untested.

**Problem:**
Happy paths are tested. Error paths are not. Disk-full, permission-denied, corrupt-data, and network-error scenarios have no test coverage.

**Details:**
Tests to add:
- `TestRotator_DeleteFails` — can't delete oldest file during rotation
- `TestSnapshot_CorruptTar` — malformed tar archive during unpack
- `TestSnapshot_PathTraversal` — tar entry with `../../etc/passwd`
- `TestExport_WriteError` — writer returns error mid-export
- `TestGrep_InvalidRegex` — pattern that doesn't compile
- `TestDiff_OneSideEmpty` — one capture has data, other is empty
- `TestMerge_ConflictingMetadata` — overlapping time ranges
- `TestReader_CorruptIndex` — malformed JSON in index.jsonl
- `TestServer_OversizedRequest` — request exceeding MaxBytesReader

**Steps:**
1. Add error path tests to each package (archive, rotate, recv)
2. Use error-injecting wrappers where needed (io.Writer that fails after N bytes)
3. Target: no critical error path uncovered

**Acceptance:**
- All listed tests pass
- Archive coverage ≥ 87%
- `make test && make lint` clean

---

### WO-39: Operational Metrics Expansion [DONE]

**Goal:** Add missing Prometheus metrics for production observability.

**Problem:**
Current metrics cover basic throughput (lines, bytes, drops). Missing: write latency, rotation events, disk usage, request duration. Operators can't diagnose performance issues.

**Details:**
New metrics:
- `logtap_push_duration_seconds` — histogram of push API request handling time
- `logtap_rotation_total` — counter of file rotations (labels: reason=size|time)
- `logtap_rotation_errors_total` — counter of failed rotations
- `logtap_disk_usage_bytes` — gauge of current capture directory size
- `logtap_active_connections` — gauge of in-flight HTTP requests
- `logtap_writer_queue_length` — gauge of writer channel occupancy

**Steps:**
1. Add metrics in `internal/recv/server.go` (push duration, active connections)
2. Add metrics in `internal/rotate/rotator.go` (rotation count, errors, disk usage)
3. Add metrics in `internal/recv/writer.go` (queue length)
4. Add test: verify metric registration and increment

**Acceptance:**
- `curl localhost:9000/metrics` shows all new metrics
- Metrics update correctly during test load
- `make test && make lint` clean

---

### WO-40: Documentation: API Stability and Examples [DONE]

**Goal:** Document stability guarantees and provide copy-paste workflow examples.

**Problem:**
Users don't know which CLI flags, capture formats, or API endpoints are stable across versions. No example workflows for common scenarios.

**Details:**

`docs/api-stability.md`:
- Capture format: metadata.json, index.jsonl schemas are stable from v0.1.0
- Loki push API compatibility guaranteed
- CLI flags: all documented flags are stable; undocumented behavior may change
- Internal packages: no stability guarantee (internal/)

`docs/examples/`:
- `load-test-workflow.sh` — full tap→recv→slice→triage→export pipeline
- `multi-namespace.sh` — tapping across namespaces
- `ci-integration.sh` — capture comparison in CI (diff two runs)
- `duckdb-analysis.sql` — export to parquet, query with DuckDB

**Steps:**
1. Create `docs/api-stability.md`
2. Create `docs/examples/` with 4 example scripts
3. Link from README.md

**Acceptance:**
- All example scripts are syntactically valid (shellcheck clean)
- README links to stability doc and examples
- No code changes needed

---

### WO-41: v0.3.0 Release [DONE]

**Goal:** Tag and publish v0.3.0 with Phase 6 hardening features.

**Depends on:** WO-32 through WO-40

**Steps:**
1. Update CHANGELOG.md with v0.3.0 section
2. Update README.md (remove addressed limitations, add new features)
3. Run `make all` for full verification
4. Tag `v0.3.0`, push tag
5. Verify release workflow (binaries, images, Homebrew formula update)

**Acceptance:**
- GitHub release page shows v0.3.0 with binaries and changelog
- Container images tagged v0.3.0
- CI green on main
- All internal packages ≥ 85% coverage

---

## Phase 7: Advanced Integration Testing

---

### WO-42: End-to-End Log Flow Testing [DONE]

**Goal:** Test the full forwarder→receiver log flow in a Kind cluster.

**Problem:**
WO-17 tests k8s API interactions (patch, discover, deploy) but not actual log delivery. The forwarder image must be built, loaded into Kind, and verified to send logs that the receiver captures.

**Result:**
- `internal/k8s/e2e_test.go` — 2 subtests: LogFlow (full pipeline), RBACRestriction
- LogFlow: deploy real receiver → create log-generator → inject real forwarder sidecar → verify `logtap_logs_received_total > 0` via API proxy
- RBACRestriction: restricted SA denied patch/create via SubjectAccessReview
- CI: `e2e` job builds Docker images, loads into Kind, runs TestE2E
- `make test-e2e` target added
- Deferred to future: network policy testing (needs Calico), OOMKill enforcement, port-forward stability

---

## Phase 8: Polish & Ecosystem (v0.5.x)

Focus: CLI test coverage, cloud storage integration, operational polish, and v1.0 readiness.

---

### WO-43: CLI Test Coverage to 50% [DONE]

**Goal:** Bring `cmd/logtap` from 33% to 50%+ coverage.

**Problem:**
Most subcommands have zero test coverage. Flag parsing and validation are covered (WO-34) but `runTap`, `runUntap`, `runRecv`, `runCheck`, `runStatus`, and helper functions like `doubleResource`, `checkReceiver`, `rollbackTap` are untested.

**Details:**
- Test `doubleResource()` with various inputs (Mi, m, Gi, plain numbers, empty)
- Test `checkReceiver()` with httptest server (reachable/unreachable)
- Test `rollbackTap()` with mock k8s client
- Test `runTap()` validation paths (mode counting, all+force, forwarder validation)
- Test `runUntap()` validation paths
- Test `runCheck()` and `runStatus()` output formatting
- Use interface-based mocking where k8s client is needed

**Steps:**
1. Create `cmd/logtap/tap_test.go` — test doubleResource, validation paths
2. Create `cmd/logtap/untap_test.go` — test validation paths
3. Extend `cmd/logtap/cmd_test.go` — test more subcommand wiring
4. Target: cmd/logtap coverage >= 50%

**Acceptance:**
- `go test -cover ./cmd/logtap/` reports >= 50%
- `make test && make lint` clean

**Result:** cmd/logtap 33% → 50%. Created tap_test.go (doubleResource, checkReceiver, runTap validation), untap_test.go (runUntap validation), archive_cmd_test.go (runInspect/Diff/Grep/Slice/Export/Merge/Snapshot/Triage), k8s_error_test.go (runStatus/runCheck no-kubeconfig), open_test.go (runOpen invalid speed), recv_test.go (runHeadless/runRecv), status_test.go (fetchReceiverMetrics mock). Executed via Codex/runforge.

---

### WO-44: Forwarder Test Coverage [DONE]

**Goal:** Add tests for `cmd/logtap-forwarder` (currently 0%).

**Problem:**
The forwarder binary has zero test coverage. The main function, health endpoint, and signal handling are completely untested.

**Details:**
- Test health endpoint (`/healthz` on port 9091) with httptest
- Test graceful shutdown signal handling
- Test argument parsing and validation
- Test the forwarder startup sequence (without requiring a real k8s cluster)
- Use httptest for the receiver target mock

**Steps:**
1. Create `cmd/logtap-forwarder/main_test.go`
2. Extract testable functions from main.go if needed
3. Target: >= 40% coverage

**Acceptance:**
- `go test -cover ./cmd/logtap-forwarder/` reports >= 40%
- `make test && make lint` clean

**Result:** cmd/logtap-forwarder 0% → 65.3%. Refactored main.go: extracted run(), Config, Dependencies for testability. Added NewPusherWithClient to forward/push.go. Created main_test.go with 10 tests (health, config validation, env loading, run pipeline, reader errors, buffer exceeded). Executed via Codex/runforge.

---

### WO-45: Cloud Capture Upload (S3/GCS)

**Goal:** Upload capture directories to cloud object storage for team sharing.

**Problem:**
Currently captures must be transferred via `tar | ssh`, `rsync`, or `logtap snapshot`. Teams using cloud infrastructure want to upload directly to S3/GCS and share a URL.

**CLI interface:**
```
logtap upload ./capture --to s3://bucket/prefix/
logtap upload ./capture --to gs://bucket/prefix/
logtap download s3://bucket/prefix/capture-2024-01-15/ --out ./capture
```

**Details:**
- Upload preserves capture directory structure (metadata.json, index.jsonl, *.jsonl.zst)
- No re-compression — files are already zstd compressed
- Progress output: `Uploading: 14/23 files (4.2 GB / 8.4 GB)`
- `--concurrency` flag for parallel uploads (default: 4)
- Uses standard AWS/GCP credential chains (env vars, profiles, instance metadata)
- Download reconstructs local capture directory from cloud prefix

**Dependencies:** `github.com/aws/aws-sdk-go-v2`, `cloud.google.com/go/storage`

**Files:**
- `cmd/logtap/upload.go` — upload subcommand
- `cmd/logtap/download.go` — download subcommand
- `internal/cloud/s3.go` — S3 upload/download
- `internal/cloud/gcs.go` — GCS upload/download
- `internal/cloud/cloud.go` — common interface

**Acceptance:**
- `logtap upload ./capture --to s3://test-bucket/` uploads all files
- `logtap download s3://test-bucket/capture/ --out ./local` restores capture
- `logtap open ./local` works after download
- `make test && make lint` clean

---

### WO-46: Webhook Notifications

**Goal:** Send webhook notifications on capture lifecycle events.

**Problem:**
Long-running captures have no external notification mechanism. Teams want Slack/PagerDuty/email alerts when a capture starts, hits disk limits, or completes.

**CLI interface:**
```
logtap recv --webhook https://hooks.slack.com/services/... --webhook-events start,stop,disk-warning
```

**Details:**
- Events: `start` (receiver begins), `stop` (receiver stops), `disk-warning` (>80% of max-disk), `rotation` (file rotated), `error` (write error)
- Payload: JSON with event type, timestamp, capture dir, disk usage, line count
- Configurable via config file (`recv.webhooks` array)
- Fire-and-forget with 5s timeout — never block the receiver pipeline
- `--webhook-events` filter (default: `start,stop,disk-warning`)

**Files:**
- `internal/recv/webhook.go` + `webhook_test.go` — webhook dispatcher
- Update `internal/recv/server.go` — wire webhook events
- Update `cmd/logtap/recv.go` — add flags

**Acceptance:**
- Webhook fires on start/stop/disk-warning events
- Webhook timeout doesn't block receiver
- `make test && make lint` clean

---

### WO-47: Capture Retention Policy [DONE]

**Goal:** Auto-delete old capture directories based on age or total size.

**Problem:**
Repeated load test runs accumulate capture directories. No built-in way to clean up old captures. Operators must manually delete or write cron scripts.

**CLI interface:**
```
logtap gc ./captures/ --max-age 7d
logtap gc ./captures/ --max-total 100GB
logtap gc ./captures/ --max-age 7d --max-total 100GB --dry-run
```

**Details:**
- Reads `metadata.json` from each subdirectory to determine capture age
- Deletes oldest captures first when `--max-total` exceeded
- `--dry-run` shows what would be deleted without acting
- `--json` flag for scripted cleanup
- Safe: refuses to delete if directory doesn't look like a capture (no metadata.json)

**Files:**
- `cmd/logtap/gc.go` — gc subcommand
- `internal/archive/gc.go` + `gc_test.go` — retention logic

**Acceptance:**
- `logtap gc ./captures/ --max-age 7d --dry-run` lists expired captures
- Actual deletion removes entire capture directories
- Non-capture directories are never deleted
- `make test && make lint` clean

**Result:** gc subcommand with --max-age (supports "7d"/"24h"), --max-total, --dry-run, --json flags. GC() scans subdirs for metadata.json, marks by age/total, sorts oldest-first. 6 tests: MaxAge, MaxTotal, DryRun, SkipNonCapture, EmptyDir, BothFlags. Executed via Codex/runforge.

---

### WO-48: HTML Triage Report

**Goal:** Generate a self-contained HTML report from triage output.

**Problem:**
`logtap triage` produces text files. Teams want a shareable report they can open in a browser — with interactive charts for timeline, error distribution, and top talkers.

**CLI interface:**
```
logtap triage ./capture --out ./triage --html
```

**Details:**
- Generates `triage/report.html` — single self-contained HTML file (inline CSS/JS)
- Timeline chart: line chart of logs/min and errors/min (uses inline SVG, no external JS libs)
- Error table: sortable top errors with counts and percentages
- Top talkers: bar chart by label value
- Recommended slices: clickable `logtap slice` commands
- No external dependencies — works offline, can be attached to a Jira ticket

**Files:**
- `internal/archive/triage_html.go` + `triage_html_test.go` — HTML template + renderer
- Update `cmd/logtap/triage.go` — add `--html` flag

**Acceptance:**
- `report.html` opens in browser with interactive charts
- No external resource dependencies (fully self-contained)
- `make test && make lint` clean

---

### WO-49: Forwarder Reliability Improvements

**Goal:** Add reconnection, buffering, and backoff to the forwarder sidecar.

**Problem:**
If the receiver is temporarily unreachable, the forwarder drops all logs immediately. Brief network blips cause permanent data loss. The forwarder needs a small retry buffer.

**Details:**
- Add retry with exponential backoff (1s, 2s, 4s, 8s, max 30s)
- Add in-memory ring buffer (default 1MB, configurable via `LOGTAP_BUFFER_SIZE`)
- On buffer full: drop oldest entries (not newest — preserve most recent logs)
- Add `/metrics` endpoint to forwarder: `logtap_forwarder_retries_total`, `logtap_forwarder_buffer_usage_bytes`, `logtap_forwarder_drops_total`
- Add `--retry-max` flag (default: 10 retries before giving up on a batch)

**Files:**
- `internal/forward/push.go` — add retry logic and buffer
- `internal/forward/buffer.go` + `buffer_test.go` — ring buffer
- Update `cmd/logtap-forwarder/main.go` — wire retry config

**Acceptance:**
- Forwarder reconnects after receiver restart
- Buffer prevents data loss during brief outages
- `make test && make lint` clean

---

### WO-50: v1.0 Release Preparation

**Goal:** Final polish for v1.0 release.

**Depends on:** WO-43 through WO-49 (or subset agreed upon)

**Steps:**
1. Audit all `--help` text for clarity and consistency
2. Verify all commands have `--json` output where applicable
3. Run full benchmark suite, document baseline numbers
4. Update README: remove "alpha" / "experimental" language
5. Update CHANGELOG.md with v1.0 section
6. Review all `internal/` APIs for consistency (error wrapping, context propagation)
7. Final coverage audit: all packages >= 85%
8. Tag `v1.0.0`, push, verify release workflow

**Acceptance:**
- GitHub release page shows v1.0.0
- All packages >= 85% coverage
- No known critical bugs
- README reflects stable status
