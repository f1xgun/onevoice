# Runbook — Encryption-Key Compromise Response

**Audience:** On-call operator + CTO (interim DPO) + counsel of record.
**Severity:** S0 — production-impacting + regulator-notifiable.
**Legal basis:** 152-ФЗ ст. 21 ч. 3.1 (24-час уведомление РКН), 152-ФЗ ст. 21 ч. 5 (уведомление субъектов), GDPR Art. 33 (72-час аналог — поддерживается дисциплинарно даже без EU-резидентов), КоАП ст. 13.11 ч. 11-12 ред. 30.05.2025 (оборотный штраф до 500 млн ₽ за несвоевременное уведомление), УК ст. 272.1 ред. 11.12.2024.

---

## 0. Status

> **DRAFT** — final polish in Phase 31 after DPIA publication (SEC-15) and formal DPO assignment (OPS-09).
> Until OPS-09 lands, the CTO acts as interim DPO per LEGAL-REVIEW §3 Q9.

Этот документ — процедурный контроль, дополняющий технические митигации Wave 1 (SEC-06 fail-closed mTLS, SEC-07 slog redaction, SEC-10 RPA screenshot hygiene, SEC-11 weak-key ban, SEC-23 LEGAL_* fail-fast). Если любой из технических контролей дал сбой и компрометация подтверждена — этот runbook говорит, что делать.

**Что меняется в Phase 31 при финализации:**
- Подставляется реальное ФИО + контакты DPO (заменяет `[TBD — после OPS-09]`).
- Tabletop drill cadence фиксируется датой первой repetition.
- Шаблон email-уведомления получает финальную юр-вычитку (счас транслирован из LEGAL-REVIEW.md §4.4 «as-is»).
- Раздел 4 «Re-encrypt» переписывается под реальный `cmd/rekey` (Phase 30 deliverable).

**Tabletop drill cadence:** минимум один раз в календарный год. Первая drill — в течение 6 месяцев с момента ship v1.5.

---

## 1. Trigger / Detect

Compromise считается **подтверждённым** при выполнении ЛЮБОГО из триггеров ниже. Подозрение (не уверенность) запускает шаги 1-2 (Revoke + Rotate) превентивно; уведомление РКН и субъектов — после подтверждения факта.

| # | Trigger | Источник сигнала | Confirm? |
|---|---|---|---|
| 1.1 | Suspected leak of `ENCRYPTION_KEY` value | env dump в логах CI, случайный коммит в публичный репозиторий, признание сотрудника, скриншот в Slack | Очень высоко |
| 1.2 | SIEM alert: аномальная скорость decryption на `integration.token_decrypted` | Audit-log из Phase 29 SEC-04 (`LogTokenDecrypted` метрики); пик > 95-percentile baseline | Высоко (требует корреляции) |
| 1.3 | Сообщение от внешнего исследователя / bug bounty | Email на `security@onevoice.app` + PoC демонстрация чтения зашифрованного токена | Высоко |
| 1.4 | Потеря физической инфраструктуры с key material | Декомиссия сервера без шреддинга диска; утрата HSM-устройства; backup-снапшот в неавторизованном S3 bucket | Средне → Высоко после forensics |
| 1.5 | Insider departure с key access | Уход сотрудника с access к prod env (Yandex Cloud Console, Vault, kubectl secrets) без timely rotation | Профилактически: считать compromise при отсутствии ротации в течение 24h после offboarding |

**Decision tree:**

```
Trigger detected?
  │
  ├── YES → Step 2 (Revoke) ВНЕОЧЕРЕДНО. Не ждём подтверждения. Стоимость false-positive ≪ стоимость false-negative.
  │
  └── NO  → SIEM monitoring continues; runbook не запускается.

Trigger confirmed (forensics, PoC, признание)?
  │
  ├── YES → Step 5 (РКН) + Step 6 (Subjects) ОБЯЗАТЕЛЬНО.
  │
  └── NO (false-positive после Revoke) → задокументировать в `docs/post-mortems/`; уведомление РКН НЕ требуется по 152-ФЗ ст. 21 ч. 3.1 (норма привязана к ФАКТУ нарушения, не к подозрению); внутреннее расследование завершается отчётом без внешнего уведомления.
```

**Time-zero (T0):** момент, с которого считается отсчёт 24h (РКН) и 72h (внутреннее расследование) — это момент **подтверждения** инцидента, не момент детекта. Подтверждение фиксируется письменно в `docs/post-mortems/YYYY-MM-DD-encryption-key-compromise.md` с подписью CTO.

