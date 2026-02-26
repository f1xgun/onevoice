# Makefile for «Описание проекта» report (LaTeX, G7-32)
# Compiler: XeLaTeX (TinyTeX, MacTeX или PATH)

TINYTEX := $(HOME)/Library/TinyTeX/bin/universal-darwin/xelatex
LATEX := $(shell command -v xelatex 2>/dev/null || ([ -x $(TINYTEX) ] && echo $(TINYTEX)) || ([ -x /Library/TeX/texbin/xelatex ] && echo /Library/TeX/texbin/xelatex) || echo xelatex)
MAIN = report
FLAGS = -interaction=nonstopmode

all: $(MAIN).pdf

$(MAIN).pdf: $(MAIN).tex preamble.inc.tex 00-title.tex 12-intro.tex 10-body-short.tex
	$(LATEX) $(FLAGS) $(MAIN).tex
	$(LATEX) $(FLAGS) $(MAIN).tex

draft:
	$(LATEX) $(FLAGS) $(MAIN).tex

clean:
	rm -f $(MAIN).aux $(MAIN).log $(MAIN).out $(MAIN).toc
	rm -f $(MAIN).fls $(MAIN).fdb_latexmk $(MAIN).synctex.gz
	rm -f chapters/*.aux 00-title.aux 12-intro.aux 10-body-short.aux

distclean: clean
	rm -f $(MAIN).pdf

# Проверка наличия xelatex (запуск: make check)
check:
	@if ! $(LATEX) --version >/dev/null 2>&1; then \
		echo "Ошибка: xelatex не найден."; \
		echo "Установите MacTeX: https://tug.org/mactex/"; \
		echo "или TinyTeX: https://yihui.org/tinytex/"; \
		echo "или добавьте путь к xelatex в PATH (например export PATH=\"/Library/TeX/texbin:\$$PATH\")"; \
		exit 1; \
	fi
	@echo "Используется: $(LATEX)"

.PHONY: all draft clean distclean check
