# Phase 19: Search & Sidebar Redesign - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-27
**Phase:** 19-search-sidebar-redesign
**Areas discussed:** Pin/Unpin UX и семантика, Layout и анатомия результата поиска, Search scope/шорткаты/навигация, Master/detail desktop и мобильный drawer

---

## Pin/Unpin UX и семантика

### Где разместить affordance «Закрепить»?

| Option | Description | Selected |
|--------|-------------|----------|
| Только в sidebar context-menu | Минимальная поверхность, как D-12 Phase 18 для «Обновить заголовок» | |
| Sidebar context-menu + кнопка в ChatHeader | Дублируем в chat header иконкой-булавкой; flicker-риск как Phase 18 D-11 | ✓ |
| Hover/swipe-affordance на самой строке | Самый быстрый жест, но ломает Phase 15 паттерн (всё в kebab) и плохо на mobile | |

**User's choice:** Sidebar context-menu + кнопка в ChatHeader
**Notes:** ChatHeader subscribe через узкий мемоизированный селектор по `pinned` — структурная мера против flicker, как в Phase 18 D-11.

### Сортировка чатов внутри секции «Закреплённые»?

| Option | Description | Selected |
|--------|-------------|----------|
| По last_message_at desc | Поле уже есть (Phase 15 backfill), без миграции | |
| По pinned_at desc (новое поле) | Новое поле, миграция, «prison-style» pin | ✓ |
| По title алфавитно | Стабильный порядок но плохо при auto_pending titles | |

**User's choice:** По pinned_at desc (новое поле)
**Notes:** Добавить `pinned_at: time.Time | null`. Идемпотентный backfill `pinned_at: null` для существующих conversation. Schema-cleanup `Pinned bool` оставлен на planner.

### Что показывать когда закреплённых чатов нет?

| Option | Description | Selected |
|--------|-------------|----------|
| Секция скрыта полностью | Чистый UI; пин из контекст-меню любого чата | ✓ |
| Header «Закреплённые» виден всегда | Discoverability, но visual noise | |

**User's choice:** Секция скрыта полностью
**Notes:** —

### UI-03: индикатор принадлежности проекту в верхней секции «Закреплённые»?

| Option | Description | Selected |
|--------|-------------|----------|
| Мини-чип с именем проекта справа от title | Reuse `ProjectChip.tsx` Phase 15 | ✓ |
| Цветная точка слева от title | Требует hash→color на проект, плохо различимо при 1-3 проектах | |
| Только tooltip на hover | Невидимо без hover, не работает на mobile | |

**User's choice:** Мини-чип с именем проекта справа от title
**Notes:** Для чатов в «Без проекта» чип не рисуется.

---

## Layout и анатомия результата поиска

### Где и как появляются результаты поиска?

| Option | Description | Selected |
|--------|-------------|----------|
| Inline dropdown прямо под input в sidebar | Совпадает с UI-06; Radix Combobox/Popover | ✓ |
| Full-width overlay поверх sidebar | Больше места, но отход от UI-06 | |
| Отдельная страница /search | Полные фильтры, но рвёт master/detail | |

**User's choice:** Inline dropdown прямо под input в sidebar
**Notes:** —

### Что показывать в одной строке результата при нескольких матчах в чате?

| Option | Description | Selected |
|--------|-------------|----------|
| Одна строка, snippet из top-scored сообщения | Просто, но не показывает фрекенцию | |
| Одна строка + count «+N совпадений» | Slack/Linear UX, badge рядом с title | ✓ |
| Несколько строк на чат | Противоречит SEARCH-03 «aggregated by conversation» | |

**User's choice:** Одна строка + count «+N совпадений»
**Notes:** Repo aggregation pipeline в Mongo: title + top_scored_snippet + match_count + score + project_name + last_message_id.

### Подсветка при клике на результат?

| Option | Description | Selected |
|--------|-------------|----------|
| Бриф flash 1.5–2s + ?highlight=msgId в URL | URL-based, refresh/saved-link работают | ✓ |
| Persistent highlight пока юзер не прокрутит/кликнет | Навязчивее, требует детект взаимодействия | |
| Подсветка отдельных слов (mark) внутри сообщения | Точнее, но требует stem-aware подсветки на фронте | |

**User's choice:** Бриф flash 1.5–2s + ?highlight=msgId в URL
**Notes:** Page parses query param on mount, scrolls via ref, applies fade-out CSS class.

### Выделение match-слов в snippet?

| Option | Description | Selected |
|--------|-------------|----------|
| Без выделения — простой текст | Простота | |
| Bold точных вхождений query в snippet | Не работает при стемминге («запланировать»→«планам») | |
| Bold + stem-aware через backend и выдачу позиций | Snowball lib, [start, end] ranges; точно | ✓ |

**User's choice:** Bold + stem-aware через backend и выдачу позиций
**Notes:** Добавить Go snowball lib (`github.com/kljensen/snowball` или аналог); planner проверяет через Context7.

---

## Search scope, шорткаты и навигация

### Дефолтный scope при просмотре проекта?

| Option | Description | Selected |
|--------|-------------|----------|
| По всему бизнесу + checkbox «Только {проект}» | Не теряем чат если он в другом проекте | |
| Текущий проект + checkbox «По всему бизнесу» | Контекст-aware: ищу в том где нахожусь | ✓ |
| Всегда по бизнесу, без фильтра проекта | Игнорирует «(optionally) current project filter» | |

**User's choice:** Текущий проект + checkbox «По всему бизнесу»
**Notes:** При root /chat (без проекта в URL) дефолт = бизнес, чекбокса нет.

### Клавиатурный шорткат фокуса на поиск?

