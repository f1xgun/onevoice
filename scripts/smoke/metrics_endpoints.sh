#!/usr/bin/env bash
# Smoke test: verify /metrics endpoints on all 6 services.
# Usage: bash scripts/smoke/metrics_endpoints.sh [HOST]
#   HOST defaults to "localhost" — override when running against docker hostnames.
# Exits non-zero if any endpoint fails to return HTTP 200 + a Prometheus
# exposition-format header line ("# HELP" or "# TYPE").

set -euo pipefail

HOST="${1:-localhost}"

declare -A TARGETS=(
  ["api"]="${HOST}:8080"
  ["orchestrator"]="${HOST}:8090"
  ["agent-telegram"]="${HOST}:8081"
  ["agent-vk"]="${HOST}:8082"
  ["agent-yandex-business"]="${HOST}:8083"
  ["agent-google-business"]="${HOST}:8084"
)

fail=0
for name in "${!TARGETS[@]}"; do
  url="http://${TARGETS[$name]}/metrics"
  body=$(curl -fsS -m 5 "$url" 2>/dev/null || true)
  if [[ -z "$body" ]]; then
    echo "FAIL  $name  $url  (no response)"
    fail=1
    continue
  fi
  if ! echo "$body" | grep -qE '^# (HELP|TYPE) '; then
    echo "FAIL  $name  $url  (response missing prometheus exposition header)"
    fail=1
    continue
  fi
  echo "OK    $name  $url"
done

if [[ $fail -ne 0 ]]; then
  echo ""
  echo "FAIL: one or more /metrics endpoints failed."
  exit 1
fi

echo ""
echo "PASS: all 6 /metrics endpoints responded with valid exposition format."
