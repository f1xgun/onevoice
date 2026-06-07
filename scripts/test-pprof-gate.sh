#!/bin/sh
set -e

SENTINEL="services/api/internal/handler/_pprof_gate_sentinel_test.go"

cleanup() { rm -f "$SENTINEL"; }
trap cleanup EXIT

mkdir -p "$(dirname "$SENTINEL")"
printf 'package handler\n\nimport _ "net/http/pprof"\n' > "$SENTINEL"

if make lint-no-pprof 2>/dev/null; then
    echo "FAIL: lint-no-pprof did not detect injected pprof import"
    exit 1
fi

cleanup

if ! make lint-no-pprof; then
    echo "FAIL: lint-no-pprof red on clean tree"
    exit 1
fi

echo "OK: pprof gate works in both directions"
