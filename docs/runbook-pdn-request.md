# Runbook — Responding to 152-ФЗ Subject-Rights Requests (Phase 22)

**Audience:** Operator monitoring the `pdn@onevoice.app` inbox (or operator-domain equivalent — value of `LEGAL_EMAIL_PDN`).
**Blocking:** Operational, not deploy-blocking. Becomes binding the moment the РКН Art. 22 entry is published per `docs/runbook-rkn-filing.md §6`.
**Legal basis:** 152-ФЗ Art. 14 §2 — operator MUST respond within **15 working days** of receipt of a verified subject request.

## 1. What is a subject-rights request?

A 152-ФЗ data subject (the natural person whose PD the operator processes) may at any time send a written request to the operator demanding one of the following:

- **Access** — copy of all PD the operator holds about them (Art. 14 §1).
- **Correction** — fix incorrect or outdated PD (Art. 14 §1).
- **Deletion** — erase PD when no longer needed (Art. 14 §1, Art. 21 §3).
- **Withdrawal of consent** — revoke a prior consent (Art. 9 §2, Art. 21).
- **Information on processors** — list of third parties to whom PD is transferred (Art. 14 §7).
- **Information on cross-border transfer** — destinations, purposes, legal basis (Art. 12).

Requests typically arrive at `pdn@onevoice.app` (value of `LEGAL_EMAIL_PDN`). This is the inbox advertised at `/legal/privacy`, `/legal/contact`, and in the РКН registry filing.

## 2. SLA — 15 working days

| Day | Action |
|-----|--------|
| 0 | Request received in `pdn@onevoice.app`. |
| ≤2 | Acknowledge receipt + log in operator inbox (separate folder per request). |
| ≤10 | If extension needed, send an interim acknowledgement citing the reason (152-ФЗ Art. 14 §3 allows up to 30 days total when justified). |
| ≤15 | Substantive response in Russian (default — switch to English ONLY on explicit request, Russian remains source of truth per D-09). |
| >15 | Subject may complain to РКН (Art. 19). Missing the SLA materially raises Art. 19 risk. |

> Working days, not calendar days. Russian holidays count as non-working (consultantplus or government.ru holiday calendar). When in doubt, treat as calendar days for an extra buffer.

## 3. Process

### 3.1 Identity verification (every request — no exceptions)

- If the request email matches a registered user's primary email → identity verified (1st factor).
- If it does NOT match → ask the requester for **either** (a) ИНН + a passport scan that matches the registered user's profile, **or** (b) a request signed via «Госуслуги»'s electronic-signature channel. 152-ФЗ Art. 14 §3 explicitly permits requiring identity proof. Do NOT respond to anonymous tips.

### 3.2 Access requests

