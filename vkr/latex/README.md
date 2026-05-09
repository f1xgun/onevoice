# Сборка пояснительной записки (LaTeX)

## TinyTeX

Если установлен [TinyTeX](https://yihui.org/tinytex/), добавьте каталог с бинарниками в `PATH` (типично на macOS):

```bash
export PATH="$HOME/Library/TinyTeX/bin/universal-darwin:$PATH"
# или: .../bin/x86_64-darwin — зависит от архитектуры
```

Сборка из этой папки:

```bash
cd vkr/latex
xelatex -interaction=nonstopmode main.tex
xelatex -interaction=nonstopmode main.tex
```

При нехватке пакетов: `tlmgr install <имя-пакета>` (класс G7-32 и шрифты — по требованиям вашего шаблона).

## Диаграммы из Mermaid (`.mmd` → PNG)

Исходники схем:

| Файл | Выход для `\includegraphics` |
|------|------------------------------|
| `images/as-is.mmd` | `images/as-is.png` |
| `diagrams/to-be-process.mmd` | `images/to-be-process.png` |
| `diagrams/to-be-agents.mmd` | `images/to-be-agents.png` |
| `diagrams/er-diagram.mmd` | `images/er-diagram.png` |

Экспорт через [Mermaid CLI](https://github.com/mermaid-js/mermaid-cli) (`@mermaid-js/mermaid-cli`):

```bash
cd vkr/latex
npx --yes @mermaid-js/mermaid-cli mmdc -i images/as-is.mmd -o images/as-is.png -w 2400 -b white
npx --yes @mermaid-js/mermaid-cli mmdc -i diagrams/to-be-process.mmd -o images/to-be-process.png -w 2400 -b white
npx --yes @mermaid-js/mermaid-cli mmdc -i diagrams/to-be-agents.mmd -o images/to-be-agents.png -w 2400 -b white
npx --yes @mermaid-js/mermaid-cli mmdc -i diagrams/er-diagram.mmd -o images/er-diagram.png -w 2400 -b white
```

Подберите `-w` (ширина) под требования ГОСТ к читаемости в PDF.
