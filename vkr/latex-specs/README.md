# Спецификация требований OneVoice

Отчёт по спецификации требований в формате LaTeX по шаблону `docs/Спецификация_требований_v1_для_ВКР_2026.docx`.

## Сборка

Требуется XeLaTeX (пакет TeX Live или MacTeX).

Если `make` выдаёт «xelatex: No such file or directory»:
- **TinyTeX** (~/Library/TinyTeX): Makefile автоматически подхватывает этот путь.
- **MacTeX:** установите [MacTeX](https://tug.org/mactex/) или добавьте в PATH: `export PATH="/Library/TeX/texbin:$PATH"`
- **Linux:** установите пакет `texlive-xetex` (и при необходимости `texlive-lang-cyrillic`)

```bash
make check    # Проверить, что xelatex найден
make          # Полная сборка (два прохода для оглавления и ссылок)
make draft    # Один проход (быстрее, для черновиков)
make clean    # Очистка вспомогательных файлов
```

Или вручную:

```bash
xelatex -interaction=nonstopmode --shell-escape main.tex
xelatex -interaction=nonstopmode --shell-escape main.tex
```

## Структура

- `chapters/01-general.tex` — Раздел 1. Общее описание
- `chapters/02-interfaces.tex` — Раздел 2.1. Внешние интерфейсы
- `chapters/03-functional.tex` — Раздел 2.2. Функциональные требования
- `chapters/04-nonfunctional.tex` — Раздел 2.3. Нефункциональные требования
- `chapters/90-appendices.tex` — Раздел 3. Приложения

## Зависимости

Переиспользуются файлы из `vkr/latex/` (класс G7-32, преамбула, стили ГОСТ).
