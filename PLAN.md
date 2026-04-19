# Tasks UX Revamp — План

**Ветка:** `feat/tasks-ux` (worktree `.worktrees/tasks-ux`)
**Основа:** `main` (688b9a4)

## 1. Что ломаем

Страница `/tasks`:
1. Задачи появляются в списке **только после завершения** (нет `running`/`pending`).
2. Колонка **Длительность** всегда `0ms` (`StartedAt == CompletedAt`).
3. В колонке **Тип** — сырые имена инструментов (`telegram__send_channel_post`, `sync_photo`) вместо человеко-читаемых названий.

## 2. Архитектурное решение

### Realtime через SSE
Оставляем SSE как механизм доставки.
- Оркестратор уже стримит `tool_call` / `tool_result` в чате. Переиспользуем эту точку.
- `services/api/internal/handler/chat_proxy.go` (проксирует orchestrator SSE) перехватывает события и:
  - на `tool_call` — создаёт `AgentTask` со статусом `running`, `StartedAt=now`, `CompletedAt=nil`; публикует событие в Task Hub.
  - на `tool_result` — обновляет таск: `status=done|error`, `CompletedAt=now`; публикует обновление в Task Hub.
- Новый endpoint `GET /api/tasks/stream` (SSE): подписка на события текущего бизнеса пользователя. Фронт `/tasks` открывает поток при монтировании.
- `services/api/internal/platform/sync.go` (фоновая синхронизация) — измерять реальное время операции (`t0 := time.Now()` → `StartedAt: &t0, CompletedAt: &now`).

### DisplayName в бэке
- Новое поле `DisplayName string` в `domain.AgentTask` (`pkg/domain/mongo_models.go`).
- Маппер `pkg/domain/toolnames.go` или `pkg/tools/display.go`:
  ```go
  func DisplayName(tool string) string // "telegram__send_channel_post" → "Отправить пост"
  ```
- Заполняем при `Create` и `Update` в `chat_proxy.go` и `sync.go`.
- Fallback: если маппинга нет — возвращаем человекочитаемую версию тулзы (`send_channel_post` → `Send channel post` через `strings.ReplaceAll(..., "_", " ") + Title`).
- Фронт рендерит `task.displayName`.

## 3. Этапы реализации

### Этап 1. Backend — domain + repository
- [ ] `pkg/domain/mongo_models.go` — добавить поле `DisplayName string \`bson:"display_name,omitempty" json:"displayName,omitempty"\`` в `AgentTask`.
- [ ] `pkg/domain/repository.go` — добавить в `AgentTaskRepository`:
  ```go
  Update(ctx context.Context, task *AgentTask) error
  GetByID(ctx context.Context, businessID, taskID string) (*AgentTask, error) // опц., для reconnect/SSE
  ```
- [ ] `services/api/internal/repository/agent_task.go` — реализовать `Update` (Mongo `FindOneAndUpdate` по `_id + business_id`, `$set` обновляемых полей).

### Этап 2. Backend — display name mapper
- [ ] `pkg/tools/display.go` — функция `DisplayName(tool string) string` + таблица переводов:
  - `telegram__send_channel_post` → "Отправить пост"
  - `telegram__send_channel_photo` → "Отправить фото"
  - `telegram__send_notification` → "Уведомление"
  - `telegram__get_reviews` → "Получить отзывы"
  - `vk__publish_post` → "Опубликовать пост"
  - `vk__get_wall_posts` → "Загрузить посты"
  - `vk__get_comments` → "Загрузить комментарии"
  - `yandex_business__get_reviews` → "Получить отзывы Яндекса"
  - `sync_title` / `sync_description` / `sync_photo` / `sync_info` → "Синхронизация ..."
  - `publish_post` → "Опубликовать пост" (для sync-recorder имён)
- [ ] Unit-тест `pkg/tools/display_test.go` — table-driven для всех известных имён + fallback.

### Этап 3. Backend — chat_proxy рефакторинг
Текущее: создание таска в `chat_proxy.go:276-326` после цикла SSE.

Новое:
- [ ] При парсинге SSE события `tool_call`:
  - Извлечь `tool_call_id`, `name`, `args` из payload.
  - Создать `AgentTask{Status: "running", StartedAt: &now, DisplayName: tools.DisplayName(name)}`.
  - Сохранить `taskID` в map `toolCallID → taskID` (внутри handler scope).
  - Опубликовать `TaskEvent{Kind: "task.created", Task: task}` в `TaskHub`.
- [ ] При парсинге SSE события `tool_result`:
  - По `tool_call_id` найти `taskID` в map.
  - `Update(task: {Status: done|error, CompletedAt: &now, Output, Error})`.
  - Опубликовать `TaskEvent{Kind: "task.updated", Task: task}`.