| Option | Description | Selected |
|--------|-------------|----------|
| Cmd/Ctrl-K фокусирует поиск | Стандарт индустрии (Slack, Linear) | ✓ |
| / (slash) фокусирует поиск | GitHub-style; конфликтует с русской раскладкой | |
| Без шорткатов — только клик | Регресс UX | |

**User's choice:** Cmd/Ctrl-K фокусирует поиск
**Notes:** Глобальный listener в `layout.tsx`. Esc — close + blur. Крадёт фокус из chat composer.

### Backend search query strategy (Message не имеет business_id)?

| Option | Description | Selected |
|--------|-------------|----------|
| Двухфазный: conversations → messages.conversation_id $in | Без миграции; на v1.3 scale fast enough (ARCHITECTURE §6.4) | ✓ |
| Денормализовать business_id на messages + бэкфилл | Однофазный query, но потенциальный backfill hot-spot | |
| Гибрид: business_id на новых + fallback two-step | Сложнее, две ветки, anti-pattern | |

**User's choice:** Двухфазный query: фильтр conversations по business_id, потом messages с conversation_id $in
**Notes:** Repo signature `Search(ctx, businessID, userID, query, projectID *string, limit) ([]SearchResult, error)`. Cap conv_id $in 1000.

### Edge case: пустой/короткий query и empty state?

| Option | Description | Selected |
|--------|-------------|----------|
| Min 2 символа, dropdown скрыт до этого | Адекватный баланс; <2 char Mongo $text всё равно мусор | ✓ |
| Min 3 символа | Конс., но «ДА»/«НЕТ» как query не работают | |
| Без min — с 1-го символа | $text не обрабатывает 1-char (stop-words), пустые впустую | |

**User's choice:** Min 2 символа, dropdown скрыт до этого
**Notes:** 250ms debounce. Spinner в input. Empty result: «Ничего не найдено по «{query}»».

---

## Master/detail desktop и мобильный drawer

### Десктоп sidebar: ширина и collapse-поведение?

| Option | Description | Selected |
|--------|-------------|----------|
| Фикс 280 + collapse кнопка («« → narrow rail) | Slack-style; localStorage persist | |
| Resizable handle (драг 200–480px) | VS Code-style; гибкий; новый dep | ✓ |
| Оставить 240 фикс, без collapse | Минимум работы; тесно для snippet | |

**User's choice:** Resizable handle (драг-ранжируемая граница 200–480px)
**Notes:** Добавить `react-resizable-panels` или custom hook + ResizeObserver. Persist width в localStorage.

### Поведение sidebar на не-chat страницах (`/integrations`, `/business` etc.)?

| Option | Description | Selected |
|--------|-------------|----------|
| Сохранить текущее: project-tree только в chat-area | Минимум изменений | |
| Всегда показывать sidebar с проектами+поиском | Поиск везде, но визуальный шум на не-chat | |
| Two-pane: nav-rail всегда + project-tree только в chat-area | Discord-style, big surgery | ✓ |

**User's choice:** Two-pane: nav-rail всегда, project-tree только в chat-area
**Notes:** Структурно крупнее остальных решений. Planner: рассмотреть отдельный plan для layout restructure.

### Mobile drawer: поведение при выборе чата?

| Option | Description | Selected |
|--------|-------------|----------|
| Авто-закрыть drawer + navigate к /chat/{id} | Стандарт mobile-чатов (Telegram, Slack) | ✓ |
| Drawer остаётся открытым, юзер закрывает вручную | Хорошо для quick-switch, но перекрывает ChatWindow | |
| Drawer персистентный split-screen | Не масштабируется на 360px экран | |

**User's choice:** Авто-закрыть drawer + navigate к /chat/{id}
**Notes:** Pin/expand-collapse/rename — drawer открыт. Только «открыть чат» закрывает.

### UI-05 «end-to-end keyboard navigation»: что нужно?

| Option | Description | Selected |
|--------|-------------|----------|
| axe-core/lighthouse audit + roving-tabindex в списке чатов | CI gate на critical/serious; PITFALLS §22 mitigated | ✓ |
| Только Radix-дефолты | Tab проходит через N чатов; риск fail audit | |
| Полный ручной keyboard mode + skip-links | Over-engineering для single-owner | |

**User's choice:** axe-core/lighthouse audit + roving-tabindex в списке чатов
**Notes:** Tab входит в список один раз; ↑/↓ переключают rows; Project headers Enter — expand/collapse. axe-core в vitest.

---

## Claude's Discretion

- Snowball lib alternative selection (`github.com/kljensen/snowball` vs `github.com/blevesearch/snowballstem`) — planner verifies via Context7.
- Resizable lib selection (`react-resizable-panels` vs custom hook) — planner picks based on bundle weight and a11y.
- Flash highlight CSS animation (color, exact duration 1.5 vs 2s, easing) — planner picks within range.
- Russian copy refinement: «Ничего не найдено», «По всему бизнесу», «+N совпадений».
- ProjectChip variant for pinned-section indicator (size prop or new component).
- Index readiness gate exact mechanism (boolean flag set after CreateIndexes vs feature flag in config).
- Snippet centering algorithm (first match vs longest match-run).
- React Query key scheme for search.
- Whether to extend Phase 18 D-08a compound index or create new for `pinned_at`.

## Deferred Ideas

- Time-range / role filters (SEARCH-L2 — v1.4)
- Saved searches (SEARCH-L3 — v1.4)
- Tool-call-arg search (SEARCH-L1 — v1.4, needs external engine)
- Drag-and-drop chat reorder (UI-L3 — v1.4)
- Per-project pinned (rejected — UI-03 locks global+project-duplicate)
- Custom Russian stemmer dictionary backend
- Search quality observability dashboards
- Cross-business / admin-mode search
- Additional shortcuts beyond Cmd/Ctrl-K
- Dedicated `/conversations/stream` SSE
