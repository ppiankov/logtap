# API Stability

This document describes which parts of logtap are stable and which may change between versions.

## Stable (from v0.1.0)

### Capture format

The capture directory layout is stable:

- `metadata.json` — schema versioned via `"version": 1`
- `index.jsonl` — one JSON line per rotated file
- `*.jsonl.zst` — zstd-compressed newline-delimited JSON log entries
- `audit.jsonl` — connection metadata

Log entry schema:

```json
{"ts":"2024-01-15T10:32:01Z","labels":{"app":"api"},"msg":"hello world"}
```

New fields may be added in future versions. Consumers should ignore unknown fields.

### Loki push API

`POST /loki/api/v1/push` accepts the standard Loki JSON push format. This endpoint will remain compatible with Loki client libraries.

### Raw push API

`POST /logtap/raw` accepts newline-delimited JSON log entries. Same entry schema as the capture format.

### Health endpoints

- `GET /healthz` — liveness probe (200 when server is running)
- `GET /readyz` — readiness probe (200 when writer has capacity, 503 under backpressure)
- `GET /api/version` — returns `{"version":"...","api":1}`

The `api` integer increments on breaking push API changes.

### CLI flags

All flags documented in `--help` output are stable. Behavior of documented flags will not change in backwards-incompatible ways within a major version.

## Unstable

### Internal packages

All code under `internal/` has no stability guarantee. Do not import these packages from external Go code.

### Undocumented behavior

Behavior not covered by `--help` or this document may change between minor versions.

### Metrics names

Prometheus metric names (`logtap_*`) may be added, renamed, or removed between minor versions. Do not build alerting rules that depend on specific metric names without pinning to a logtap version.
