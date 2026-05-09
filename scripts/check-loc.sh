#!/usr/bin/env bash
set -euo pipefail
OFFENDERS=$(git ls-files '*.go' '*.ts' '*.tsx' \
  | grep -vE '(_test\.go|__tests__/|\.test\.tsx?|\.spec\.tsx?)$' \
  | grep -vE '/generated/' \
  | grep -vE '\.pb\.go$' \
  | xargs wc -l 2>/dev/null \
  | awk '$1>500 && $2!="total"{print $1"\t"$2}')
if [[ -n "$OFFENDERS" ]]; then
  echo "files exceeding 500 LOC:" >&2
  echo "$OFFENDERS" >&2
  exit 1
fi
