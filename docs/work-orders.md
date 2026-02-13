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
- Delete oldest files when `--max-disk` exceeded
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
- Keyboard: `q` or Ctrl+C to quit, `f` to toggle follow, `/` to filter
- On exit: flush buffers, close files, offer export (see WO-02a)

**Dependencies**: `github.com/charmbracelet/bubbletea` + `lipgloss`

**Files**:
- `internal/recv/tui.go` -- bubbletea model, views
- `internal/recv/stats.go` -- atomic counters for live stats
- `internal/recv/logview.go` -- ring buffer of recent log lines for display

**Verification**: Visual -- run recv with a sender, confirm stats update and logs scroll.

---

### WO-02a: Export and replay

**Goal**: On exit, export capture to tar.gz. Second user opens archive with same TUI.

**Export (on recv exit)**:
```
Flushing buffers... done
Export capture to ./capture-2024-01-15T103201.tar.gz? [Y/n]
```

Creates tar.gz containing:
- All JSONL files (uncompressed inside archive for streaming replay)
- `metadata.json`: start time, end time, total lines, total bytes, labels seen

**Replay**:
```
logtap open ./capture-2024-01-15T103201.tar.gz
```

**Behavior**:
- Extracts to temp dir, reads JSONL files in timestamp order
- Renders same TUI as live mode (stats pane + log pane)
- Replays at original speed by default
- `--speed 10x` to fast-forward, `--speed 0` for instant load
- Keyboard: arrow keys to scrub, `space` to pause/resume
- Read-only -- no receiver, no disk writes

**Files**:
- `internal/archive/export.go` -- tar.gz creation with metadata
- `internal/archive/replay.go` -- timestamp-ordered JSONL reader
- `cmd/logtap/open.go` -- open subcommand

**Verification**:
```bash
# Capture some data
logtap recv --dir /tmp/capture --max-disk 100MB
# (send data, then Ctrl+C, answer Y to export)

# Replay on another machine
logtap open ./capture-2024-01-15T103201.tar.gz
logtap open --speed 10x ./capture-2024-01-15T103201.tar.gz
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
| `logtap open` | Open and replay a capture archive |
| `logtap check` | Validate cluster readiness |
| `logtap tap` | Inject sidecar into workloads |
| `logtap untap` | Remove sidecar from workloads |
| `logtap status` | Show tapped workloads and stats |

## Deliberate Non-Goals (v1)

- No metrics ingestion
- No trace ingestion
- No indexing or search engine
- No browser UI
- No CRDs or operators
- No long-term retention management
- No OpenTelemetry collector role
- No logging agent config patching (sidecar instead)
- No Fluent Bit support (v1.1)
