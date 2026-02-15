#!/usr/bin/env bash
# multi-namespace.sh — Tap workloads across multiple namespaces
#
# Prerequisites:
#   - logtap installed
#   - kubectl configured with access to all target namespaces
set -euo pipefail

TARGET="${TARGET:-localhost:3100}"
NAMESPACES="${NAMESPACES:-frontend backend payments}"

echo "=== Checking cluster ==="
logtap check

echo "=== Tapping namespaces: $NAMESPACES ==="
for ns in $NAMESPACES; do
  echo "--- Tapping namespace: $ns ---"
  logtap tap \
    --namespace "$ns" \
    --all \
    --force \
    --target "$TARGET"
done

echo "=== All namespaces tapped ==="
logtap status

echo "=== Press Enter to untap all ==="
read -r

for ns in $NAMESPACES; do
  echo "--- Untapping namespace: $ns ---"
  logtap untap --namespace "$ns" --all --force
done

echo "=== Verify clean ==="
logtap check
