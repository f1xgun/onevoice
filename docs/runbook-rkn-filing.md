# Runbook — РКН Filings (Phase 22)

**Audience:** Operator deploying OneVoice to production.
**Blocking:** Pre-launch checklist item — see `docs/runbook-launch-readiness.md §6 (Legal compliance)`. Production deploy MUST NOT proceed without the «Уведомление об обработке персональных данных» (Art. 22) confirmation AND the separate cross-border-transfer notification (Art. 12 amended, 1-Sept-2025).
**Operator action only:** Phase 22 does NOT generate a pre-filled form template (no document auto-generation). Filings are performed manually on rkn.gov.ru by the operator. This document is the operational checklist of what to file, what to declare verbatim, and what to verify post-filing.
**Legal basis:** 152-ФЗ Art. 22 (standard notification) + 152-ФЗ Art. 12 amended on 1 Sept 2025 (cross-border transfer notification — now a SEPARATE filing).

## 1. Prerequisites

Before you visit `https://pd.rkn.gov.ru`, gather the following. These map 1:1 to the `LEGAL_*` env vars the production deploy will consume — keep both in lock-step.

- [ ] **Verified legal entity** registered in РФ (ООО or ИП). Phase 22 does not commit a default — the operator names the entity at deploy time.
- [ ] **ИНН of that entity** — 10 digits for ООО, 12 for ИП. Maps to `LEGAL_INN`.
- [ ] **Юридический адрес of that entity** — the full registered address (city, street, building, room as listed in ЕГРЮЛ/ЕГРИП). Maps to `LEGAL_ADDRESS`.
- [ ] **Active correspondence email** routed to the entity. РКН sends decisions to this address. Best practice: a shared inbox the operator monitors daily.
- [ ] **Dedicated PDN inbox** — `pdn@onevoice.app` (or operator-domain equivalent). All 152-ФЗ subject-rights requests funnel here. Maps to `LEGAL_EMAIL_PDN`. Must be live and monitored BEFORE filing (РКН confirmation email lands here too).
- [ ] **Full list of third-party processors** at filing time:
  - Yandex Cloud — hosting (РФ)
  - Unisender Go — transactional email (РФ)
  - OpenRouter — LLM gateway (optional, depending on deployment configuration)
  - Anthropic PBC — LLM provider (США) → cross-border filing required (§3 below)
  - OpenAI L.L.C. — LLM provider (США) → cross-border filing required (§3 below)
- [ ] **Operator account on `https://pd.rkn.gov.ru`** — register with the entity's ИНН and a working email (allow ≥1 business day for activation if first time).

> Cross-check the values against ЕГРЮЛ/ЕГРИП before filing — РКН validates by ИНН and a mismatch causes the entry to bounce back for correction (typical re-loop = 10–15 business days).

## 2. Step-by-step: Standard «Уведомление об обработке персональных данных» (152-ФЗ Art. 22)

| # | Action | Reference |
|---|--------|-----------|
| 2.1 | Visit `https://pd.rkn.gov.ru` and sign in to the operator account from §1. | — |
| 2.2 | Choose «Подать уведомление об обработке ПДн» (the standard Art. 22 notification). | 152-ФЗ Art. 22 §1 |
| 2.3 | Enter legal entity data verbatim. Source: production env vars set by the operator. | `LEGAL_ENTITY_NAME`, `LEGAL_INN`, `LEGAL_ADDRESS` |
| 2.4 | Choose «начало обработки», fill expected start date — use deploy-day +1 (one calendar day buffer for the form review). | — |
| 2.5 | Declare PD categories — see §4 for the exact RU strings to paste. | services/frontend/content/legal/privacy.ru.md §3 |
| 2.6 | Declare purposes of processing — see §4. | services/frontend/content/legal/privacy.ru.md §4 |
| 2.7 | Declare processors — see §5. List Yandex Cloud + Unisender Go in the РФ block; declare US-resident processors in the cross-border filing (§3). | — |
| 2.8 | Storage period: «до удаления учётной записи + 5 лет аудит-логи» (until account deletion + 5 years audit retention per Phase 21 ACCT-06). | docs/runbook-email-dns.md §7 (audit-row precedent) |
| 2.9 | Subject rights checklist: access, correction, deletion, withdrawal, complaint to РКН — tick all five. | 152-ФЗ Art. 14, Art. 21 |
| 2.10 | Submit; expect a 30-day processing window before the entry appears at `https://pd.rkn.gov.ru` operator-search by ИНН. | 152-ФЗ Art. 22 §6 |