---

## 2. Revoke

**Цель:** немедленно лишить компрометирующего значения practical utility. Выполняется в течение 15 минут с момента детекта (не подтверждения — превентивно).

**Phase 30 forward dependency** — следующие команды предполагают envelope encryption с KMS-managed master key (`yc kms symmetric-key`):

```bash
# Disable compromised KMS data-encryption key:
yc kms symmetric-key disable --id <KMS_KEY_ID>

# Verify the key is now in DISABLED state:
yc kms symmetric-key get --id <KMS_KEY_ID> | grep status
```

**Pre-Phase-30 fallback** (текущее состояние v1.5):

```bash
# Rotate ENCRYPTION_KEY env across all pods immediately.
# The new value is generated in Step 3; here we only flip pods off the old one.
kubectl -n onevoice-prod set env deployment/api      ENCRYPTION_KEY="$(cat /secure/new-key)"
kubectl -n onevoice-prod set env deployment/orchestrator ENCRYPTION_KEY="$(cat /secure/new-key)"
kubectl -n onevoice-prod rollout restart deployment/api deployment/orchestrator
```

**Force-disable all active integrations** (admin-only REST endpoint, выкатывается параллельно с Phase 30 envelope encryption — пока не доступен, эквивалент = SQL update `UPDATE integration SET enabled=false WHERE platform IN (...)` через DBA console):

```
PATCH /internal/v1/integrations/disable-all (admin-only)
  Body: { "reason": "encryption_key_compromise_<incident_id>" }
  Result: все active integrations переводятся в `enabled=false`;
          downstream агенты получают 401/403 при попытке использовать
          расшифрованные токены и прерывают workflow.
```

Это вынуждает все business owners пройти re-OAuth flow (для Telegram/VK) или повторный paste-of-Session_id (для Yandex.Бизнес) — что само по себе является механизмом subject-side incident notification: владелец видит «ваша интеграция отключена в связи с инцидентом безопасности» в UI и кликает в email-уведомление из Step 6.

---

## 3. Rotate

**Цель:** ввести в эксплуатацию новый ключ шифрования. Выполняется в течение часа после Step 2.

**Pre-Phase-30 path (v1.5 текущая реализация):**

```bash
# Generate a new 32-byte key (base64 24 → 32 ASCII chars):
openssl rand -base64 24 > /secure/new-key

# Sanity check — must pass SEC-11 validators (no all-zeros, no repeat
# pattern, no zxcvbn score < 3, Shannon entropy >= 3.5):
go run ./services/api/internal/config/validators_cli.go \
    --check-encryption-key "$(cat /secure/new-key)"

# Deploy (см. шаг 2 выше — kubectl set env + rollout restart).
```

**Post-Phase-30 path** (envelope encryption с KMS):

```bash
# Create a new KMS symmetric key:
yc kms symmetric-key create \
    --name onevoice-token-dek-$(date +%Y%m%d) \
    --default-algorithm AES_256

# Capture the new key-id and update production env:
NEW_KEY_ID=$(yc kms symmetric-key list --format json | jq -r '.[0].id')
kubectl -n onevoice-prod set env deployment/api \
    TOKEN_ENCRYPTION_KMS_KEY_ID="$NEW_KEY_ID"
kubectl -n onevoice-prod rollout restart deployment/api
```

В обоих случаях НЕ удаляем старый ключ — он нужен Step 4 (Re-encrypt) для расшифровки legacy ciphertexts. Удаление старого ключа — после успешного завершения re-encryption job и cooling-period в 7 дней.

---

## 4. Re-encrypt

**Цель:** расшифровать все ciphertexts старым ключом и зашифровать новым, без service downtime.

**Phase 30 deliverable:** `cmd/rekey` — выполняет zero-downtime re-encryption с dual-decrypt window.

```bash
# Перед запуском: TOKEN_ENCRYPTION_KMS_VERSION_MAP должен содержать активную
# версию KMS-ключа (формат "<versionID>=<int16>"), иначе rekey прерывается с
# ошибкой, т.к. key_version не может быть записан >= target-version.
go run ./services/api/cmd/rekey \
    --target-version 2 \
    --batch 100 \
    --concurrency 4 \
    --dry-run=false

# Job iterates over the integrations table, decrypts each row (legacy AES via
# ENCRYPTION_KEY, or the previous envelope DEK via KMS), then re-encrypts with
# envelope encryption: a fresh per-row DEK is wrapped by the KMS master key and
# the token is sealed with that DEK. Atomically updates the row + its
# `key_version` column so concurrent reads stay correct during the rolling rekey.
```

