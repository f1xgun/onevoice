from __future__ import annotations

import os
import re
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Todo:
    id: str
    content: str


def parse_roadmap_todos(roadmap_path: Path) -> list[Todo]:
    """
    Parses the YAML frontmatter in docs/VKR_ROADMAP.md.

    We intentionally avoid external deps (PyYAML) and extract:
      - id: <id>
      - content: <text>

    Assumptions:
      - Frontmatter is bounded by first two '---' lines.
      - Todos appear as:
          - id: something
            content: "..."
        or content: ...
    """
    text = roadmap_path.read_text(encoding="utf-8")
    parts = text.split("---", 2)
    if len(parts) < 3:
        raise RuntimeError(f"Expected YAML frontmatter in {roadmap_path}")

    frontmatter = parts[1]
    lines = frontmatter.splitlines()

    todos: list[Todo] = []
    current_id: str | None = None
    current_content: str | None = None

    id_re = re.compile(r"^\s*-\s*id:\s*(.+?)\s*$")
    content_re = re.compile(r"^\s*content:\s*(.+?)\s*$")

    for line in lines:
        m_id = id_re.match(line)
        if m_id:
            # flush previous
            if current_id and current_content is not None:
                todos.append(Todo(id=current_id, content=current_content))
            current_id = m_id.group(1).strip().strip('"').strip("'")
            current_content = None
            continue

        m_content = content_re.match(line)
        if m_content and current_id:
            raw = m_content.group(1).strip()
            # strip wrapping quotes if present
            if (raw.startswith('"') and raw.endswith('"')) or (raw.startswith("'") and raw.endswith("'")):
                raw = raw[1:-1]
            current_content = raw

    if current_id and current_content is not None:
        todos.append(Todo(id=current_id, content=current_content))

    if not todos:
        raise RuntimeError(f"No todos parsed from frontmatter in {roadmap_path}")
    return todos


def ensure_file(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists():
        return
    path.write_text(content, encoding="utf-8")


def default_brief(todo: Todo) -> str:
    return f"""## {todo.id}

**Roadmap task:** {todo.content}

### Назначение
- Подготовить материалы для раздела `{todo.id}` в формате, удобном для фактчекинга и последующей сборки в `vkr/draft/draft.md`.

### Критерии готовности
- В `evidence.md` перечислены ключевые утверждения/факты, каждое сопровождается 2–3 независимыми источниками (или пометкой, что источники найти не удалось).
- В `factcheck.md` по каждому утверждению есть статус `ok/needs_fix/reject` и правка формулировки при необходимости.
- В `output.md` отсутствуют непроверенные факты; все значимые факты имеют ссылки `[n]`.

### Вопросы к пользователю (если нужны)
- Если для выполнения требуются выбор технологий/аналогов/предметной области, зафиксировать вопрос в `vkr/decisions/open_questions.md`.
"""


def default_evidence(todo: Todo) -> str:
    return f"""## Evidence: {todo.id}

Формат блока:

- **утверждение**: ...
- **источник_1**: ...
- **источник_2**: ...
- **источник_3**: ...
- **дата/контекст**: ...
- **заметки**: ...

---

_Пока пусто._
"""


def default_factcheck(todo: Todo) -> str:
    return f"""## Factcheck: {todo.id}

Формат блока:

- **утверждение**: ...
- **статус**: ok | needs_fix | reject
- **подтверждающие_источники**: ...
- **пояснение**: ...
- **рекомендуемая_правка_формулировки**: ...

---

_Пока пусто._
"""


def default_output(todo: Todo) -> str:
    return f"""## {todo.id}

_TBD_
"""


def default_status(todo: Todo) -> str:
    return """status: pending
owner: ""
updated: ""
notes: ""
"""


def main() -> None:
    repo_root = Path(__file__).resolve().parents[2]
    roadmap = repo_root / "docs" / "VKR_ROADMAP.md"
    todos = parse_roadmap_todos(roadmap)

    tasks_root = repo_root / "vkr" / "tasks"
    for todo in todos:
        task_dir = tasks_root / todo.id
        ensure_file(task_dir / "brief.md", default_brief(todo))
        ensure_file(task_dir / "evidence.md", default_evidence(todo))
        ensure_file(task_dir / "factcheck.md", default_factcheck(todo))
        ensure_file(task_dir / "output.md", default_output(todo))
        ensure_file(task_dir / "status.md", default_status(todo))

    print(f"Initialized {len(todos)} task folders under {tasks_root}")


if __name__ == "__main__":
    # Avoid surprises if script is run from elsewhere
    os.environ.setdefault("PYTHONUTF8", "1")
    main()