## 3. Step-by-step: Separate cross-border «Уведомление о трансграничной передаче персональных данных» (152-ФЗ Art. 12 amended, 1-Sept-2025)

This is a **separate** filing — not a section of the Art. 22 notification. Since 1 September 2025 cross-border transfers to non-«adequate» jurisdictions require their own form. OneVoice's LLM providers Anthropic PBC and OpenAI L.L.C. are US-based; the US is not on the РКН adequacy list. **File this notification at least 30 days BEFORE production launch** (LEGAL-05 — РКН has 10 working days to object; build a buffer in.)

| # | Action |
|---|--------|
| 3.1 | Visit `https://pd.rkn.gov.ru`; choose «Подать уведомление о трансграничной передаче». |
| 3.2 | Declare US recipients explicitly and verbatim: «Anthropic PBC (США)» and «OpenAI L.L.C. (США)». If the deploy uses OpenRouter as the LLM gateway, list it too (but note that OpenRouter routes onward to Anthropic/OpenAI so the disclosure must still name the ultimate provider). |
| 3.3 | Purpose of transfer (verbatim RU): «Обеспечение работы LLM-провайдеров для обработки запросов пользователей в рамках предоставления сервиса OneVoice». |
| 3.4 | Legal basis: «Согласие субъекта персональных данных» — link to `/legal/consent` (the user has explicitly consented to cross-border transfer during registration per Plan 22-02 Surface C / Surface D). The published `services/frontend/content/legal/consent.ru.md` text is the verbatim copy РКН expects to see. |
| 3.5 | Expected start date of cross-border transfer — set 30 days from filing, NOT earlier. РКН can object within 10 working days; an additional 20-day buffer protects against last-minute objections triggering re-filing. |
| 3.6 | Submit; expect 30-day processing. Confirmation arrives by email + appears in the operator-account dashboard. Cross-border filings show up as a separate entry from the Art. 22 notification on `https://pd.rkn.gov.ru`. |

## 4. PD categories — verbatim phrasing for the form

Paste each line into the appropriate form field on `https://pd.rkn.gov.ru` so the РКН entry matches the published policy text byte-for-byte (D-09 — Russian source of truth).

Categories of PD processed (mirror `services/frontend/content/legal/privacy.ru.md §3`):

- адрес электронной почты пользователя
- IP-адрес и идентификатор сессии
- User-Agent (тип браузера и устройства)
- контактная информация владельца бизнеса (имя, телефон, e-mail в рамках бизнес-аккаунта)
- содержимое пользовательских сообщений в чате (UGC)

Purposes of processing (mirror `services/frontend/content/legal/privacy.ru.md §4` and `consent.ru.md §3`):

- регистрация и аутентификация пользователя
- предоставление основного функционала сервиса (управление бизнес-аккаунтами, чат с ассистентом)
- обеспечение безопасности и аудит действий пользователя
- хранение журналов аудита в течение установленного законом срока

## 5. Third-party processor list — verbatim phrasing

Use this order on the form (Russian processors first, then a separate cross-border line — even though the cross-border filing is a different notification, the standard Art. 22 form has its own «трансграничная передача» disclosure section that must list the same recipients):

| Processor | Role | Jurisdiction | Filing scope |
|-----------|------|--------------|--------------|
| Yandex Cloud | Хостинг (compute + объектное хранилище) | РФ | §2 standard filing |
| Unisender Go | Транзакционная электронная почта | РФ | §2 standard filing |
| OpenRouter (если используется) | Шлюз доступа к LLM-провайдерам | — | §3 cross-border filing (списан как маршрутизатор) |
| Anthropic PBC | LLM-провайдер | США | §3 cross-border filing — REQUIRED |
| OpenAI L.L.C. | LLM-провайдер | США | §3 cross-border filing — REQUIRED |

## 6. Verification checklist (post-filing)

