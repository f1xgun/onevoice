# Launch Readiness Runbook

**Audience:** Operator (you). Walks the operator-side gates required before a production deploy.

**Skip-blockers:** A failure in §6 (Legal compliance) is a HARD block — production deploy must not proceed. §3 (Database), §5 (Email), §6 (Legal compliance), and §7 (Operational hardening — Grafana) must all be GREEN before flipping the public DNS record.

## 1. Audience and scope

This document is the master pre-deploy gate for OneVoice. Every section corresponds to a subsystem with operator-facing dependencies that the codebase alone cannot enforce (DNS records, regulator filings, secret-management). Each section lists actionable checks; the linked sub-runbook explains how to fulfil each.

## 2. When to run

- Before EVERY staging-deploy (catches misconfigured `.env.staging` early).
- Before EVERY production-deploy (catches drift between staging and prod environments).
- After any change to: legal entity (e.g. ИП → ООО transfer), DNS provider, processor list (e.g. swapping Anthropic for a different LLM vendor), or the policy-version constants in `pkg/legalconfig/versions.go` / `services/frontend/lib/legal/versions.ts`.

CI runs the codebase-side parity checks (`make lint-all` includes `check-legal-versions-parity` and `lint-migrations`); the operator still has to walk this checklist because no automation can verify a live РКН registry entry or a DKIM record at the DNS edge.

## 3. Database

- [ ] `migrate up` on a fresh DB reaches the latest version with no errors.
- [ ] `bash scripts/check-migrations-parity.sh` exits 0 (no duplicate versions, every `.up.sql` has a paired `.down.sql`, prod/test counts match the documented divergence).
- [ ] Backups are routinely produced — see §4 (Phase 23-03 backup + restore drill).
- [ ] Phase 22-01 migrations applied: `000016_phase_22_user_consents_forensic.up.sql` AND `000017_phase_22_user_consents_backfill.up.sql` (in `migrations/postgres/`). Confirm by `psql -c "\d user_consents"` showing `withdrawn_at`, `ip`, `user_agent` columns.

## 4. Backups (Phase 23-03)

Backup + restore infrastructure ships in Phase 23-03 (restic + KMS-encrypted password + weekly CI drill). Pre-deploy gate:

- [ ] `RESTIC_PASSWORD_KMS_KEY_ID` + `RESTIC_PASSWORD_CIPHERTEXT` + `RESTIC_REPOSITORY` set in production env (see [docs/runbook-restore.md §1](runbook-restore.md) for KMS key + ciphertext provisioning).
- [ ] Yandex Cloud KMS key + Object Storage bucket exist; backup container's service account has `kms.keys.decrypt` and `storage.editor` permissions.
- [ ] One full backup-restore drill completed in the last 90 days against scratch PG+Mongo per [docs/runbook-restore.md §4](runbook-restore.md).
- [ ] `pushgateway:9091/metrics | grep backup_last_success_timestamp` returns a recent timestamp.
- [ ] Operator has the plaintext restic password saved in the password manager for disaster recovery (verifies WR-04 round-trip equality between operator-saved value and what the container reads from KMS).
- [ ] Weekly `backup-restore-drill.yml` CI job is green (the `entrypoint-roundtrip` sub-job is the regression guard for the `| base64 -d` bug fixed in 23-07).

## 5. Email infrastructure (Phase 21-01)

- [ ] DKIM / SPF / DMARC verified per `docs/runbook-email-dns.md` (run the `dig` checks from §6 of that runbook).
- [ ] `UNISENDER_API_KEY` non-empty in production env (`docker compose exec api env | grep UNISENDER` should show a non-empty value).
- [ ] `UNISENDER_FROM_EMAIL=noreply@onevoice.app` set.
- [ ] Test email lands in real `@yandex.ru` AND `@mail.ru` inboxes (not spam) — operator manually verifies via the Unisender console or the password-reset flow.

## 6. Legal compliance (Phase 22)

Failure of ANY item below is a **HARD block** on production deploy. The launch-readiness document explicitly designates **§6 (Legal compliance)** as the hard-block section so that an operator skimming this checklist cannot accidentally skip it.

### 6.1 Backend env vars (LEGAL_*)

- [ ] `LEGAL_ENTITY_NAME` set to a non-placeholder value matching the РКН registry entity name (the value the operator typed into the Art. 22 filing per `docs/runbook-rkn-filing.md §2.3`).
- [ ] `LEGAL_INN` set to a valid 10-digit (ООО) or 12-digit (ИП) ИНН matching the РКН entry.
- [ ] `LEGAL_ADDRESS` set to the юридический адрес matching the РКН entry.
- [ ] `LEGAL_EMAIL_PDN` set to a routable inbox (default `pdn@onevoice.app`); a third-party test message lands in the inbox.

### 6.2 Frontend env vars (NEXT_PUBLIC_LEGAL_*)

