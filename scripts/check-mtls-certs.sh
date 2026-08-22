#!/usr/bin/env bash
# check-mtls-certs.sh — verify the dev/test mTLS material is well-formed.
#
# Asserts, for every leaf in infra/mtls/certs/:
#   - the cert is signed by infra/mtls/certs/ca.crt
#   - the cert is not within 30 days of expiry
#
# Exit code 1 with a diagnostic line per failure; exit 0 if everything passes.
# Wired into CI to reject PRs that ship near-expiry or mis-signed material.
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"
if [[ -z "$ROOT" ]]; then
    ROOT="$(cd "$(dirname "$0")/.." && pwd)"
fi
CERT_DIR="$ROOT/infra/mtls/certs"
CA="$CERT_DIR/ca.crt"
SERVICES=(api orchestrator agent-telegram agent-vk agent-yandex-business agent-google-business nats)
THRESHOLD_SECS=$((30 * 24 * 60 * 60))   # 30 days

failures=0

if [[ ! -f "$CA" ]]; then
    echo "ERROR: missing CA cert at $CA — run scripts/gen-mtls-certs.sh first" >&2
    exit 1
fi

# verify CA cert is parseable
if ! openssl x509 -in "$CA" -noout -subject >/dev/null 2>&1; then
    echo "ERROR: $CA is not a valid X.509 certificate" >&2
    exit 1
fi

for svc in "${SERVICES[@]}"; do
    leaf="$CERT_DIR/$svc.crt"
    if [[ ! -f "$leaf" ]]; then
        echo "FAIL  $svc: missing leaf cert at $leaf"
        failures=$((failures + 1))
        continue
    fi

    # Signed by the CA?
    if ! openssl verify -CAfile "$CA" "$leaf" >/dev/null 2>&1; then
        echo "FAIL  $svc: leaf is NOT signed by infra/mtls/certs/ca.crt"
        failures=$((failures + 1))
        continue
    fi

    # Within 30 days of expiry?
    if ! openssl x509 -in "$leaf" -noout -checkend "$THRESHOLD_SECS" >/dev/null 2>&1; then
        not_after=$(openssl x509 -in "$leaf" -noout -enddate | cut -d= -f2)
        echo "FAIL  $svc: leaf expires within 30 days (notAfter=$not_after)"
        failures=$((failures + 1))
        continue
    fi

    echo "OK    $svc"
done

if (( failures > 0 )); then
    echo "✗ check-mtls-certs: $failures failure(s)" >&2
    exit 1
fi
echo "✓ check-mtls-certs: all ${#SERVICES[@]} leaf certs verify against the CA"
