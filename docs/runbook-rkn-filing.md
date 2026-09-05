# Runbook — РКН Filings (Phase 22, updated for the RF-only LLM contour)

**Audience:** Operator deploying OneVoice to production.
**Blocking:** Pre-launch checklist item — see `docs/runbook-launch-readiness.md §6 (Legal compliance)`. Production deploy MUST NOT proceed without the «Уведомление об обработке персональных данных» (Art. 22) confirmation.
**Operator action only:** Filings are performed manually on `https://pd.rkn.gov.ru` by the operator. This document is the operational checklist of what to file, what to declare verbatim, and what to verify post-filing.
**Legal basis:** 152-ФЗ Art. 22 (standard notification). 152-ФЗ Art. 12 (cross-border transfer notification) applies ONLY if the operator deliberately routes personal data outside the RF (`ALLOW_TRANSBORDER_LLM=true`) — see §3.

> **What changed vs. the June 2026 version.** The production LLM contour is now RF-hosted (DeepSeek V4 Flash on Yandex AI Studio, with an RF-hosted fallback model) and the api / orchestrator refuse to boot in production with any hosted foreign provider key set (`llm.EnforceResidency`). Anthropic PBC and OpenAI L.L.C. are therefore NOT processors and the Art. 12 cross-border filing is NOT required. The legal texts v1.1 (`services/frontend/content/legal/*.md`) state that no cross-border transfer takes place; keep the filing consistent with them.

## 1. Prerequisites

Before you visit `https://pd.rkn.gov.ru`, gather the following. These map 1:1 to the `LEGAL_*` env vars the production deploy will consume — keep both in lock-step.

- [ ] **Verified legal entity** registered in РФ (ООО or ИП). The operator names the entity at deploy time.
- [ ] **ИНН of that entity** — 10 digits for ООО, 12 for ИП. Maps to `LEGAL_INN`.
- [ ] **Юридический адрес of that entity** — the full registered address as listed in ЕГРЮЛ/ЕГРИП. Maps to `LEGAL_ADDRESS`.
- [ ] **Active correspondence email** routed to the entity. РКН sends decisions to this address.
- [ ] **Dedicated PDN inbox** — `pdn@<operator-domain>`. All 152-ФЗ subject-rights requests funnel here. Maps to `LEGAL_EMAIL_PDN`. Must be live and monitored BEFORE filing.
- [ ] **Full list of third-party processors** at filing time (all RF):
  - Yandex Cloud (ООО «Яндекс.Облако») — hosting (compute, object storage, KMS) AND the Yandex AI Studio platform serving the language-model requests (РФ). Request logging on AI Studio is disabled by the self-hosted provider (`x-data-logging-enabled: false`).
  - Unisender Go — transactional email (РФ).
- [ ] **Confirmation that production env carries no hosted foreign LLM key** (`OPENROUTER_API_KEY` / `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` unset, `ALLOW_TRANSBORDER_LLM` unset). The boot gate enforces it; verify anyway before declaring "no cross-border transfer".
- [ ] **Operator account on `https://pd.rkn.gov.ru`** — register with the entity's ИНН and a working email (allow ≥1 business day for activation).

> Cross-check the values against ЕГРЮЛ/ЕГРИП before filing — РКН validates by ИНН and a mismatch bounces the entry back (typical re-loop = 10–15 business days).

## 2. Step-by-step: Standard «Уведомление об обработке персональных данных» (152-ФЗ Art. 22)

| # | Action | Reference |
|---|--------|-----------|
| 2.1 | Sign in to the operator account on `https://pd.rkn.gov.ru`. | — |
| 2.2 | Choose «Подать уведомление об обработке ПДн». | 152-ФЗ Art. 22 §1 |
| 2.3 | Enter legal entity data verbatim from the production env. | `LEGAL_ENTITY_NAME`, `LEGAL_INN`, `LEGAL_ADDRESS` |
| 2.4 | «Начало обработки» — deploy day +1. | — |
| 2.5 | Declare PD categories — §4 below. | `services/frontend/content/legal/privacy.ru.md §3` |
| 2.6 | Declare purposes — §4 below. | `privacy.ru.md §4`, `consent.ru.md §2` |
| 2.7 | Declare processors — §5 below (all РФ). In the «трансграничная передача» section answer **«не осуществляется»**. | — |
| 2.8 | Storage period: «до удаления учётной записи; данные третьих лиц — до отключения интеграции или удаления организации; журналы аудита — 5 лет после удаления учётной записи». | `privacy.ru.md §5` |
| 2.9 | Subject rights: access, correction, deletion, withdrawal, complaint to РКН — tick all five. | 152-ФЗ Art. 14, Art. 21 |
| 2.10 | Legal basis for third-party data: «обработка по поручению оператора (пользователя Сервиса), ст. 6 ч. 3». | `terms.ru.md §7` |
| 2.11 | Submit; expect a 30-day window before the entry appears in the operator search by ИНН. | 152-ФЗ Art. 22 §6 |

## 3. Cross-border filing (152-ФЗ Art. 12) — ONLY if you deliberately leave the RF perimeter

Not required in the default setup. File it only if the operator sets `ALLOW_TRANSBORDER_LLM=true` and configures a foreign provider (this also switches OFF outbound personal-data redaction, so the consent text must disclose the transfer). In that case:

