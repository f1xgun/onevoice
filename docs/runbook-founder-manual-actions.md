# Runbook — ручные действия фаундера до и после запуска

**Для кого:** фаундер / оператор. Всё, что нельзя сделать кодом: юрлицо, подачи в РКН, провайдеры, платежи, аккаунты площадок, прод-хост.
**Как читать:** блоки идут в порядке критического пути. Внутри блока — чекбоксы. Ссылки на код и другие runbook'и даны там, где нужно свериться. Рядом с шагом стоит ориентир по времени и стоимости, где он известен.
**Статус кода на 2026-09-06:** ядро 152-ФЗ реализовано (согласия, удаление, аудит 5 лет, редакция ПДн, RF-only LLM-гейт в prod). Единственный настоящий блокер запуска — юрлицо: без него нет подачи в РКН, оферты и приёма платежей.

---

## 0. До всего: решения, которые нужно зафиксировать

- [ ] **LLM-контур остаётся РФ-only.** Primary — DeepSeek V4 Flash на Yandex AI Studio; fallback — вторая модель там же (см. §3). Если когда-нибудь понадобится иностранная модель — сначала §7 «Трансграничка», потом код (`ALLOW_TRANSBORDER_LLM=true`).
- [ ] **Форма юрлица:** ИП на УСН 6 % (быстро, 3 дня, 0 ₽) или сразу ООО (нужно для IT-аккредитации, инвесторов, госзаказа, защиты личных активов). Рекомендация из юр-приложения: для SaaS с чужими ПДн ООО оправдано раньше, чем по выручке, но стартовать можно с ИП.
- [ ] **Домен и почта оператора:** домен, на котором будет продакшен, и ящик `pdn@<домен>` для запросов субъектов. Домен фигурирует в РКН-уведомлении, политике и футере.

## 1. Юрлицо и банк (1–2 недели, ~0–15 тыс. ₽)

- [ ] Зарегистрировать ИП/ООО через Госуслуги / банк. ОКВЭД основной: 62.01 (разработка ПО); дополнительные 62.09, 63.11.
- [ ] УСН «доходы» 6 % (заявление при регистрации). При ООО — сразу оценить АУСН, если регион поддерживает.
- [ ] Расчётный счёт (Т-Бизнес / Точка / Сбер). Сразу спросить у банка про витрину сервисов для МСП — это канал дистрибуции (§9).
- [ ] ЭДО (Диадок или СБИС) — понадобится для B2B-клиентов по счёту и для агентств.
- [ ] Записать реквизиты: полное наименование, ИНН, юр. адрес. Они уйдут в `LEGAL_ENTITY_NAME`, `LEGAL_INN`, `LEGAL_ADDRESS` и в `NEXT_PUBLIC_LEGAL_*` (см. `docs/runbook-launch-readiness.md §6.2`).

## 2. Юрист: разовый ревью текстов (1 неделя, 30–80 тыс. ₽)

Отдать IT-юристу пакет и вопросы. Тексты v1.1 уже лежат в репозитории: `services/frontend/content/legal/{privacy,consent,terms}.ru.md`.

- [ ] **Проверить тексты v1.1** на соответствие 152-ФЗ в редакции 2026 года. Ключевые места: раздел 7 Соглашения (поручение обработки ПДн клиентов бизнеса), раздел 6 (ИИ-контент и ответственность), формулировка «трансграничная передача не осуществляется».
- [ ] **Спросить про согласие как условие услуги.** Регистрация требует три галочки (соглашение, политика, согласие на ПДн). Email и имя нужны для договора — по ст. 6 ч. 1 п. 5 согласие на них не требуется. Юрист должен сказать, оставить ли отдельное согласие обязательным или сделать его информирующим, чтобы не нарушать запрет отказа в услуге.
- [ ] **Оферта на платные тарифы** (цены зафиксированы: Free / Pro 3 990–4 990 ₽ за локацию / Enterprise от 15 000 ₽; годовой price-lock для всех платящих бета-клиентов). Нужна до первого платежа. Убрать из terms фразу «сервис предоставляется бесплатно» в тот же релиз.
- [ ] **Оговорка про Яндекс Бизнес** (раздел 8 Соглашения): автоматизация действий от имени пользователя нарушает пользовательское соглашение Яндекса → риск блокировки кабинета. Юрист подтверждает формулировку раскрытия.
- [ ] **Договор для агентств / B2B** (подписной, с актами) — можно позже, к первому агентству.

