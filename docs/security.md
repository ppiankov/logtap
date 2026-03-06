# Security & Safety

logtap is designed to be safe for production-adjacent use during load testing and incident investigation.

## Data safety

- **PII redaction** — `--redact` strips emails, credit card numbers, JWTs, bearer tokens, IPs, SSNs, and phone numbers before bytes hit disk. Custom patterns supported via YAML
- **Audit trail** — every connection, push, and rotation event is logged to `audit.jsonl` inside the capture directory
- **Bounded resources** — `--max-disk` and `--max-file` enforce hard caps. When disk is full, oldest files are rotated out. The receiver never blocks the sender
- **No upstream impact** — sidecar injection is read-only. The forwarder reads existing pod logs; it does not modify application logging or intercept traffic
- **Clean removal** — `logtap untap` removes all injected sidecars. `logtap status` detects orphaned sidecars. `logtap check` validates cluster state

## Network safety

- **Localhost by default** — receiver binds to `127.0.0.1:3100`, not `0.0.0.0`
- **TLS support** — `--tls-cert` and `--tls-key` for encrypted transport
- **Webhook auth** — bearer tokens or HMAC-SHA256 signatures for webhook notifications
- **Service mesh aware** — auto-detects Linkerd/Istio and adds sidecar bypass annotations

## File safety

- **Restrictive permissions** — capture files are written with `0600`/`0700` permissions
- **Path traversal protection** — cloud download validates all object keys against directory escape
- **No secrets in captures** — redaction happens in the receive pipeline, before the writer

## Production guardrails

- **`--allow-prod` required** — tapping production namespaces requires an explicit flag
- **`--force` required** — namespace-wide tap (`--all`) requires explicit confirmation
- **Dry-run support** — `--dry-run` on `tap`, `untap`, and `deploy` shows changes without applying
- **Auto-rollback** — failed sidecar injection automatically rolls back the workload
