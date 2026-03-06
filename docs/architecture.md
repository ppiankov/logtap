# Architecture

## System overview

```
                          Kubernetes Cluster
                         ┌─────────────────────────────┐
  logtap tap ──────────► │  workload + logtap-forwarder│
                         │  (sidecar reads pod logs)   │
                         └──────────┬──────────────────┘
                                    │ Loki push API
                                    ▼
  logtap recv ──► HTTP server ──► writer ──► rotator ──► capture/
                   │                                       ├── metadata.json
                   ├── redactor (PII)                      ├── index.jsonl
                   ├── audit logger                        ├── *.jsonl.zst
                   └── TUI (stats + log pane)              └── audit.jsonl

  logtap open <capture/>      replay with TUI
  logtap inspect <capture/>   index-only summary (instant)
  logtap slice <capture/>     filtered copy to new capture
  logtap export <capture/>    parquet / CSV / JSONL
  logtap triage <capture/>    parallel anomaly scan
```

## Capture directory

The capture directory **is** the portable artifact. No double-compression — files are already zstd-compressed by the rotator. Transfer with `tar cf`, `rsync`, or `scp`.

```
capture/
  metadata.json              # written on start, updated on exit
  index.jsonl                # label-to-file index, one line per rotated file
  audit.jsonl                # connection metadata audit trail
  2024-01-15T103201-000.jsonl.zst
  2024-01-15T103512-000.jsonl.zst
```

See [API Stability](api-stability.md) for schema guarantees.