**Pre-Phase-30 fallback (v1.5):** автоматизированной re-encryption нет; операционно процесс выглядит так:

1. Force-disable всех integrations (Step 2) → сессии всё равно недействительны.
2. Subject notification (Step 6) с инструкцией повторно подключить интеграцию.
3. При повторном подключении новый ciphertext шифруется уже новым ключом — нет необходимости трогать legacy rows.
4. Старые `encrypted_user_token` колонки помечаются `enabled=false`; через 30 дней delete row (subject-rights cleanup).

**Это операционно дорого при scale-up.** Phase 30 envelope encryption + `cmd/rekey` — реальное решение; runbook v1.5 фиксирует только процедуру.

---

## 5. Notify РКН (24h)

**Дедлайн:** 24 часа с момента подтверждения инцидента (T0 в Step 1).
**Правовая база:** 152-ФЗ ст. 21 ч. 3.1 (ред. 30.05.2025).
**Канал подачи:** `https://pd.rkn.gov.ru` — форма «Уведомление об инциденте» в личном кабинете оператора (тот же, что используется в `docs/runbook-rkn-filing.md` §2).
**Ответственный:** DPO `[TBD — после OPS-09]`; до OPS-09 — CTO выступает interim DPO и подаёт уведомление лично.

**Обязательные поля формы (по 152-ФЗ ст. 21 ч. 3.1 + сложившаяся практика РКН, источник: LEGAL-REVIEW.md §4.4):**

1. Дата и время обнаружения инцидента.
2. Дата и время подтверждения инцидента (T0).
3. Категории ПДн, потенциально затронутых (cookies сессии Yandex `Session_id`, метаданные интеграции, OAuth refresh tokens).
4. Предполагаемое число затронутых субъектов.
5. Предполагаемые последствия (см. соответствующий блок шаблона в Step 6).
6. Принятые меры реагирования (Step 2-4 этого runbook).
7. Контакты ответственного лица (DPO).

**Через 72 часа от T0** — повторное уведомление с дополнительными деталями (forensics результаты, обновлённое число затронутых субъектов, окончательная классификация инцидента). Это формальный обычай РКН, не явная норма 152-ФЗ.

**Если расследование показало, что компрометация НЕ подтверждена** — уведомление РКН не подаётся; внутренний отчёт публикуется в `docs/post-mortems/` для аудита (Step 8).

---

## 6. Notify Subjects

**Дедлайн:** «без неоправданной задержки» (152-ФЗ ст. 21 ч. 5). Практика РКН: 5-10 рабочих дней с T0; чем быстрее — тем лучше для смягчения штрафа. **Рекомендуемая цель: 48 часов от T0** (то есть на следующий рабочий день после РКН-уведомления).

**Канал доставки (обязательно ВСЕ три, не выбор):**
1. Email на адрес владельца business account (primary).
2. In-app notification на dashboard OneVoice (banner + modal на следующем входе).
3. UI-флажок «integration disabled — please reconnect» на странице интеграций (триггерится Step 2 force-disable).

**Шаблон email (transplant верба́тим из LEGAL-REVIEW.md §4.4):**

> Template source: LEGAL-REVIEW.md §4.4. Adapt the bracketed placeholders
> per incident. Subject-line wording is locked by legal counsel; do not edit
> without DPO review.

