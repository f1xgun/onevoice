# OneVoice Launch Readiness — Pre-Deploy Checklist

**Audience:** Operator deploying OneVoice to production.
**Run frequency:** Every staging-deploy AND every production-deploy. CI does NOT replace this checklist — CI catches drift inside the codebase; the items below verify the running deployment against the operator's external commitments (DNS, РКН, env vars, mailbox routability).
**Skip-blockers:** A failure in §6 (Legal compliance) is a HARD block — production deploy must not proceed. §3 (Database), §5 (Email), and §6 must all be GREEN before flipping the public DNS record.

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
- [ ] Backups are routinely produced — see §4 (deferred).
- [ ] Phase 22-01 migrations applied: `000016_phase_22_user_consents_forensic.up.sql` AND `000017_phase_22_user_consents_backfill.up.sql` (in `migrations/postgres/`). Confirm by `psql -c "\d user_consents"` showing `withdrawn_at`, `ip`, `user_agent` columns.

## 4. Backups

Deferred to Phase 23.1 (per `.planning/milestones/v1.4-ROADMAP.md`). Placeholder until shipped:

- [ ] *(deferred)* Backup schedule documented and tested.
- [ ] *(deferred)* Restore drill performed within the last 90 days.

## 5. Email infrastructure (Phase 21-01)

- [ ] DKIM / SPF / DMARC verified per `docs/runbook-email-dns.md` (run the `dig` checks from §6 of that runbook).
- [ ] `UNISENDER_API_KEY` non-empty in production env (`docker compose exec api env | grep UNISENDER` should show a non-empty value).
- [ ] `UNISENDER_FROM_EMAIL=noreply@onevoice.app` set.
- [ ] Test email lands in real `@yandex.ru` AND `@mail.ru` inboxes (not spam) — operator manually verifies via the Unisender console or the password-reset flow.

## 6. Legal compliance (Phase 22 — THIS PLAN)

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
- [ ] `/legal/consent` — Russian renders; cross-border section names Anthropic PBC + OpenAI L.L.C. matching the РКН Art. 12 filing.
- [ ] `/legal/contact` — `DataControllerBlock` renders the real entity; 15-day SLA copy visible referencing `pdn@onevoice.app`.
- [ ] Footer renders the three legal links + copyright + ПДн contact email on `/`, `/login`, `/register`, `/chat`, `/settings/account`, `/settings/privacy`, `/legal/privacy`.
- [ ] Register a fresh test account — both consent checkboxes are required; submit button is disabled until BOTH are ticked.
- [ ] Switch `/legal/privacy` locale to EN — the «English translation provided for convenience. The Russian version prevails in any dispute.» disclaimer is visible at the top of the page (D-09 source-of-truth disclaimer).

### 6.4 Codebase parity

- [ ] `bash scripts/check-legal-versions-parity.sh` exits 0 (Go `pkg/legalconfig/versions.go` and TS `services/frontend/lib/legal/versions.ts` carry identical TOS / Privacy / PDN version strings). Equivalent invocations: `make check-legal-versions` or `make check-legal-versions-parity`.
- [ ] `make lint-all` exits 0 (which now includes the parity check as a dependency).

### 6.5 РКН filings (per `docs/runbook-rkn-filing.md`)

- [ ] `docs/runbook-rkn-filing.md §2` completed — standard «Уведомление об обработке персональных данных» (152-ФЗ Art. 22) filed; РКН confirmation email received.
- [ ] `docs/runbook-rkn-filing.md §3` completed — separate «Уведомление о трансграничной передаче персональных данных» (152-ФЗ Art. 12 amended) filed naming Anthropic PBC + OpenAI L.L.C. as US recipients. **Filed AT LEAST 30 days BEFORE launch** (LEGAL-05). Confirmation email received.
- [ ] Operator entry visible at `https://pd.rkn.gov.ru` searching by ИНН.
- [ ] Cross-border filing appears as a separate entry in the operator dashboard.

### 6.6 PDN-request operations

- [ ] `docs/runbook-pdn-request.md` reviewed by the operator; the 15-day SLA process is understood.
- [ ] `pdn@onevoice.app` inbox (the value of `LEGAL_EMAIL_PDN`) is set up, monitored daily, and the operator's incident-response calendar reflects the SLA.
- [ ] Template responses in `docs/runbook-pdn-request.md §4` are saved as inbox drafts or stored in a shared notes document for fast use.

## 7. Observability

Deferred to Phase 23.2 (per `.planning/milestones/v1.4-ROADMAP.md`). Placeholder until shipped:

- [ ] *(deferred)* Grafana dashboards covering API + orchestrator + agents are live.
- [ ] *(deferred)* Alert thresholds documented for `email_outbox_failed_rows_total`, `llm_request_errors_total`, `tool_dispatch_timeouts_total`.

## 8. Rate limits

Deferred to Phase 23.4 (per `.planning/milestones/v1.4-ROADMAP.md`). Placeholder until shipped:

- [ ] *(deferred)* `RATE_LIMIT_REGISTER`, `RATE_LIMIT_LOGIN`, `RATE_LIMIT_CHAT`, `RATE_LIMIT_HITL` env vars set appropriately for v1.4 beta scale.
- [ ] *(deferred)* `RateLimiter` from `pkg/llm/` wired into the orchestrator and integration-tested.

## 9. References

- `docs/runbook-email-dns.md` — Email DNS setup (Phase 21-01)
- `docs/runbook-rkn-filing.md` — РКН Art. 22 + Art. 12 cross-border filing (Phase 22-03)
- `docs/runbook-pdn-request.md` — 15-day SLA process for 152-ФЗ Art. 14 subject-rights requests (Phase 22-03)
- `scripts/check-legal-versions-parity.sh` — Drift guard between Go and TS policy-version constants
- `scripts/check-migrations-parity.sh` — Drift guard between prod and test migration paths
- `.env.example` — All required env vars with operator commentary; see §13 «Legal entity (Phase 22)» and the PHASE 22 LAUNCH GATE warning block

---

*Phase 22-03 master pre-deploy gate. Update whenever a new operator-facing dependency is added.*
