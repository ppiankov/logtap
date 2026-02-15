#!/usr/bin/env bash
# load-test-workflow.sh — Full tap -> recv -> triage -> export pipeline
#
# Prerequisites:
#   - logtap installed
#   - kubectl configured for target cluster
#   - Target workloads deployed
set -euo pipefail

NAMESPACE="${NAMESPACE:-default}"
CAPTURE_DIR="${CAPTURE_DIR:-./capture}"
TRIAGE_DIR="${TRIAGE_DIR:-./triage}"
DISK_CAP="${DISK_CAP:-10GB}"

echo "=== Step 1: Verify cluster readiness ==="
logtap check

echo "=== Step 2: Start receiver ==="
logtap recv \
  --listen :3100 \
  --dir "$CAPTURE_DIR" \
  --max-disk "$DISK_CAP" \
  --redact \
  --headless &
RECV_PID=$!
sleep 2

echo "=== Step 3: Tap workloads ==="
logtap tap \
  --namespace "$NAMESPACE" \
  --all \
  --force \
  --target "localhost:3100"

echo "=== Receiver running (PID $RECV_PID). Run your load test now. ==="
echo "=== Press Enter when done to stop capturing. ==="
read -r

echo "=== Step 4: Untap workloads ==="
logtap untap --namespace "$NAMESPACE" --all --force

echo "=== Step 5: Stop receiver ==="
kill "$RECV_PID" 2>/dev/null || true
wait "$RECV_PID" 2>/dev/null || true

echo "=== Step 6: Inspect capture ==="
logtap inspect "$CAPTURE_DIR"

echo "=== Step 7: Triage for anomalies ==="
logtap triage "$CAPTURE_DIR" --out "$TRIAGE_DIR"
echo "--- Triage summary ---"
cat "$TRIAGE_DIR/summary.md"

echo "=== Step 8: Export to parquet ==="
logtap export "$CAPTURE_DIR" --format parquet --out "${CAPTURE_DIR}.parquet"
echo "Parquet file: ${CAPTURE_DIR}.parquet"

echo "=== Done ==="
echo "Replay:  logtap open $CAPTURE_DIR"
echo "Query:   duckdb -c \"SELECT count(*) FROM '${CAPTURE_DIR}.parquet'\""