```
Тема: Важное уведомление о безопасности вашего аккаунта OneVoice

[Имя клиента], здравствуйте.

[ДАТА] мы обнаружили инцидент информационной безопасности,
затронувший данные вашего аккаунта OneVoice.

ЧТО ПРОИЗОШЛО
[Краткое описание — фактическое, без юр-эвфемизмов.
Пример: «Был неавторизованный доступ к ключу шифрования
интеграционных токенов. Мы не можем исключить, что
зашифрованные cookies сессии Яндекс, которые вы передали
нам при подключении Яндекс.Бизнес, были расшифрованы
третьими лицами.»]

КАКИЕ ВАШИ ДАННЫЕ МОГЛИ ПОСТРАДАТЬ
- Cookies сессии Яндекс (Session_id и связанные), которые
  вы передали при подключении Яндекс.Бизнес [ДАТА подключения].
- Метаданные интеграции: ID карточки, дата подключения, история
  действий агента в Яндекс.Бизнес.

КАКОВЫ ПОСЛЕДСТВИЯ
Если cookies были расшифрованы и попали к третьим лицам:
- третьи лица могли получить доступ к вашему аккаунту Яндекс,
  включая Яндекс.Почту, Яндекс.Диск, Яндекс.Деньги, Яндекс.Маркет;
- ваша карточка в Яндекс.Бизнесе могла быть изменена без вашего ведома;
- ваш аккаунт Яндекс может быть заблокирован Яндексом за
  подозрительную активность.

ЧТО МЫ СДЕЛАЛИ
- [ДАТА], [ВРЕМЯ]: обнаружили инцидент.
- [ДАТА], [ВРЕМЯ]: ротировали все ключи шифрования.
- [ДАТА], [ВРЕМЯ]: уведомили Роскомнадзор.
- [ДАТА], [ВРЕМЯ]: запустили внутреннее расследование.
- [ДАТА]: отчёт по результатам расследования будет опубликован.

ЧТО НЕОБХОДИМО СДЕЛАТЬ ВАМ — НЕМЕДЛЕННО
1. Перейдите на https://passport.yandex.ru/profile/devices и
   завершите все активные сессии вашего Яндекс-аккаунта.
2. Смените пароль от Яндекс ID.
3. Включите двухфакторную аутентификацию (если не включена).
4. Проверьте Яндекс.Почту и Яндекс.Диск на подозрительную активность.
5. При обнаружении подозрительных действий обратитесь в Яндекс
   (https://yandex.ru/support/id/troubleshooting/hacked.html).
6. Переподключите Яндекс.Бизнес в OneVoice после смены пароля
   (старая интеграция отключена нами автоматически).

ВАШИ ПРАВА
По 152-ФЗ вы имеете право:
- получить полную информацию о фактически обработанных данных
  и обстоятельствах инцидента (ст. 14 152-ФЗ);
- отозвать согласие и потребовать удаления своих данных
  (ст. 9 ч. 2);
- обратиться в Роскомнадзор с жалобой (https://rkn.gov.ru);
- обратиться в суд за компенсацией морального вреда (ст. 17).

КОНТАКТЫ
- DPO OneVoice: [ИМЯ], [EMAIL], [ТЕЛЕФОН]
- Горячая линия инцидента: [EMAIL]
- Дата отправки: [ДАТА]

Приносим искренние извинения. Этот инцидент — наш провал в защите
данных, которые вы нам доверили. Мы делаем всё возможное чтобы
понять причины и не допустить повторения.

[Подпись: CEO / CTO OneVoice]
```

**Обязательные элементы по ст. 21 ч. 5 152-ФЗ (контрольный чек-лист — каждый блок шаблона выше покрывает соответствующий пункт):**

1. Факт нарушения, дата обнаружения → блок «ЧТО ПРОИЗОШЛО».
2. Категории затронутых ПДн → блок «КАКИЕ ВАШИ ДАННЫЕ МОГЛИ ПОСТРАДАТЬ».
3. Предполагаемые последствия → блок «КАКОВЫ ПОСЛЕДСТВИЯ».
4. Меры по устранению и предотвращению → блок «ЧТО МЫ СДЕЛАЛИ».
5. Контакты ответственного лица (DPO) → блок «КОНТАКТЫ».
6. Информация о правах субъекта (ст. 14 + право на жалобу в РКН) → блок «ВАШИ ПРАВА».

---

## 7. Internal Investigation (72h)

**Цель:** подготовить первичный отчёт о причинах, объёме и последствиях инцидента в течение 72 часов с T0. Это GDPR Art. 33 аналог — даже при отсутствии EU-резидентных субъектов OneVoice поддерживает эту дисциплину как best practice.

**Scope (что входит в отчёт):**

- Forensics timeline — kто, когда, как получил доступ к key material.
- Объём раскрытия — точное число затронутых subjects + категории integrations.
- Атрибуция (если возможна) — внутренний / внешний actor; preserved evidence chain.
- Root cause analysis — какой контроль (или его отсутствие) сделал инцидент возможным.
- Action items — short-term (внутри 7 дней) + medium-term (внутри 30 дней).

**Required artifacts (источники данных):**

- Дамп `audit_log` за окно `T0 - 90d → T0 + 7d`. После landing Phase 29 SEC-04 (`LogTokenDecrypted` + `LogIntegrationAccessed` + `LogRPAScopeViolation`) — это первичный источник; до Phase 29 — приходится восстанавливать из application logs (`pkg/logger` + Loki/CloudWatch dump).
- Pod logs за тот же window (api, orchestrator, agent-*).
- KMS key access log (`yc kms symmetric-key list-operations --id <KEY_ID>` для post-Phase-30; SSH/console access log для pre-Phase-30).
- Git audit — `git log --all --source --remotes -p -S "ENCRYPTION_KEY"` для проверки случайного коммита значения.
- CI/CD audit — environment variables в Actions/GitLab runners, audit secret-loading плагинов.