1. Re-issue `consent.ru.md` / `privacy.ru.md` (add the recipients, purposes and legal basis), bump `pkg/legalconfig` + `versions.ts` so every user re-consents.
2. On `https://pd.rkn.gov.ru` choose «Подать уведомление о трансграничной передаче»; name every foreign recipient explicitly (an intermediary gateway does not replace the ultimate provider's name).
3. The USA is not on the РКН adequacy list — РКН may prohibit or restrict the transfer within 10 working days; file at least 30 days before launch.

## 4. PD categories — verbatim phrasing for the form

Categories (mirror `privacy.ru.md §3`):

- адрес электронной почты пользователя
- имя пользователя
- IP-адрес и идентификатор браузера (User-Agent)
- имя, телефон и адрес владельца бизнес-аккаунта
- содержание сообщений пользователя в чате Сервиса
- метаданные интеграций с внешними площадками (в зашифрованном виде)
- данные третьих лиц из подключённых площадок: имена (никнеймы) авторов отзывов и сообщений, тексты отзывов и сообщений, публичные идентификаторы авторов — обрабатываются по поручению пользователя

Purposes (mirror `privacy.ru.md §4`, `consent.ru.md §2`):

- регистрация и аутентификация пользователя
- предоставление функционала Сервиса: управление карточками организаций, публикация материалов и ответов на отзывы, подготовка черновиков с помощью языковых моделей
- ведение журналов аудита действий пользователя
- обеспечение информационной безопасности
- выполнение требований законодательства Российской Федерации

## 5. Third-party processor list — verbatim phrasing

| Processor | Role | Jurisdiction |
|-----------|------|--------------|
| ООО «Яндекс.Облако» (Yandex Cloud) | Хостинг (вычислительные ресурсы, объектное хранилище, KMS); платформа Yandex AI Studio для обработки запросов к языковым моделям, сохранение содержимого запросов отключено | РФ |
| Unisender Go | Транзакционная электронная почта | РФ |

Трансграничная передача: **не осуществляется**.

## 6. Verification checklist (post-filing)

- [ ] Confirmation email received from РКН (typically within 5 business days).
- [ ] Operator registry entry visible at `https://pd.rkn.gov.ru` by ИНН search (within 30 calendar days).
- [ ] `LEGAL_EMAIL_PDN` inbox receives an end-to-end test message from a 3rd-party mailbox.
- [ ] `/legal/privacy` lists the same operator details (legal name, ИНН, address, ПДн email) as the РКН entry — verbatim match.
- [ ] `/legal/privacy §6` and `/legal/consent §3` list exactly the processors declared in §5 and state that no cross-border transfer takes place.
- [ ] Production env: no hosted foreign LLM keys, `ALLOW_TRANSBORDER_LLM` unset (the api / orchestrator would refuse to boot otherwise).

## 7. After verification passes (launch-readiness gate)

Mirror these into `docs/runbook-launch-readiness.md §6`:

- [ ] `LEGAL_ENTITY_NAME`, `LEGAL_INN`, `LEGAL_ADDRESS`, `LEGAL_EMAIL_PDN` set in production env to non-placeholder values matching the РКН entry.
- [ ] `NEXT_PUBLIC_LEGAL_*` mirror the backend values.
- [ ] `/legal/privacy`, `/legal/terms`, `/legal/consent`, `/legal/contact` render in Russian with version v1.1 and `effective_from`.
- [ ] `DataControllerBlock` renders the REAL legal name + ИНН.
- [ ] `Footer` renders the three legal links + operator copyright + ПДн contact email on every public + authed route.
- [ ] РКН registry entry visible (search by ИНН); PDF export saved to operator records.

## 8. Amendments

If the legal entity changes (ИП → ООО, transfer of the business) OR the processor list changes (new hosting, new LLM provider, analytics vendor):

1. File «Уведомление об изменении сведений» on `https://pd.rkn.gov.ru` **within 10 working days** (152-ФЗ Art. 22 §7).
2. Update `LEGAL_*` / `NEXT_PUBLIC_LEGAL_*` env vars; redeploy.
3. Bump `pkg/legalconfig` AND `services/frontend/lib/legal/versions.ts` together (`scripts/check-legal-versions-parity.sh` enforces it); the bump triggers `ReConsentModal` for every active user.
4. Update `privacy.*.md` / `consent.*.md` (RU and EN together — the parity script checks both) — the file change moves the `policy_sha256` carried in audit rows.
5. If the change introduces a foreign processor, follow §3 first.

## 9. References

- 152-ФЗ Art. 22 — Уведомление об обработке ПДн
- 152-ФЗ Art. 12 — трансграничная передача (only when applicable)
- 152-ФЗ Art. 6 ч. 3 — поручение обработки (third-party data on the user's behalf)
- РКН operator portal — `https://pd.rkn.gov.ru`
- `docs/runbook-launch-readiness.md` — master pre-deploy checklist
- `docs/runbook-pdn-request.md` — 15-day SLA on data-subject requests
- `docs/runbook-pdn-incident.md` — 24h / 72h incident notification
- `docs/runbook-founder-manual-actions.md` — the operator's end-to-end manual checklist (legal entity, filings, providers, payments)
