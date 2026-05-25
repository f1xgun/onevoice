#!/usr/bin/env bash
# check-legal-versions-parity.sh — Phase 22 policy-version drift guard.
#
# Asserts pkg/legalconfig/versions.go and services/frontend/lib/legal/versions.ts
# carry IDENTICAL version strings for tos / privacy / pdn. Drift between the two
# would mean users see one version in the ReConsentModal but the server validates
# another — a silent-failure class (409 version_mismatch loops, or worse,
# accepted-with-wrong-version audit rows). T-22F-09 / T-22D-08 mitigation.
#
# Wired into `make lint-all` so a PR bumping the Go const without the TS const
# (or vice versa) fails CI before merge.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_FILE="$REPO_ROOT/pkg/legalconfig/versions.go"
TS_FILE="$REPO_ROOT/services/frontend/lib/legal/versions.ts"

if [[ ! -f "$GO_FILE" ]]; then
  echo "FAIL: missing pkg/legalconfig/versions.go ($GO_FILE)" >&2
  exit 1
fi
if [[ ! -f "$TS_FILE" ]]; then
  echo "FAIL: missing services/frontend/lib/legal/versions.ts ($TS_FILE)" >&2
  exit 1
fi

# Extract the value from a const declaration.
# Go side: lines like `TOSVersion     = "v1.0"` (inside a const ( ... ) block).
# TS side: lines like `export const TOS_VERSION = 'v1.0';`
extract_go() {
  local name="$1"
  grep -E "^[[:space:]]*${name}[[:space:]]*=" "$GO_FILE" | head -1 | sed -E 's/.*=[[:space:]]*"([^"]+)".*/\1/'
}
extract_ts() {
  local name="$1"
  grep -E "^[[:space:]]*export[[:space:]]+const[[:space:]]+${name}[[:space:]]*=" "$TS_FILE" | head -1 | sed -E "s/.*=[[:space:]]*['\"]([^'\"]+)['\"].*/\1/"
}

declare -a MISMATCH=()
for pair in "TOSVersion:TOS_VERSION" "PrivacyVersion:PRIVACY_VERSION" "PDNVersion:PDN_VERSION"; do
  GO_NAME="${pair%%:*}"
  TS_NAME="${pair##*:}"
  GO_VAL="$(extract_go "$GO_NAME" || echo '')"
  TS_VAL="$(extract_ts "$TS_NAME" || echo '')"
  if [[ -z "$GO_VAL" ]]; then
    echo "FAIL: const $GO_NAME not found in pkg/legalconfig/versions.go" >&2
    exit 1
  fi
  if [[ -z "$TS_VAL" ]]; then
    echo "FAIL: const $TS_NAME not found in services/frontend/lib/legal/versions.ts" >&2
    exit 1
  fi
  if [[ "$GO_VAL" != "$TS_VAL" ]]; then
    MISMATCH+=("$GO_NAME($GO_VAL) != $TS_NAME($TS_VAL)")
  fi
done

if [[ ${#MISMATCH[@]} -gt 0 ]]; then
  echo "FAIL: legal version drift detected between Go and TS:" >&2
  for m in "${MISMATCH[@]}"; do
    echo "  $m" >&2
  done
  echo >&2
  echo "Fix by bumping both files together. See docs/runbook-launch-readiness.md §6 (Legal compliance)." >&2
  exit 1
fi

echo "OK: pkg/legalconfig and services/frontend/lib/legal versions match (tos/privacy/pdn)"