После ревью: внести правки в `.ru.md` и `.en.md` **одновременно**, поднять версию в `pkg/legalconfig/versions.go` и `services/frontend/lib/legal/versions.ts` (скрипт `scripts/check-legal-versions-parity.sh` не даст разойтись). Бамп версии заставит всех пользователей переподписать тексты при входе.

## 3. Yandex Cloud: аккаунт, модели, договор (2–3 дня)

Аккаунт и сервисный аккаунт `onevoice-llm` уже созданы (папка `onevoice`, ключ в `.env`), DeepSeek V4 Flash проверен живьём 21.08.

- [ ] **Договор и ПДн.** В личном кабинете Yandex Cloud принять договор-оферту и найти документ об обработке персональных данных (Yandex Cloud выступает обработчиком по ст. 6 ч. 3). Сохранить PDF в папку оператора — РКН может запросить.
- [ ] **Письменный запрос в поддержку Yandex Cloud:** «Используются ли содержимое запросов к моделям Yandex AI Studio для обучения моделей? Подтвердите, что при заголовке `x-data-logging-enabled: false` данные запросов не сохраняются». Ответ сохранить. (Код уже шлёт этот заголовок на каждый запрос.)
- [ ] **Fallback-модель в РФ.** Провижинить вторую модель в той же папке (Qwen3-235B или gpt-oss-120b — обе есть в AI Studio) и записать её в `SELF_HOSTED_1_URL/_MODEL/_API_KEY` на прод-хосте. Проверить в консоли, что function calling у неё работает на наших тулах (как делали для DeepSeek). Без этого fallback = нет, и при сбое DeepSeek чат просто ошибётся — приемлемо для беты, но не для платных клиентов.
- [ ] **Убедиться, что в прод-окружении НЕТ ключей** `OPENROUTER_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY` и не выставлен `ALLOW_TRANSBORDER_LLM`. Иначе api и orchestrator откажутся стартовать в production — это специально.
- [ ] **KMS-ключ и бакет** для шифрования токенов и бэкапов существуют; сервисный аккаунт бэкапов имеет `kms.keys.decrypt` + `storage.editor` (см. `docs/runbook-launch-readiness.md §2`).
- [ ] Заявка на **Yandex Cloud Boost / стартап-гранты** на облако — бесплатные кредиты покрывают инфра-базлайн (~13 тыс. ₽/мес) на первые месяцы.

## 4. РКН: уведомление оператора (подать за 30+ дней до запуска)

Полный порядок — `docs/runbook-rkn-filing.md`. Кратко:

- [ ] Ящик `pdn@<домен>` живой, проверен письмом с чужого адреса.
- [ ] Учётная запись оператора на `pd.rkn.gov.ru` (по ИНН, активация ~1 день).
- [ ] Подать «Уведомление об обработке ПДн» (ст. 22): категории и цели — дословно из `privacy.ru.md §3–4`; обработчики — только Yandex Cloud и Unisender (РФ); трансграничная передача — «не осуществляется».
- [ ] **Уведомление по ст. 12 (трансграничная) НЕ подавать** — оно нужно только при иностранном LLM-провайдере.
- [ ] Через ~30 дней проверить запись в реестре по ИНН, сохранить PDF.
- [ ] Прочитать `docs/runbook-pdn-request.md` (15 рабочих дней на запрос субъекта) и `docs/runbook-pdn-incident.md` (24 ч / 72 ч при утечке). Оба становятся обязательными с момента появления записи в реестре.

## 5. Почта: Unisender (1 день)

- [ ] Аккаунт Unisender Go, верифицировать домен-отправитель (SPF/DKIM/DMARC — `docs/runbook-email-dns.md`).
- [ ] `UNISENDER_API_KEY` в прод-окружение. Пустой ключ = письма о подтверждении email, сбросе пароля и удалении аккаунта **молча не отправляются**.
- [ ] Принять условия обработки ПДн Unisender (обработчик, РФ) — сохранить в папку оператора.

## 6. Площадки: боты и приложения (1–2 дня)

