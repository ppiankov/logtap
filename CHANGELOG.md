# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.1.0] - 2026-02-15

### Added

- HTTP receiver with Loki push API (`POST /loki/api/v1/push`) and raw JSONL endpoint (`POST /logtap/raw`)
- File rotation with zstd compression and bounded disk usage (hard cap with oldest-file deletion)
- Live TUI with stats, top talkers, scrollable log pane, vim-style navigation, and regex search
- PII redaction engine with 7 built-in patterns (credit card with Luhn, email, JWT, bearer, IPv4, SSN, phone) and custom YAML patterns
- Capture directory as portable artifact with metadata.json, index.jsonl, and zstd-compressed JSONL files
- Replay command (`open`) with speed control, time/label/grep filters, and TUI playback
- Capture inspection (`inspect`) with label breakdown, timeline sparkline, and JSON output
- Filtered capture extraction (`slice`) with index-aware file skipping
- Export to parquet, CSV, and JSONL formats with filter support
- Triage command for parallel anomaly scanning with error signature normalization and incident window detection
- Sidecar injection (`tap`) with session IDs, resource quota pre-checks, and multi-user isolation
- Sidecar removal (`untap`) with session-scoped cleanup
- Cluster readiness validation (`check`) with RBAC checks, orphan detection, and quota analysis
- Tapped workload monitoring (`status`) with pod health and receiver stats
- In-cluster receiver deployment with port-forward tunnel
- Forwarder sidecar binary and multi-arch container image (`ghcr.io/ppiankov/logtap-forwarder`)
- Production namespace protection (`--allow-prod` flag with label-based detection)
- TLS support for receiver (`--tls-cert`, `--tls-key`)
- Audit logging (append-only JSONL with connection metadata)
- TUI redaction status display
- Prometheus metrics endpoint (`/metrics`)
- Headless mode for receiver (`--headless`)

### Testing

- Backpressure and stress tests for receiver pipeline (sustained throughput, concurrent connections, drop counter accuracy)
- Stress tests for file rotator (concurrent writes, tight disk cap, high rotation frequency)
- Coverage tests for all internal packages (archive 86.6%, forward 87.4%, k8s 85.0%, recv 90.2%, rotate 86.6%, sidecar 94.2%)
