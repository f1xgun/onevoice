## MVP runbook: intro + ch1-1 + ch1-2

Цель: провести полный цикл research → factcheck → write для трёх первых задач:
- `intro`
- `ch1-1`
- `ch1-2`

Артефакты уже созданы в:
- `vkr/tasks/intro/`
- `vkr/tasks/ch1-1/`
- `vkr/tasks/ch1-2/`

### 0) Подготовка (MainEditor)

1. Проверить, что каркас существует:
   - `vkr/draft/toc.md`
   - `vkr/draft/draft.md`
   - `vkr/sources/sources.md`
2. Для каждого из трёх тикетов проставить в `status.md`:
   - `status: researching`
   - `owner: ResearchAgent_<k>`
   - `updated: YYYY-MM-DD`

### 1) Research phase (3 ResearchAgents параллельно)

Для каждого тикета:

- Прочитать `vkr/tasks/<id>/brief.md`.
- Заполнить `vkr/tasks/<id>/evidence.md` блоками утверждений.
- Убедиться, что:
  - каждое утверждение атомарно (1 мысль),
  - у каждого значимого факта ≥2 независимых источника,
  - соблюдена давность (≤3/≤5 лет).

Завершение:
- обновить `status.md` на `status: research_done`, `updated: ...`

### 2) Factcheck phase (3 FactcheckAgents параллельно)

Для каждого тикета:

- Прочитать `vkr/tasks/<id>/evidence.md`.
- Заполнить `vkr/tasks/<id>/factcheck.md` вердиктами по каждому утверждению.
- Если не хватает подтверждений/есть противоречия:
  - выставить `needs_fix`/`reject`,
  - перечислить, что именно неверно или чего не хватает,
  - (опционально) указать, что нужно дозапросить исследователю.

Завершение:
- если большинство ключевых утверждений `ok` → `status: factcheck_ok`
- иначе → `status: factcheck_needs_more` (и вернуть в research цикл)

### 3) Writing phase (MainEditor последовательно)

Порядок: сначала `intro`, затем `ch1-1`, затем `ch1-2`.

Для каждого тикета со `status: factcheck_ok`:

1. Написать `vkr/tasks/<id>/output.md` на основе:
   - `brief.md` (структура/объём/требования)
   - `factcheck.md` (только ok и исправленные формулировки)
2. Проставить в тексте ссылки `[n]`:
   - если источник новый → добавить в `vkr/sources/sources.md` как следующий номер
   - если источник уже есть → использовать существующий номер
3. Проверить “гейты спросить пользователя”:
   - если раздел требует выбора/подтверждения (предметная область, стек и т.п.)
     - зафиксировать вопрос в `vkr/decisions/open_questions.md`
     - поставить `status: blocked_user`
     - не переводить в `ready` до появления решения в `vkr/decisions/decisions.md`
4. Интегрировать текст в `vkr/draft/draft.md` (в соответствующий раздел).
5. Обновить `status.md` → `status: ready`.

### 4) Критерии успешного MVP

- Для `intro`, `ch1-1`, `ch1-2`:
  - заполнены `evidence.md` и `factcheck.md`,
  - `output.md` содержит научный текст и ссылки `[n]`,
  - `status.md` = `ready` (или `blocked_user`, если есть незакрытые вопросы).
- `vkr/sources/sources.md` содержит все `[n]`, использованные в `draft.md`.

