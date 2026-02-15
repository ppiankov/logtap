#!/usr/bin/env bash
# ci-integration.sh — Compare two load test captures in CI
#
# Usage:
#   ci-integration.sh <baseline-capture/> <current-capture/>
#
# Returns exit code 1 if error rate increased significantly.
set -euo pipefail

BASELINE="${1:?Usage: ci-integration.sh <baseline/> <current/>}"
CURRENT="${2:?Usage: ci-integration.sh <baseline/> <current/>}"

echo "=== Inspecting baseline ==="
logtap inspect "$BASELINE"

echo ""
echo "=== Inspecting current ==="
logtap inspect "$CURRENT"

echo ""
echo "=== Diff ==="
logtap diff "$BASELINE" "$CURRENT"

echo ""
echo "=== Diff (JSON for parsing) ==="
DIFF_JSON=$(logtap diff "$BASELINE" "$CURRENT" --json)
echo "$DIFF_JSON" | jq .

# Extract error counts for comparison
BASELINE_ERRORS=$(echo "$DIFF_JSON" | jq '.a.error_lines // 0')
CURRENT_ERRORS=$(echo "$DIFF_JSON" | jq '.b.error_lines // 0')

echo ""
echo "Baseline errors: $BASELINE_ERRORS"
echo "Current errors:  $CURRENT_ERRORS"

# Fail if current run has 2x+ more errors than baseline
if [ "$BASELINE_ERRORS" -gt 0 ] && [ "$CURRENT_ERRORS" -gt $((BASELINE_ERRORS * 2)) ]; then
  echo "FAIL: Error count increased from $BASELINE_ERRORS to $CURRENT_ERRORS (>2x)"
  exit 1
fi

echo "PASS: Error rate within acceptable range"