- v1.4 stop-gap: manual export. Run admin SQL (read-only role) against `users`, `user_consents`, `audit_logs` filtering by `user_id`. Save the export as a password-protected `.zip`; send the password via a separate channel (e.g. SMS to the requester's verified phone).
- v1.5 self-service `GET /users/me/export.zip` is **deferred** — note this as `TBD` if asked; the manual export is sufficient for current launch traffic per ROADMAP estimates.

### 3.3 Deletion requests

- Direct the user to «Настройки → Конфиденциальность» (the WithdrawalPanel surface shipped in Plan 22-02, Surface F). Self-service path: «Отозвать согласие на ПДн» → 30-day grace per Phase 21-04 → permanent deletion.
- If the user cannot reach the self-service flow (e.g. lost access to their account), operator runs the deletion server-side: SQL `UPDATE users SET deletion_requested_at = NOW(), deletion_reason = 'consent_withdrawn'` then let the Phase 21-04 grace-period worker finish the rest. Always log the operator action in `docs/manual-audit-log.md`.

### 3.4 Withdrawal requests

- Same UX as deletion — withdrawal of TOS / Privacy / PDN consent is BUNDLED in the WithdrawalPanel and triggers the same 30-day deletion path (Phase 22-01 D-13, D-14). There is no «withdraw consent but keep the account» path in v1.4 (you cannot use the service without TOS+Privacy+PDN consent — withdrawing any one of them implies account closure).

### 3.5 Correction requests

- v1.4 has no self-service profile edit for email or other PD fields. After identity verification, the operator runs an admin SQL `UPDATE users SET email = $new WHERE id = $verified_user_id` (or the appropriate column). Audit-log the action manually in `docs/manual-audit-log.md` until the programmatic mapping ships (deferred — v1.5+).

### 3.6 Audit-log every operator action

- Use existing `pkg/audit` actions where they map cleanly: `consent.recorded`, `consent.withdrawn` already fire from the self-service surfaces.
- For manual operator actions (SQL edits, manual export sends), append a row to `docs/manual-audit-log.md` (operator-maintained file outside the codebase) with: timestamp, operator email, request ID, action taken, affected user_id, link to inbox thread. РКН audits can request this log.

## 4. Template responses (Russian)

Copy-paste these into the reply email, fill the `{placeholders}`, and route from `pdn@onevoice.app`.

### 4.1 Acknowledge (day 0–2)

> Здравствуйте, {имя}!
>
> Подтверждаем получение Вашего обращения от {дата} касающегося обработки персональных данных. В соответствии с ч. 2 ст. 14 Федерального закона № 152-ФЗ ответ будет направлен в течение 15 рабочих дней с даты подтверждения Вашей личности.
>
> Для подтверждения личности просим направить {ИНН + скан паспорта / запрос, подписанный через Госуслуги}.
>
> С уважением,
> Оператор персональных данных
> {LEGAL_ENTITY_NAME} (ИНН {LEGAL_INN})

### 4.2 Access — fulfilled

> Здравствуйте, {имя}!
>
> Во исполнение Вашего запроса от {дата} направляем выгрузку обрабатываемых нами персональных данных. Архив защищён паролем, который направим отдельным каналом (SMS на номер, привязанный к учётной записи).
>
> {краткое перечисление: e-mail, IP, история согласий, события аудит-лога}.
>
> Если у Вас есть вопросы по составу выгрузки, ответьте на это письмо.

### 4.3 Deletion — referred to self-service

> Здравствуйте, {имя}!
>
> Для удаления Вашей учётной записи и связанных с ней данных воспользуйтесь самостоятельной отменой согласия на обработку ПДн: после входа в учётную запись откройте «Настройки → Конфиденциальность» → «Отозвать согласие на ПДн». В течение 30 дней Ваши данные будут удалены окончательно (152-ФЗ ст. 21).
>
> Если Вы не имеете доступа к учётной записи, ответьте на это письмо — мы выполним удаление от имени оператора после подтверждения личности.

### 4.4 Correction — fulfilled

> Здравствуйте, {имя}!
>
> Подтверждаем внесение исправлений: {что изменено}. Изменение зафиксировано в журнале аудита {дата, ID записи}. Если требуется дополнительная корректировка — ответьте на это письмо.

### 4.5 Information on processors / cross-border transfers

> Здравствуйте, {имя}!
>
> На текущую дату Ваши персональные данные обрабатываются нашими подрядчиками в следующем составе: Yandex Cloud (хостинг, РФ), Unisender Go (транзакционная почта, РФ). Трансграничная передача осуществляется в LLM-провайдеры Anthropic PBC (США) и OpenAI L.L.C. (США) на основании Вашего согласия от {дата согласия}. Подробности — в политике по адресу `/legal/consent`.

## 5. Escalation

- If the request is unclear, fragmented, or appears to come from a representative without authority → ask for clarification within day 5.
- If the requester threatens an РКН complaint, escalate to legal counsel **before** the substantive reply. Document the entire chain in the operator inbox; РКН will request it during an investigation.
- If the request would force operator action incompatible with another regulation (e.g. AML retention vs. deletion request), respond with the legal-conflict reasoning and cite the conflicting law. РКН recognises lawful-conflict refusals.

## 6. References

- 152-ФЗ Art. 14 — operator obligations + 15-day SLA — `https://www.consultant.ru/document/cons_doc_LAW_61801/`
- 152-ФЗ Art. 21 — withdrawal + obligation to delete
- 152-ФЗ Art. 19 — РКН complaint mechanism (what happens if SLA is missed)
- `docs/runbook-rkn-filing.md` — РКН registry filing (the entry that publicises `pdn@onevoice.app` as the official inbox)
- `docs/runbook-launch-readiness.md` §6 — operator MUST have this runbook reviewed + the inbox live before deploy
- `services/frontend/app/(app)/settings/privacy/page.tsx` — the WithdrawalPanel self-service surface mentioned in §3.3 and §3.4

---

*Phase 22-03 operator handoff. Update whenever 152-ФЗ amendments change the SLA or the form of substantive responses.*