- [ ] **Telegram:** боевой бот через @BotFather → `TELEGRAM_BOT_TOKEN`; `TELEGRAM_APPROVAL_HMAC_SECRET` одинаковый на `api` и `agent-telegram`. Помнить: Telegram замедлён РКН с февраля 2026 — уведомления владельцу дублируются в email, HITL-ссылки должны открываться и из веба.
- [ ] **VK:** приложение VK ID → `VK_CLIENT_ID`, `VK_CLIENT_SECRET`, `VK_REDIRECT_URI`; в настройках приложения оба redirect: `/oauth/vk/callback` и `/oauth/vk/community-callback`. Без этого фронт откатывается на вставку community-токена (работает, но хуже).
- [ ] **Яндекс Бизнес (режим представителя):** отдельный Яндекс-аккаунт представителя с **TOTP** (не SMS); `YANDEX_REP_LOGIN` + `YANDEX_SHARED_BUSINESS_ID` одинаковые на `api` и `agent-yandex-business`; засеять сессию через `POST /internal/v1/yandex/shared-session` по mTLS. `SCOPE_GATE_ENFORCE` оставить выключенным до обкатки.
- [ ] **MAX (новый канал, продуктовое решение):** зарегистрировать бизнес-аккаунт и бота в MAX заранее — агент в бэклоге как ставка №1 после того, как MAX обогнал Telegram по аудитории в РФ.
- [ ] **Yandex SmartCaptcha:** `SMARTCAPTCHA_SITE_KEY`, `SMARTCAPTCHA_SECRET_KEY`, `NEXT_PUBLIC_SMARTCAPTCHA_SITE_KEY` — иначе мягкий тир защиты от перебора паролей молча выключен.

## 7. Трансграничка — только если решите использовать иностранную модель

По умолчанию не нужна. Если решение изменится:

- [ ] Сначала юрист правит `consent.ru.md`/`privacy.ru.md` (получатели, цели, основание), бамп версии → переподписание.
- [ ] Подать отдельное «Уведомление о трансграничной передаче» на `pd.rkn.gov.ru` за 30+ дней; США вне списка «безопасных» стран — РКН может запретить.
- [ ] Только после этого `ALLOW_TRANSBORDER_LLM=true` в проде. Помнить: этот флаг заодно выключает редакцию ПДн перед моделью.

## 8. Платежи (после юрлица, перед первым платящим клиентом)

- [ ] Эквайринг с рекуррентом: ЮKassa (выбрана) или CloudPayments. Подключение 3–7 дней, нужны реквизиты и сайт с офертой и политикой.
- [ ] **Касса по 54-ФЗ** для оплат картой от физлиц/ИП — обычно облачная касса пакетом с эквайрингом. Для юрлиц по счёту касса не нужна: счёт + акт/УПД через ЭДО.
- [ ] Опубликовать оферту с тарифами (§2), обновить terms (`v1.2`), бамп версии.
- [ ] Включить биллинг в продукте (план ЮKassa-чекаута ждал именно юрлицо и 54-ФЗ).

## 9. Дистрибуция и статусы (после запуска, по мере сил)

- [ ] **Витрины для МСП:** заявки в маркетплейсы сервисов банков (Точка, Т-Бизнес, СберБизнес), «Цифровая платформа МСП», Bitrix24 Marketplace. Нужны: оферта, политика, лендинг, реквизиты.
- [ ] **Реестр российского ПО** (Минцифры): ради доверия и госзаказа, не ради НДС (льгота для SaaS-как-услуги спорна и не покрывает «рекламное» ПО). 1–3 месяца, пошлины нет.
- [ ] **IT-аккредитация** (только ООО): страховые взносы 7,6 %, налог на прибыль 0 % — когда появится ФОТ.
- [ ] **Маркировка своей рекламы** (erid через ОРД, отчётность в ЕРИР, рекламный сбор 3 %) — в момент старта платного продвижения. Органический контент на своих каналах не маркируется.
- [ ] **Оговорка про Meta** при любом упоминании Instagram в материалах; платное продвижение там запрещено (72-ФЗ).

## 10. Прод-хост и деплой (1–2 дня, после §3–6)

Полный порядок — `docs/deployment.md` + `docs/runbook-launch-readiness.md`. Ручная часть:

