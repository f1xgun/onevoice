# Runbook — Personal-Data Incident (152-ФЗ Art. 21 §3.1 notification)

**Audience:** Operator on duty (founder) and anyone with production access.
**Blocking:** Operational. Becomes binding the moment the РКН Art. 22 registry entry is published (`docs/runbook-rkn-filing.md`).
**Legal basis:** 152-ФЗ Art. 21 §3.1 — on discovering an unlawful or accidental transfer (access, leak) of personal data the operator MUST notify Роскомнадзор **within 24 hours** of discovery and send the results of the internal investigation **within 72 hours**. Missing either window is a separate offence with its own fine, on top of the leak itself.

## 1. What counts as an incident

Any of the following, confirmed or strongly suspected:

- unauthorised read of `users`, `user_consents`, `businesses`, `integrations` (tokens/cookies), `audit_logs`, Mongo conversations / reviews cache, MinIO objects, or backups;
- a leaked secret that protects personal data: `ENCRYPTION_KEY`, `TOKEN_ENCRYPTION_KMS_*` access, `A2A_PAYLOAD_KEY`, `JWT_SECRET`, DB / Redis / Mongo passwords, NATS nkey seeds, mTLS CA key;
- a misconfiguration that exposed personal data publicly (open bucket, debug endpoint, log shipping to a third party, `ALLOW_TRANSBORDER_LLM=true` without a filed basis);
- a compromised provider account (Yandex Cloud, Unisender, Telegram bot token) through which personal data could be read;
- a laptop / workstation with production access lost or compromised.

A bug that shows one user another user's data (tenant isolation failure) is an incident even for a single record.

## 2. Timeline

| Deadline | Action |
|----------|--------|
| T+0 | Discovery. Write down the exact time (UTC and Moscow). Start an incident log (`docs/manual-audit-log.md` section or a dedicated file outside the repo). |
| T+1h | Contain: revoke / rotate the compromised secret (see §3), block the exposure path, snapshot evidence (logs, `audit_logs` export, container images) BEFORE changing anything else. |
| T+4h | Scope: which tables / subjects / categories; how many subjects; whether special categories (health data inside clinic reviews) were involved. |
| ≤ T+24h | **Notification №1 to РКН** via the operator account on `https://pd.rkn.gov.ru` → «Уведомление об инциденте» (form per Art. 21 §3.1). Also inform ГосСОПКА / НКЦКИ if the incident is a computer attack (Указ 250 / 187-ФЗ regime applies to операторы ИС; file through the РКН form's НКЦКИ section when offered). |
| ≤ T+72h | **Notification №2 to РКН** — results of the internal investigation: cause, persons responsible (if any), measures taken, measures to prevent recurrence. |
| ≤ T+72h | Notify affected subjects when the leak creates a real risk for them (leaked credentials, cookies, contact data): email via the outbox with what leaked, what we did, what they should do (change password, re-connect integration). Russian text, plain language. |
| T+7d | Post-mortem in the repo (`docs/incidents/YYYY-MM-DD-<slug>.md`), with the fixes as PRs. |

## 3. Containment cheatsheet

- **`ENCRYPTION_KEY` / KMS** — follow `docs/runbook-encryption-key-compromise.md` (rotation + re-encryption + forced re-connect).
- **`A2A_PAYLOAD_KEY`** — generate a new 32-byte key, set on `api` AND `agent-yandex-business` together, redeploy both; in-flight Yandex connect flows fail closed and are retried by the user.
- **NATS nkey seeds** — `make clean-nats-creds && make nats-creds`, redeploy every service (they mount their own seed); old seeds are refused by the server as soon as `auth.conf` changes.
- **`JWT_SECRET`** — rotate; every session is invalidated; users log in again.
- **DB / Redis / Mongo passwords** — rotate in the secret store, restart the stack; check `audit_logs` for `integration.token_decrypted` bursts that do not match user activity.
- **Yandex Cloud** — revoke the service-account key (`yc iam access-key delete`), check Audit Trails for reads of the bucket / KMS.
- **Telegram bot token** — `/revoke` in @BotFather, update `TELEGRAM_BOT_TOKEN`, redeploy `agent-telegram` and `api`.

## 4. Evidence to preserve

- `audit_logs` export for the window (`SELECT * FROM audit_logs WHERE created_at BETWEEN … ORDER BY created_at`) — the 5-year retention exists for exactly this.
- Container logs (`docker compose logs --since <T-1h>`), nginx access logs, Yandex Cloud Audit Trails.
- The compromised artefact itself (screenshot of the public bucket listing, the leaked file hash) — never the plaintext personal data beyond what is needed to scope.

## 5. What to write to РКН (structure)

1. Operator details (verbatim from the registry entry: name, ИНН, address, contact).
2. Date and time of discovery; date and time of the incident if known.
3. Categories of personal data and the estimated number of subjects.
4. Description of what happened and the suspected cause.
5. Measures already taken (containment) and planned.
6. Contact person for the investigation.

Keep the wording factual and short; the 72-hour report expands each point with the investigation result.

## 6. After the incident

- Bump the relevant runbook and `docs/security.md` threat model with what was learned.
- If the incident changed the processor list or the data flows, file «Уведомление об изменении сведений» per `docs/runbook-rkn-filing.md §8`.
- Repeat leaks are what the turnover-based fines in 152-ФЗ / КоАП target; the prevention measures in the 72-hour report must actually ship as PRs.

## 7. References

- 152-ФЗ Art. 21 §3.1 — 24h / 72h notification duty
- КоАП ст. 13.11 — fines for leaks and for missing the notification windows
- `docs/runbook-encryption-key-compromise.md` — key rotation procedure
- `docs/runbook-rkn-filing.md` — registry entry and amendment filings
- `docs/runbook-pdn-request.md` — subject-rights inbox (affected subjects will write there)