- [ ] Убрать финальный цикл создания тасков по `toolResults` (строки 276-326).
- [ ] Edge case: `tool_result` без предшествующего `tool_call` (reconnect и т.п.) — логируем WARN, таск не создаём.

**Предусловие (проверено):** `tool_call_id` сейчас в SSE **не передаётся**. Источник есть — `llm.ToolCall.ID` доступен в `orchestrator.go:132` (`tc.ID`), но теряется. Нужен Этап 3a ниже.

### Этап 3a. Orchestrator — пробросить tool_call_id в SSE (предварительно к Этапу 3)
- [ ] `services/orchestrator/internal/orchestrator/orchestrator.go`:
  - В `Event` struct (строки 30-37) добавить `ToolCallID string`.
  - На emit `EventToolCall` (строка 142) и `EventToolResult` (строки 166-170) заполнять `ToolCallID: tc.ID`.
- [ ] `services/orchestrator/internal/handler/chat.go`:
  - В `sseEvent` struct (строки 55-62) добавить `ToolCallID string \`json:"tool_call_id,omitempty"\``.
  - В цикле по событиям (строки 127-139) копировать `sse.ToolCallID = event.ToolCallID` для `EventToolCall`/`EventToolResult`.
- [ ] Обновить тесты `orchestrator_test.go`, `e2e_test.go`, `concurrent_test.go` — проверить, что `ToolCallID` присутствует и одинаков в парах call/result.
- Обратная совместимость: поле с `omitempty`, старые потребители не ломаются.

### Этап 4. Backend — TaskHub (pub/sub) + SSE endpoint
- [ ] `services/api/internal/taskhub/hub.go` — in-process hub:
  ```go
  type Event struct {
    Kind string       // "task.created" | "task.updated"
    Task domain.AgentTask
  }
  type Hub struct { /* subscribers map[businessID][]chan Event */ }
  func (h *Hub) Subscribe(businessID string) (ch <-chan Event, unsub func())
  func (h *Hub) Publish(businessID string, ev Event)
  ```
  — буферизованные каналы (64), неблокирующая публикация (drop при переполнении + лог).
- [ ] `services/api/internal/handler/agent_task.go` — новый метод `StreamTasks(w, r)`:
  - Auth через middleware → получить `businessID`.
  - Установить SSE-заголовки (`Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`).
  - Подписаться в hub, пушить `data: <json>\n\n` + `Flush()`.
  - Heartbeat каждые 20s (`: ping\n\n`).
  - Отписаться при `ctx.Done()`.
- [ ] `services/api/internal/router/router.go:129` — `r.Get("/tasks/stream", handlers.AgentTask.StreamTasks)`.
- [ ] Прокинуть `TaskHub` через `service.AgentTaskService` или напрямую в handler + в `chat_proxy.go` + в `platform.Syncer`.

### Этап 5. Backend — sync.go duration fix
- [ ] `services/api/internal/platform/sync.go:recordTask` — параметр `startedAt time.Time` вместо вычисления внутри.
- [ ] Все вызовы `s.recordTask(...)` (строки 104-126 и аналоги): запомнить `start := time.Now()` до операции, передать.
- [ ] Заполнять `DisplayName` из `tools.DisplayName`.
- [ ] Публиковать `task.created` / `task.updated` в `TaskHub` для фоновых sync-задач.

### Этап 6. Frontend — SSE client + store update
- [ ] `services/frontend/app/(app)/tasks/page.tsx`:
  - Подписка на `/api/tasks/stream` через `EventSource` в `useEffect` (cleanup на unmount).
  - На `task.created` — `queryClient.setQueryData(['tasks', ...], prev => prepend(prev, task))`.
  - На `task.updated` — заменить по `task.id` в списке.
  - Retry при `onerror` — `EventSource` делает авто-reconnect, но держим флаг состояния для UI.
- [ ] Fallback: `useQuery({ refetchInterval: 30_000 })` на случай потери SSE.

### Этап 7. Frontend — rendering
- [ ] `services/frontend/types/task.ts` — добавить `displayName?: string`.
- [ ] `services/frontend/app/(app)/tasks/page.tsx:255` — `{task.displayName || task.type}`.
- [ ] Статусы-бейджи: добавить вариант `running` (жёлтый/синий пульсирующий индикатор) — сейчас в коде только `done` (Завершено) и `error` (Ошибка).
- [ ] Локализация статусов — `"running"` → "В процессе".
- [ ] Длительность для running-тасков: показывать «идёт N с» с тиканьем каждую секунду (опц., если не усложнит — иначе просто «—»).

