# Makefile for Requirements Specification LaTeX document
# Compiler: XeLaTeX (TinyTeX, MacTeX или PATH)

TINYTEX := $(HOME)/Library/TinyTeX/bin/universal-darwin/xelatex
LATEX := $(shell command -v xelatex 2>/dev/null || ([ -x $(TINYTEX) ] && echo $(TINYTEX)) || ([ -x /Library/TeX/texbin/xelatex ] && echo /Library/TeX/texbin/xelatex) || echo xelatex)
MAIN = main
FLAGS = -interaction=nonstopmode --shell-escape

all: $(MAIN).pdf

$(MAIN).pdf: $(MAIN).tex preamble.inc.tex chapters/*.tex bibliography.tex
	$(LATEX) $(FLAGS) $(MAIN).tex
	$(LATEX) $(FLAGS) $(MAIN).tex

draft:
	$(LATEX) $(FLAGS) $(MAIN).tex

clean:
	rm -f $(MAIN).aux $(MAIN).log $(MAIN).out $(MAIN).toc
	rm -f $(MAIN).fls $(MAIN).fdb_latexmk $(MAIN).synctex.gz
	rm -f chapters/*.aux

distclean: clean
	rm -f $(MAIN).pdf

# Проверка наличия xelatex (запуск: make check)
check:
	@if ! $(LATEX) --version >/dev/null 2>&1; then \
		echo "Ошибка: xelatex не найден."; \
		echo "Установите MacTeX: https://tug.org/mactex/"; \
		echo "или добавьте путь к xelatex в PATH (например export PATH=\"/Library/TeX/texbin:\$$PATH\")"; \
		exit 1; \
	fi
	@echo "Используется: $(LATEX)"

.PHONY: all draft clean distclean check