- [ ] VM в Yandex Cloud (ru-central1), security group, DNS на домен, TLS (`scripts/init-letsencrypt.sh`).
- [ ] Секреты: `JWT_SECRET`, `ENCRYPTION_KEY`, `POSTGRES_PASSWORD`, `MONGO_ROOT_USERNAME/PASSWORD`, `REDIS_PASSWORD`, `MINIO_ROOT_USER/PASSWORD`, `ORCHESTRATOR_INTERNAL_SECRET`, `A2A_PAYLOAD_KEY` (32 байта, одинаковый на api и agent-yandex-business), `TELEGRAM_APPROVAL_HMAC_SECRET`. Генерировать `openssl rand -base64 32`, хранить в менеджере секретов, не в git.
- [ ] `make mtls-certs` (или прод-CA по `infra/mtls/README.md`) и `make nats-creds` — nkey-сиды сервисов и ACL шины (`infra/nats/README.md`).
- [ ] `APP_ENV=production` — включает все fail-closed проверки (LEGAL\_\*, ORCHESTRATOR_INTERNAL_SECRET, Redis, RF-only LLM).
- [ ] `LEGAL_*` и `NEXT_PUBLIC_LEGAL_*` = реальные реквизиты из §1, дословно как в РКН-записи.
- [ ] Бэкапы: контейнер бэкапов с доступом к KMS и бакету; провести восстановление по `docs/runbook-restore.md` один раз до запуска.
- [ ] Наблюдаемость: Grafana-дашборды (в т. ч. «Product / North-Star»), алерты — `docs/runbook/observability.md`.
- [ ] Пройти чеклист `docs/runbook-launch-readiness.md` целиком и поставить дату.

## 11. Что держать под рукой после запуска

- Папка оператора: договоры с Yandex Cloud и Unisender, ответ Яндекса про обучение/логирование, PDF записи РКН, ревью юриста, журнал ручных действий (`docs/manual-audit-log.md` вне репозитория).
- Календарь: 15 рабочих дней на запрос субъекта; 24 ч / 72 ч при инциденте; 10 рабочих дней на уведомление РКН об изменениях (смена юрлица, нового обработчика).
- Законопроект о маркировке ИИ-контента (внесён 17.08.2026, планируемое вступление 01.03.2027): когда примут — включить флаг ИИ-контента в продукте и обновить раздел 6.5 Соглашения.

## Гибридный вход на лендинге

- [ ] Проверить реальные реквизиты юрлица и `UNISENDER_API_KEY` в защищённой конфигурации.
- [ ] Начать с `LANDING_ENTRY_MODE=hybrid`; квота — не более 10 новых организаций в неделю.
- [ ] Читать `waitlist` и `channel_votes`, согласовывать подключение каналов с организациями.
- [ ] Пересмотр через 3 недели или на 20-й регистрации: `activation_rate_7d ≥ 35%` → `open`,
      `< 15%` → `waitlist_only`; между порогами сохранить `hybrid`.
      RPM-01 должен быть исправлен и проверен до использования этой метрики.
- [ ] Стоп-краны: более 25 активных Free, более 600 ₽ на организацию в месяц,
      более 90 минут поддержки в день два дня подряд.
- [ ] При стоп-кране rollback: `LANDING_ENTRY_MODE=waitlist_only` и перезапуск frontend
      с обновлённым окружением без rebuild. Для Compose изменение `.env` требует
      пересоздания frontend с прежним образом: обычный `restart` сохраняет старое
      окружение. Порядок и проверка — [frontend config](frontend-config.md).
      Проверить отсутствие `/register` на лендинге; существующие аккаунты сохраняются.

## Canonical-email collisions before upgrade

Before PostgreSQL migration `000037_users_email_canonical` (API test-copy
`000036_users_email_canonical`), run the read-only report from the release checkout:

```sh
docker compose exec -T postgres psql -X -U postgres -d onevoice \
  < scripts/report-email-collisions.sql
```

The report includes **all accounts**, including accounts pending deletion, grouped
by `lower(btrim(email))`, exactly as the migration does. `business_count` counts
memberships, including suspended memberships and pending-deletion organizations.
`last_login` is the latest retained `auth.login_success` audit event; NULL means
unknown, not proof that the account was never used. Restrict report access as it
contains account identifiers and email addresses; never commit production output.

1. Take a verified backup and pause account writes through the deployment
   maintenance procedure. Run the report before applying migrations. A nonempty
   report blocks deployment; do not bypass or remove the migration collision guard.
2. For each group, identify the legitimate mailbox holder through independent
   identity verification and review memberships, ownership, billing and retained
   audit history. The mailbox holder's approved account keeps the address.
   Creation date, recent login, or organization count alone must never decide
   ownership. If the same person owns both accounts, they explicitly select the
   account to keep. If ownership is disputed, leave the group unchanged and keep
   deployment blocked. Never rename or delete `demo-owner@onevoice.local`.