- [ ] Confirmation email received from РКН (typically within 5 business days of submit) — both for the Art. 22 standard notification AND the Art. 12 cross-border notification.
- [ ] Operator registry entry visible at `https://pd.rkn.gov.ru` by ИНН search (within 30 calendar days of submit).
- [ ] Cross-border transfer notification listed separately in the operator dashboard (it is NOT a sub-entry of the Art. 22 filing).
- [ ] Operator inbox `pdn@onevoice.app` (the value of `LEGAL_EMAIL_PDN`) receives an end-to-end test message from a 3rd-party mailbox.
- [ ] Privacy policy at `/legal/privacy` lists the same operator details (legal name, ИНН, address, ПДн email) as the РКН entry — verbatim match.
- [ ] `/legal/consent` lists the same cross-border recipients (Anthropic PBC, OpenAI L.L.C.) declared in the §3 filing.

## 7. After verification passes (D-29 launch-readiness gate)

Mirror these into `docs/runbook-launch-readiness.md §6` and check them off there too — the launch-readiness checklist is the master pre-deploy gate.

- [ ] `LEGAL_ENTITY_NAME`, `LEGAL_INN`, `LEGAL_ADDRESS`, `LEGAL_EMAIL_PDN` set in production env to non-placeholder values matching the РКН registry entry.
- [ ] `NEXT_PUBLIC_LEGAL_ENTITY_NAME`, `NEXT_PUBLIC_LEGAL_INN`, `NEXT_PUBLIC_LEGAL_ADDRESS`, `NEXT_PUBLIC_LEGAL_EMAIL_PDN` mirror the backend values.
- [ ] `/legal/privacy`, `/legal/terms`, `/legal/consent`, `/legal/contact` render in Russian with current version + `effective_from` date.
- [ ] `DataControllerBlock` on `/legal/privacy` and `/legal/contact` renders the REAL legal name + ИНН (no «—», no «[Юридическое лицо — будет обновлено]»).
- [ ] `Footer` renders the three legal links + operator copyright + ПДн contact email on every public + authed route.
- [ ] РКН registry entry visible at `https://pd.rkn.gov.ru` (search by ИНН).
- [ ] Cross-border filing confirmation received and saved to the operator records (PDF export of the dashboard entry recommended).

## 8. Rollback

If the legal entity changes (e.g. ИП → ООО, or operator transfers business) OR the cross-border processor list changes (e.g. swapping Anthropic for Yandex GPT and dropping OpenAI):

1. File a «Уведомление об изменении сведений» on `https://pd.rkn.gov.ru` **within 10 working days** per 152-ФЗ Art. 22 §7. Late amendments trigger Art. 19 risk (subject complaint to РКН).
2. Update `LEGAL_*` and `NEXT_PUBLIC_LEGAL_*` env vars in production. Roll a fresh deploy.
3. Bump `pkg/legalconfig.PrivacyVersion` AND `services/frontend/lib/legal/versions.ts PRIVACY_VERSION` together (the parity script `scripts/check-legal-versions-parity.sh` enforces this — see `docs/runbook-launch-readiness.md §6`). The bump triggers `ReConsentModal` for every active user so they must re-accept the new operator + the updated processor list.
4. Update `services/frontend/content/legal/privacy.ru.md` and `consent.ru.md` operator block and processor list — committing the file changes the `policy_sha256` carried in audit-log rows.
5. If the change is JUST a cross-border processor swap (no entity change), file an «Уведомление об изменении сведений о трансграничной передаче» separately — the Art. 12 cross-border filing is independent of Art. 22 and does not auto-update.

## 9. References

- 152-ФЗ Art. 22 (Уведомление об обработке ПДн) — `https://www.consultant.ru/document/cons_doc_LAW_61801/`
- 152-ФЗ Art. 12 amended on 1 Sept 2025 (cross-border transfer notification) — refer to the latest ConsultantPlus revision
- РКН operator portal — `https://pd.rkn.gov.ru`
- Public operator search — `https://pd.rkn.gov.ru` (search by ИНН)
- `.planning/research/FEATURES.md §Flow 4` — verbatim policy language and timing precedent
- `.planning/phases/22-legal-compliance-scaffolding/22-CONTEXT.md` D-19..D-24, D-29 — ID-level traceability
- `docs/runbook-launch-readiness.md` — master pre-deploy checklist (§6 Legal compliance HARD block)
- `docs/runbook-pdn-request.md` — 15-day SLA on data-subject requests (152-ФЗ Art. 14 §2)

---

*Phase 22-03 operator handoff. Update whenever the РКН form fields or processor list changes.*
