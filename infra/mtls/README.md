# OneVoice — internal mTLS

This directory holds the **dev / test** mTLS substrate for cluster-internal
communication between `services/orchestrator`, the four platform agents
(`agent-telegram`, `agent-vk`, `agent-yandex-business`, `agent-google-business`)
and the API's internal listener on `:8443`.

## What this is

The API exposes two HTTP listeners:

- **Public `:8080`** — terminated by nginx in production; plain HTTP inside the
  Docker network. Not covered here.
- **Internal `:8443`** — talked to by orchestrator + agents over cluster-internal
  HTTPS with **mTLS client-cert verification**. The cert material in this directory
  is what powers that TLS channel in dev/test.

Two helpers in `pkg/mtls/` (`LoadServerTLSConfig`, `LoadClientTLSConfig`) read
the certs via env vars (`ONEVOICE_MTLS_CA_PATH`, `ONEVOICE_MTLS_CERT_PATH`,
`ONEVOICE_MTLS_KEY_PATH`) and produce `*tls.Config` values wired into the
API's `internalSrv.TLSConfig` and tokenclient's `http.Transport.TLSClientConfig`.

## Dev vs Prod CA divergence

> The dev CA's private key (`ca.key`) is generated locally and is **never**
> trusted in production.

Production must:

1. Provision a **separate** root CA via the organization's secrets manager
   (HashiCorp Vault PKI, Yandex Cloud KMS, AWS PCM, etc.).
2. Issue per-service leaf certs from that prod CA — never reuse the dev CA.
3. Mount the prod CA + leaf material into each container via the secrets
   manager's volume driver (NOT the `infra/mtls/certs/` repo path).
4. Set `ONEVOICE_MTLS_*` env vars in the production compose / deployment manifest
   to point at the mounted secret paths.

The `infra/mtls/certs/.gitignore` blocks all key material from being committed;
`scripts/check-mtls-certs.sh` is wired into CI so the CI cert harness uses
ephemeral material that never overlaps with production.

A PR that mounts `infra/mtls/certs/` into a production compose file MUST be
rejected.

## Cert generation (dev / test)

One-time, idempotent:

```bash
# from the repo root
bash scripts/gen-mtls-certs.sh
# OR via the Makefile:
make mtls-certs
# OR from this directory:
cd infra/mtls/ca && make certs
```

This produces under `infra/mtls/certs/`:

```
ca.crt                          # root CA cert (PEM)
ca.key                          # root CA private key (PEM) — gitignored
api.crt / api.key               # server-side leaf for the API internal :8443 listener
orchestrator.crt / orchestrator.key
agent-telegram.crt / agent-telegram.key
agent-vk.crt / agent-vk.key
agent-yandex-business.crt / agent-yandex-business.key
agent-google-business.crt / agent-google-business.key
```

Each leaf cert is signed by the dev CA, valid for 365 days, and includes
`subjectAltName=DNS:<service>,DNS:localhost` so it works inside Docker
(where the hostname is the service name) and on a developer's localhost.

The script is **idempotent** — re-running it is a no-op when
`infra/mtls/certs/ca.crt` already exists. Use `make clean-certs` (or `rm`)
to force regeneration.

## Cert rotation runbook

In dev, the dev CA cert lives for 10 years; leaf certs live for 1 year. When
the 1-year window approaches:

1. `make clean-certs` — removes all leaf material; CA cert stays unless removed
   manually.
2. `make mtls-certs` — re-issues new leaf certs from the existing CA.
3. `docker compose restart api orchestrator agent-telegram agent-vk agent-yandex-business agent-google-business`
   — services reload the new cert at startup.

`scripts/check-mtls-certs.sh` flags leaf certs that are within 30 days of
expiry; CI rejects PRs that ship expiring material.

In production, follow your organization's CA rotation runbook. There is **no
CRL / OCSP** in v1.4 — see "Revocation strategy" below.

## Revocation strategy (v1.4 — accepted risk)

v1.4 does **not** implement CRL or OCSP. Compromise response is:

1. Generate a fresh CA in the secrets manager.
2. Issue new leaf certs.
3. Rolling-restart all services.

This is acceptable for cluster-internal traffic because the trust boundary
is the Docker network — a leaked leaf cert is only useful from inside that
network, which already implies host compromise. v1.5+ may add OCSP stapling.

## Troubleshooting

| Symptom | Likely cause | Fix |
|--------|--------------|-----|
| `tls: handshake failure` on API startup | `ONEVOICE_MTLS_*` env var unset or path wrong | Verify env in `docker compose config`; check the volume mount actually contains `ca.crt`/`<service>.crt`/`<service>.key`. |
| `x509: certificate signed by unknown authority` from tokenclient | Service cert signed by different CA than the one in `ONEVOICE_MTLS_CA_PATH` | Re-run `scripts/gen-mtls-certs.sh`; ensure every service mounts the SAME `ca.crt`. |
| `tls: handshake failure: missing client certificate` | API listener requires client cert; client (tokenclient) had no cert config | Confirm `ONEVOICE_MTLS_ENABLED=true` on the calling service; confirm cert/key env vars resolve to a readable file inside the container. |
| `tls: certificate is not valid for any names` | Cert's SAN doesn't include the hostname the client used | Regenerate with the right service name; the script's SAN line includes `DNS:<service>,DNS:localhost`. |
| Time-skew errors | Container clock drift > leaf cert's notBefore | Sync host clock; consider `ntpd` in the container if drift is recurring. |