### Этап 8. Обратная совместимость
- [ ] Существующие таски в Mongo без `display_name` → в `ListByBusinessID` на чтение делать backfill через `tools.DisplayName(task.Type)` перед возвратом (без записи, чтобы не блокировать поиск). Или: миграционный скрипт `scripts/backfill_task_display_name.go` с `UpdateMany` — решаем при реализации.

### Этап 9. Тесты
- [ ] `pkg/tools/display_test.go` (unit).
- [ ] `services/api/internal/repository/agent_task_test.go` — `Update` (integration с Mongo test container).
- [ ] `services/api/internal/taskhub/hub_test.go` — pub/sub, backpressure drop, multi-subscriber.
- [ ] `services/api/internal/handler/chat_proxy_test.go` — добавить сценарий tool_call → tool_result, проверить 2 hub-события и `Create+Update` на моке репо.
- [ ] E2E smoke (`test/integration/`): запустить чат с тулзой → убедиться, что через `/tasks/stream` приходит `created(running)` → `updated(done)`.

## 4. Open Questions

| # | Вопрос | Зачем | Решение |
|---|--------|-------|---------|
| ~~Q1~~ | ~~В SSE `tool_call` уже передаётся уникальный `tool_call_id`?~~ | — | **Закрыт:** не передаётся. Источник `llm.ToolCall.ID` есть. Решено Этапом 3a. |
| Q2 | У фронта несколько одновременных чатов / бизнесов? | SSE hub scope | Фильтруем по `businessID` активного бизнеса; при смене бизнеса — переподключаем EventSource. |
| Q3 | Backfill display_name для старых задач? | Качество UX на странице истории | MVP: backfill на лету при GET (без записи). Миграционный скрипт — опционально позже. |
| Q4 | Running-длительность: тикающий таймер или статичный «—»? | UX живости | MVP: статичный «—». Таймер — следующая итерация. |
| Q5 | `sync_*` задачи — им тоже нужен realtime? | Consistency | Да, но публикация из `sync.go` — через общий `TaskHub`. |

## 5. Затронутые файлы

**Создать:**
- `pkg/tools/display.go`
- `pkg/tools/display_test.go`
- `services/api/internal/taskhub/hub.go`
- `services/api/internal/taskhub/hub_test.go`

**Изменить:**
- `pkg/domain/mongo_models.go` — `DisplayName`
- `pkg/domain/repository.go` — `Update`, `GetByID`
- `services/api/internal/repository/agent_task.go` — реализация `Update` + `GetByID` + backfill display_name в `ListByBusinessID`
- `services/api/internal/handler/chat_proxy.go` — realtime create/update по SSE событиям (строки 276-326 переписать)
- `services/api/internal/handler/agent_task.go` — `StreamTasks`
- `services/api/internal/platform/sync.go` — startedAt реальный + displayName + publish в hub
- `services/api/internal/router/router.go` — `/tasks/stream`
- `services/api/internal/service/agent_task.go` — прокинуть hub (если нужно)
- `services/api/cmd/main.go` — wiring `TaskHub`
- `services/frontend/types/task.ts` — `displayName?`
- `services/frontend/app/(app)/tasks/page.tsx` — EventSource, rendering, running badge
- `services/orchestrator/internal/orchestrator/orchestrator.go` — `ToolCallID` в `Event` + заполнение на emit call/result
- `services/orchestrator/internal/handler/chat.go` — `tool_call_id` в `sseEvent` + копирование в цикле

## 6. Риски

- **Race tool_call/tool_result** — если `tool_result` пришёл до того, как `Create` успел закоммитить в Mongo: либо сделать `Update` как upsert с условием, либо сериализовать обработку событий одного stream (он и так последовательный — должно быть ОК).
- **SSE back-pressure** — медленный клиент забивает канал. Решение: неблокирующая публикация с drop + heartbeat + клиент-рефетч через REST при отставании.
- **Множественность API инстансов** (если горизонтально масштабируется) — in-process hub не увидит события других инстансов. Решение: если это реальность — мигрировать на NATS pub/sub (`events.tasks.{businessID}`). Для MVP — in-process.
- **Старые `tool_call` без id** — обрабатывать как сейчас (создавать по агрегации `toolResults`), либо пропускать realtime для них и показывать только после завершения (деградация, не регрессия).

## 7. Definition of Done

- Открываю `/tasks` в одной вкладке, пишу в чате запрос с тулзой в другой → вижу, как задача появляется в списке со статусом «В процессе», затем переходит в «Завершено» без F5.
- Колонка «Длительность» показывает осмысленное значение (мс/сек).
- В колонке «Тип» — «Отправить пост» вместо `telegram__send_channel_post`.
- Существующие старые задачи рендерятся с backfill-названием (не ломаются).
- `make lint-all` и `make test-all` зелёные.