- [ ] `NEXT_PUBLIC_LEGAL_ENTITY_NAME` mirrors `LEGAL_ENTITY_NAME` byte-for-byte.
- [ ] `NEXT_PUBLIC_LEGAL_INN` mirrors `LEGAL_INN`.
- [ ] `NEXT_PUBLIC_LEGAL_ADDRESS` mirrors `LEGAL_ADDRESS`.
- [ ] `NEXT_PUBLIC_LEGAL_EMAIL_PDN` mirrors `LEGAL_EMAIL_PDN`.
- [ ] Helper assertion: `pkg/legalconfig.Entity.IsPlaceholder()` returns false at API startup (no `console.warn` from the frontend's `loadLegalEntity()` either). The runtime assert is a defence-in-depth — this checklist is the primary gate.

### 6.3 Frontend smoke tests (manual, ≤5 min)

- [ ] `/legal/privacy` — Russian renders; `DataControllerBlock` shows the REAL ИНН (not «—»); `Footer` shows the REAL operator name (not «[Юридическое лицо — будет обновлено]»).
- [ ] `/legal/terms` — Russian renders; effective_from + version visible.
- [ ] `/legal/consent` — Russian renders; §3 names the RF processors only (Yandex Cloud / AI Studio, Unisender Go) and states that no cross-border transfer takes place — this MUST match the production LLM wiring (`SELF_HOSTED_N_*` only, `ALLOW_TRANSBORDER_LLM` unset). If the operator deliberately enables a foreign provider, the consent + privacy texts and the Art. 12 filing must be re-done first.
- [ ] `/legal/contact` — `DataControllerBlock` renders the real entity; 15-day SLA copy visible referencing `pdn@onevoice.app`.
- [ ] Footer renders the three legal links + copyright + ПДн contact email on `/`, `/login`, `/register`, `/chat`, `/settings/account`, `/settings/privacy`, `/legal/privacy`.
- [ ] Register a fresh test account — both consent checkboxes are required; submit button is disabled until BOTH are ticked.
- [ ] Switch `/legal/privacy` locale to EN — the «English translation provided for convenience. The Russian version prevails in any dispute.» disclaimer is visible at the top of the page (D-09 source-of-truth disclaimer).

### 6.4 Codebase parity

- [ ] `bash scripts/check-legal-versions-parity.sh` exits 0 (Go `pkg/legalconfig/versions.go` and TS `services/frontend/lib/legal/versions.ts` carry identical TOS / Privacy / PDN version strings). Equivalent invocations: `make check-legal-versions` or `make check-legal-versions-parity`.
- [ ] `make lint-all` exits 0 (which now includes the parity check as a dependency).

### 6.5 РКН filings (per `docs/runbook-rkn-filing.md`)

- [ ] `docs/runbook-rkn-filing.md §2` completed — standard «Уведомление об обработке персональных данных» (152-ФЗ Art. 22) filed; РКН confirmation email received.
- [ ] `docs/runbook-rkn-filing.md §3` — NOT required in the default RF-only LLM setup (no cross-border transfer). Only if `ALLOW_TRANSBORDER_LLM=true` is deliberately set: file the separate «Уведомление о трансграничной передаче персональных данных» (152-ФЗ Art. 12) naming the foreign recipients **AT LEAST 30 days BEFORE launch** (LEGAL-05) and re-issue the consent text first.
- [ ] Operator entry visible at `https://pd.rkn.gov.ru` searching by ИНН.
- [ ] Production env has NO `OPENROUTER_API_KEY` / `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` and `ALLOW_TRANSBORDER_LLM` is unset — the api and orchestrator refuse to boot otherwise (`llm.EnforceResidency`), which is the code-level guarantee behind the "no cross-border transfer" statement in `/legal/privacy` §6.
- [ ] `docs/runbook-pdn-incident.md` reviewed — the 24h / 72h РКН incident-notification duty is understood and the РКН operator account can file it.

### 6.6 PDN-request operations

- [ ] `docs/runbook-pdn-request.md` reviewed by the operator; the 15-day SLA process is understood.
- [ ] `pdn@onevoice.app` inbox (the value of `LEGAL_EMAIL_PDN`) is set up, monitored daily, and the operator's incident-response calendar reflects the SLA.
- [ ] Template responses in `docs/runbook-pdn-request.md §4` are saved as inbox drafts or stored in a shared notes document for fast use.

## §7 Operational hardening — Grafana password gate

**HARD BLOCK.** The deploy MUST NOT proceed if any of these fail.
Source plan: `.planning/phases/23-operational-hardening/23-02-grafana-auth-network-PLAN.md`
(decisions D-09, D-10, D-11).

Run the snippet below from a shell that has `.env.prod` sourced (or in a
context where `GF_SECURITY_ADMIN_PASSWORD` is already exported):

```bash
# 7.1 GF_SECURITY_ADMIN_PASSWORD must be set
if [ -z "${GF_SECURITY_ADMIN_PASSWORD:-}" ]; then
  echo "FAIL §7.1: GF_SECURITY_ADMIN_PASSWORD is empty"; exit 1
fi

# 7.2 Must not be the literal "admin"
if [ "${GF_SECURITY_ADMIN_PASSWORD}" = "admin" ]; then
  echo "FAIL §7.2: GF_SECURITY_ADMIN_PASSWORD is the default 'admin'"; exit 1
fi

# 7.3 Length floor 16 (UTF-8 bytes)
if [ "${#GF_SECURITY_ADMIN_PASSWORD}" -lt 16 ]; then
  echo "FAIL §7.3: GF_SECURITY_ADMIN_PASSWORD shorter than 16 chars"; exit 1
fi

# 7.4 Observability stack must NOT publish ports
if grep -qE '^\s+ports:' docker-compose.observability.yml; then
  echo "FAIL §7.4: docker-compose.observability.yml has a ports: mapping"; exit 1
fi

echo "OK §7 Grafana password gate"
```

Acceptable outcomes:

- All four lines silently pass and the script prints `OK §7 Grafana password gate`.
- Any `FAIL §7.x` line: STOP the deploy. Regenerate the password
  (`openssl rand -base64 24`), update `.env.prod`, re-run the gate.

Operator runbook for accessing Grafana now that the host port is gone:
[docs/runbook-observability-access.md](runbook-observability-access.md).

## 8. Observability network

Sub-gate of §7. After `docker compose -f docker-compose.observability.yml up -d`
with `GF_SECURITY_ADMIN_PASSWORD` set:

- [ ] `ss -tlnp | grep -E ':3003|:9090|:3100'` on the host returns NO host listener for those ports — only Docker-internal bridges.
- [ ] Grafana reachable from a tailnet client at `http://<host-tailnet-name>:3000`.
- [ ] `tailscale status` on the host shows the tailnet up and the production ACL applied.

## 9. Rate limits and login brute-force defence (Phase 23-04)

Phase 23-04 wires lockout + SmartCaptcha on `/auth/login`. Pre-deploy gate:

- [ ] `LOCKOUT_FAIL_THRESHOLD_CAPTCHA` (default 4) and `LOCKOUT_FAIL_THRESHOLD_LOCK` (default 10) set appropriately for v1.4 beta scale.
- [ ] `LOCKOUT_DURATION` (default 15m) set; operators should not lower below 5m (defeats brute-force protection) or raise above 60m (poor UX after typo storms).
- [ ] `TRUSTED_PROXY_CIDRS` matches the production load balancer's published IP ranges. If empty, `X-Forwarded-For` is NEVER trusted (default — fail-closed).
- [ ] `SMARTCAPTCHA_SITE_KEY` + `SMARTCAPTCHA_SECRET_KEY` + `NEXT_PUBLIC_SMARTCAPTCHA_SITE_KEY` all set with non-placeholder values from the Yandex Cloud SmartCaptcha console. If any is empty, the captcha tier (4-9 failed attempts) silently becomes a no-op (boot warning logged) — TierLocked (10+) still gates brute force but the soft-block tier provides zero rate-limit gain.
- [ ] Manual smoke test: 4 wrong-password attempts surface the captcha widget on /login; 10 wrong attempts trip 423 + `code:account_locked` + `Retry-After: <seconds>` header.
- [ ] `RATE_LIMIT_REGISTER` (default 5/min), `RATE_LIMIT_LOGIN` (default 10/min) and `RATE_LIMIT_REFRESH` (default 60/min) set appropriately. Raise `RATE_LIMIT_REFRESH` when many users share one egress IP — each authenticated page load spends one refresh.

## 10. References

- `docs/runbook-email-dns.md` — Email DNS setup (Phase 21-01)
- `docs/runbook-rkn-filing.md` — РКН Art. 22 + Art. 12 cross-border filing (Phase 22-03)
- `docs/runbook-pdn-request.md` — 15-day SLA process for 152-ФЗ Art. 14 subject-rights requests (Phase 22-03)
- `docs/runbook-restore.md` — Backup + restore drill (Phase 23-03)
- `docs/runbook-observability-access.md` — Tailscale operator access to Grafana now that the host port is gone (Phase 23-02)
- `scripts/check-legal-versions-parity.sh` — Drift guard between Go and TS policy-version constants
- `scripts/check-migrations-parity.sh` — Drift guard between prod and test migration paths
- `.env.example` — All required env vars with operator commentary; see §13 «Legal entity (Phase 22)» and §14 «Operational hardening (Phase 23)»

---

*Phase 23 master pre-deploy gate. Update whenever a new operator-facing dependency is added.*
