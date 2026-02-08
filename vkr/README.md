## VKR multi-agent workspace

Этот каталог предназначен для мультиагентной подготовки пояснительной записки по структуре и правилам из `docs/VKR_ROADMAP.md`.

### Структура

- `draft/`: единый черновик и оглавление
- `tasks/<task_id>/`: “тикеты” по каждому разделу roadmap (brief/evidence/factcheck/output/status)
- `sources/`: единый реестр источников для ссылок `[n]`
- `decisions/`: вопросы к пользователю, принятые решения, допущения
- `prompts/`: шаблоны инструкций для ролей агентов

### Принцип работы (кратко)

1. ResearchAgent заполняет `tasks/<id>/evidence.md`.
2. FactcheckAgent проверяет факты и пишет `tasks/<id>/factcheck.md`.
3. MainEditor пишет `tasks/<id>/output.md`, обновляет `sources/sources.md`, затем переносит раздел в `draft/draft.md`.

