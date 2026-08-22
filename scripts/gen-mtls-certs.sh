#!/usr/bin/env bash
# gen-mtls-certs.sh — generate dev/test mTLS material for OneVoice.
#
# Idempotent: a second invocation with an existing infra/mtls/certs/ca.crt
# is a no-op and exits 0. Use `make clean-certs` (or `rm -rf
# infra/mtls/certs/*`) to force regeneration.
#
# Output layout under infra/mtls/certs/:
#   ca.crt / ca.key                                     # root CA
#   <service>.crt / <service>.key  for each service in
#   {api, orchestrator, agent-telegram, agent-vk,
#    agent-yandex-business, agent-google-business, nats}
#
# The `nats` leaf is the NATS server's TLS cert (SAN DNS:nats). Every client
# already mounts this dir and trusts ca.crt, so it doubles as the NATS transport
# CA (NATS_CA_PATH=<mount>/ca.crt); NATS client auth is by nkey, not client cert.
#
# Each leaf cert includes  subjectAltName=DNS:<service>,DNS:localhost
# so it works inside Docker (service-name hostname) and on a developer's
# localhost.
#
# Production:  this script is for DEV ONLY. See infra/mtls/README.md.
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"
if [[ -z "$ROOT" ]]; then
    ROOT="$(cd "$(dirname "$0")/.." && pwd)"
fi
CERT_DIR="$ROOT/infra/mtls/certs"
CA_CNF="$ROOT/infra/mtls/ca/openssl-ca.cnf"
CA_DAYS=3650
LEAF_DAYS=365
SERVICES=(api orchestrator agent-telegram agent-vk agent-yandex-business agent-google-business nats)

mkdir -p "$CERT_DIR"

if [[ -f "$CERT_DIR/ca.crt" ]]; then
    echo "✓ infra/mtls/certs/ca.crt already exists — skipping (run 'make clean-certs' to force)"
    exit 0
fi

if ! command -v openssl >/dev/null 2>&1; then
    echo "ERROR: openssl is required but not installed" >&2
    exit 1
fi

echo "→ Generating dev root CA (10y validity)..."
openssl req -x509 -newkey rsa:4096 -nodes \
    -days "$CA_DAYS" \
    -keyout "$CERT_DIR/ca.key" \
    -out "$CERT_DIR/ca.crt" \
    -config "$CA_CNF" \
    -extensions v3_ca \
    2>/dev/null

chmod 600 "$CERT_DIR/ca.key"
chmod 644 "$CERT_DIR/ca.crt"

for svc in "${SERVICES[@]}"; do
    echo "→ Issuing leaf cert for $svc..."
    KEY="$CERT_DIR/$svc.key"
    CRT="$CERT_DIR/$svc.crt"
    CSR="$CERT_DIR/$svc.csr"
    EXT="$CERT_DIR/$svc.ext"

    openssl req -newkey rsa:2048 -nodes \
        -keyout "$KEY" \
        -out "$CSR" \
        -subj "/CN=$svc/O=OneVoice" \
        2>/dev/null

    # SAN: container DNS (service name) + localhost for dev host calls.
    cat > "$EXT" <<EOF
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
subjectAltName = DNS:$svc,DNS:localhost
EOF

    openssl x509 -req -in "$CSR" \
        -CA "$CERT_DIR/ca.crt" \
        -CAkey "$CERT_DIR/ca.key" \
        -CAcreateserial \
        -days "$LEAF_DAYS" \
        -extfile "$EXT" \
        -out "$CRT" \
        2>/dev/null

    chmod 600 "$KEY"
    chmod 644 "$CRT"
    rm -f "$CSR" "$EXT"
done

# Remove the CA serial file — fresh runs regenerate it.
rm -f "$CERT_DIR/ca.srl"

echo "✓ Generated certs for: ${SERVICES[*]}"
