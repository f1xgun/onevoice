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

# T-22F-12 mitigation — RU/EN policy frontmatter parity.
# D-09 declares the Russian text the legal source-of-truth; the EN translation
# is "for convenience". When a policy is bumped, both *.ru.md AND *.en.md
# frontmatter must move together — otherwise users on /?locale=en see a stale
# `version`/`effective_from` while the server happily accepts the RU value.
# This catches the bump-one-forget-the-other class.
CONTENT_DIR="$REPO_ROOT/services/frontend/content/legal"
declare -a CONTENT_MISMATCH=()
extract_md_field() {
  # Read frontmatter field value. Frontmatter is the block between the first
  # pair of `---` lines at top of file. Avoids dragging in YAML deps.
  local file="$1"
  local field="$2"
  awk -v f="$field" '
    /^---$/ { fm++; next }
    fm == 1 && $0 ~ "^" f ":" {
      sub("^" f ":[[:space:]]*", "")
      gsub(/^["'"'"']|["'"'"']$/, "")
      print
      exit
    }
  ' "$file"
}
for slug in tos privacy consent terms; do
  RU="$CONTENT_DIR/${slug}.ru.md"
  EN="$CONTENT_DIR/${slug}.en.md"
  # Skip slugs that don't have both locale files (e.g., tos isn't a content
  # slug — TOS uses terms.{ru,en}.md). Existence-skip is intentional: the
  # contract is "if both files exist, they must agree".
  if [[ ! -f "$RU" || ! -f "$EN" ]]; then
    continue
  fi
  for field in version effective_from; do
    RU_VAL="$(extract_md_field "$RU" "$field")"
    EN_VAL="$(extract_md_field "$EN" "$field")"
    if [[ -z "$RU_VAL" ]]; then
      echo "FAIL: ${slug}.ru.md missing frontmatter field '${field}'" >&2
      exit 1
    fi
    if [[ -z "$EN_VAL" ]]; then
      echo "FAIL: ${slug}.en.md missing frontmatter field '${field}'" >&2
      exit 1
    fi
    if [[ "$RU_VAL" != "$EN_VAL" ]]; then
      CONTENT_MISMATCH+=("${slug}.{ru,en}.md ${field}: ru='$RU_VAL' en='$EN_VAL'")
    fi
  done
done

if [[ ${#CONTENT_MISMATCH[@]} -gt 0 ]]; then
  echo "FAIL: legal policy MD frontmatter drift between RU (source-of-truth) and EN:" >&2
  for m in "${CONTENT_MISMATCH[@]}"; do
    echo "  $m" >&2
  done
  echo >&2
  echo "D-09 mandates RU/EN move together. Translate the new Russian body and bump the EN frontmatter in the same commit." >&2
  exit 1
fi

echo "OK: pkg/legalconfig and services/frontend/lib/legal versions match (tos/privacy/pdn)"
echo "OK: RU/EN frontmatter (version + effective_from) matches across privacy/terms/consent"