3. Obtain approval from the other account's holder for a distinct replacement
   address they control. Record operator identity, approval reference, keeper and
   renamed UUIDs, old and new addresses, reason, and membership review in the
   restricted change record. No memberships, organizations, credentials, or OAuth
   identities are merged or transferred by this procedure.
4. In an interactive `psql -X -v ON_ERROR_STOP=1` session, set these variables to
   the reviewed values. Do not use example UUIDs. Execute the transaction below
   for **one approved account at a time**. The operator must be an existing user
   so the audit foreign key resolves. The target address must be unique after
   canonicalization; the account must subsequently verify its replacement email.
   Invalidate all outstanding password-reset and email-verification links in the
   same transaction, so the old mailbox cannot control the renamed account.

```sql
\prompt 'Keeper user UUID: ' keeper_id
\prompt 'Account to rename UUID: ' renamed_id
\prompt 'Exact existing address of renamed account: ' old_email
\prompt 'Approved replacement address: ' new_email
\prompt 'Operator user UUID: ' operator_id
\prompt 'Approval reference and reason: ' reason
BEGIN;
LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE;
WITH changed AS (
    UPDATE users u
    SET email = lower(btrim(:'new_email')),
        email_verified = false, email_verified_at = NULL, updated_at = now()
    WHERE u.id = :'renamed_id'::uuid
      AND u.email = :'old_email'
      AND u.id <> :'keeper_id'::uuid
      AND lower(btrim(u.email)) <> 'demo-owner@onevoice.local'
      AND lower(btrim(:'new_email')) <> 'demo-owner@onevoice.local'
      AND btrim(:'new_email') <> ''
      AND btrim(:'reason') <> ''
      AND EXISTS (
          SELECT 1 FROM users k WHERE k.id = :'keeper_id'::uuid
          AND lower(btrim(k.email)) = lower(btrim(u.email))
      )
      AND NOT EXISTS (
          SELECT 1 FROM users n
          WHERE lower(btrim(n.email)) = lower(btrim(:'new_email'))
      )
    RETURNING u.id, u.email
), reset_tokens_invalidated AS (
    UPDATE password_reset_tokens SET consumed_at = now()
    WHERE user_id IN (SELECT id FROM changed) AND consumed_at IS NULL
), verification_tokens_invalidated AS (
    UPDATE email_verification_tokens SET consumed_at = now()
    WHERE user_id IN (SELECT id FROM changed) AND consumed_at IS NULL
), audited AS (
    INSERT INTO audit_logs (user_id, action, resource, details, user_email_at_event)
    SELECT :'operator_id'::uuid, 'admin.email_collision_reconciled',
           'users/' || c.id::text,
           jsonb_build_object('keeper_id', :'keeper_id', 'renamed_id', c.id,
                              'old_email', :'old_email', 'new_email', c.email,
                              'reason', :'reason'),
           (SELECT email FROM users WHERE id = :'operator_id'::uuid)
    FROM changed c
    RETURNING id
)
SELECT count(*) = 1 AS reconciliation_ok FROM audited \gset
\if :reconciliation_ok
    COMMIT;
\else
    ROLLBACK;
    \echo 'No account changed. Recheck identifiers, collision and replacement address.'
\endif
```

Any SQL error requires `ROLLBACK` before retrying. An audit failure rolls back the
rename with it. Retain the audit event ID from the restricted operator review.
Re-run the report after each transaction. If the other account is to be deleted,
first resolve its address as above, then use the existing consent-based account
removal workflow, including ownership checks and grace period. Soft deletion
alone does **not** resolve a collision; do not directly delete a users row or
bypass ownership checks. No account is automatically renamed, merged or deleted.

5. Once the report has zero rows, apply the normal migrations with writes still
   paused. The migration locks and checks again, so a concurrent collision fails
   closed. If the migration was already marked dirty by a failed attempt, inspect
   the schema and migration transaction outcome before following the migration
   tool's recovery procedure; never force the failed version as successfully
   applied. Confirm the unique index exists and retrying the report returns zero
   rows before reopening writes. Notify affected holders through their verified
   contact channels and complete replacement-email verification.

The report regression runner `scripts/test-email-collisions.py` uses only CTE
fixtures inside a read-only transaction. It checks case/space collisions,
soft-deleted accounts, audit-derived last login, membership counts, and the
post-reconciliation zero-collision condition without changing stored accounts.