**Sign-off:** CTO + interim DPO (`[TBD — после OPS-09]`).

**Output:** `docs/post-mortems/YYYY-MM-DD-encryption-key-compromise.md` — формат blameless 5-whys с action items с owners + dates. Публикуется не позднее T0+72h в private repo (`security/post-mortems`), полная версия; redacted version (без identifiers конкретных субъектов) — для public audit при необходимости.

---

## 8. Audit + Post-mortem

**Cadence + governance:**

- Post-mortem document filed at: `docs/post-mortems/YYYY-MM-DD-encryption-key-compromise.md`
- Format: blameless, 5-whys, action items with owners + dates.
- **Tabletop drill cadence: minimum once per calendar year.** First drill within 6 months of v1.5 ship date.
- Tabletop participants: CTO + interim DPO + on-call SRE + counsel of record. Length: 2 часа. Output: short report appended к этому runbook с lessons learned + предложенными правками runbook текста.
- Annual review of this runbook — обязательно после каждого drill; вне расписания — при изменении НПА (152-ФЗ редакции, GDPR amendments), при изменении архитектуры (envelope encryption landing в Phase 30 — runbook ОБЯЗАТЕЛЬНО переписывается).

**Что отслеживается в audit_log как evidence trail после инцидента:**

| Event | Source | Retention |
|---|---|---|
| `incident.detected` | runbook initiator | indefinite |
| `incident.confirmed` | CTO sign-off | indefinite |
| `key.disabled` | KMS / kubectl | 5 years |
| `key.created` | KMS / openssl wrapper | 5 years |
| `integrations.disabled_all` | admin REST endpoint / DBA console | 5 years |
| `rkn.notification_sent` | DPO confirmation | indefinite |
| `subjects.notification_sent` | mail provider receipt + in-app delivery log | 5 years |
| `post_mortem.published` | git commit hash | indefinite |

---

## Annex A — Contact list

| Role | Name | Contact | Notes |
|---|---|---|---|
| DPO (formal) | `[TBD — после OPS-09]` | `[TBD — после OPS-09]` | Phase 31 / OPS-09 deliverable |
| DPO (interim) | CTO | `cto@onevoice.app` | До формального назначения DPO |
| CEO | TBD | `ceo@onevoice.app` | Sign-off на email-уведомление субъектам (см. шаблон в Step 6) |
| Security on-call | rotation | `security@onevoice.app` + PagerDuty escalation policy `onevoice-security` | First responder для шагов 1-2 |
| Counsel of record | TBD | TBD | Юр-сопровождение РКН-уведомления + подготовка ответов на subject-rights запросы |
| РКН (центральный аппарат) | — | `https://rkn.gov.ru/` + `https://pd.rkn.gov.ru` | Подача уведомлений через личный кабинет оператора |
| РКН (региональное управление) | по месту регистрации юр.лица | Контакты на сайте РКН в разделе «Территориальные органы» | Уведомление о территориальном инциденте может дополнительно дублироваться в региональное управление |
| Горячая линия инцидента | — | `[TBD — после OPS-09]` | Публикуется в email-шаблоне (Step 6); до landing OPS-09 = тот же `security@onevoice.app` |

---

## Annex B — Cross-references

- `docs/runbook-rkn-filing.md` — стандартные РКН-уведомления (Art. 22 + cross-border Art. 12); этот runbook добавляет отдельный канал для breach-уведомлений (ст. 21 ч. 3.1).
- `docs/runbook-pdn-request.md` — обработка subject-rights запросов; в кризисный период объём запросов растёт, операторы должны быть готовы.
- `.planning/sib-audit/LEGAL-REVIEW.md` §4.4 — источник subject-notification email template + полный legal commentary.
- `.planning/sib-audit/CRYPTO.md` §C — техническое обоснование угроз ENCRYPTION_KEY compromise.
- `.planning/sib-audit/TARGET-ARCH.md` SEC-14 — формальный requirement, который этот документ закрывает (draft); SEC-17 / SEC-19 / SEC-21 (Phase 30) — envelope encryption + `cmd/rekey` job, на которые этот runbook ссылается как forward dependencies.
