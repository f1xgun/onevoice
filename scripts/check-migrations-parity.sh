#!/usr/bin/env bash
# check-migrations-parity.sh — dual-path migration sanity check.
#
# Enforces:
# 1. No duplicate version numbers within migrations/postgres/ or services/api/migrations/.
# 2. Every *.up.sql has a matching *.down.sql in the same directory.
# 3. Reports (warning, not failure) when the two paths have different file counts —
# legitimate divergence is allowed but flagged for review.
#
# Wired into `make lint-all` post-Phase-20 to prevent regression of the
# duplicate-000008 collision.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROD_DIR="$REPO_ROOT/migrations/postgres"
TEST_DIR="$REPO_ROOT/services/api/migrations"

fail=0

check_dir() {
  local dir="$1"
  local label="$2"
  local dups
  dups=$(ls "$dir"/*.up.sql 2>/dev/null | awk -F'/' '{print $NF}' | awk -F'_' '{print $1}' | sort | uniq -d || true)
  if [ -n "$dups" ]; then
    echo "FAIL [$label] duplicate version numbers found in $dir:"
    echo "$dups" | sed 's/^/  - /'
    fail=1
  fi

  # Pair check: every .up.sql needs a sibling .down.sql.
  for up in "$dir"/*.up.sql; do
    local down="${up%.up.sql}.down.sql"
    if [ ! -f "$down" ]; then
      echo "FAIL [$label] missing companion down migration: $down"
      fail=1
    fi
  done
}

check_dir "$PROD_DIR" "prod"
check_dir "$TEST_DIR" "test"

prod_count=$(ls "$PROD_DIR"/*.up.sql 2>/dev/null | wc -l | tr -d ' ')
test_count=$(ls "$TEST_DIR"/*.up.sql 2>/dev/null | wc -l | tr -d ' ')
if [ "$prod_count" != "$test_count" ]; then
  echo "WARN dual-path divergence: prod has $prod_count migrations, test has $test_count. Confirm intentional."
fi

if [ "$fail" -eq 0 ]; then
  echo "Migration parity OK (prod=$prod_count, test=$test_count)"
fi
exit "$fail"
