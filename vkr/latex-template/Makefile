# Makefile for LaTeX documents (ГОСТ G7-32, TinyTeX / MacTeX / PATH)
# Документы: пояснительная записка (rpz), согласование темы (soglasovanie-temy)

TINYTEX := $(HOME)/Library/TinyTeX/bin/universal-darwin/xelatex
LATEX := $(shell command -v xelatex 2>/dev/null || ([ -x $(TINYTEX) ] && echo $(TINYTEX)) || ([ -x /Library/TeX/texbin/xelatex ] && echo /Library/TeX/texbin/xelatex) || echo xelatex)
FLAGS = -interaction=nonstopmode --shell-escape

# Согласование темы (не зависит от extra/Title.pdf и глав rpz)
soglasovanie-temy.pdf: soglasovanie-temy.tex preamble.inc.tex
	$(LATEX) $(FLAGS) soglasovanie-temy.tex
	$(LATEX) $(FLAGS) soglasovanie-temy.tex

# Пояснительная записка (полный документ)
rpz.pdf: rpz.tex preamble.inc.tex 00-abstract.tex 12-intro.tex 20-analysis.tex 30-design.tex 40-impl.tex 80-conclusion.tex 81-biblio.tex 90-appendix1.tex
	$(LATEX) $(FLAGS) rpz.tex
	$(LATEX) $(FLAGS) rpz.tex

# По умолчанию собираем согласование темы (не требует extra/Title.pdf)
all: soglasovanie-temy.pdf

clean:
	rm -f soglasovanie-temy.aux soglasovanie-temy.log soglasovanie-temy.out
	rm -f soglasovanie-temy.fls soglasovanie-temy.fdb_latexmk soglasovanie-temy.synctex.gz
	rm -f rpz.aux rpz.log rpz.out rpz.toc
	rm -f rpz.fls rpz.fdb_latexmk rpz.synctex.gz
	rm -f 00-abstract.aux 12-intro.aux 20-analysis.aux 30-design.aux 40-impl.aux 80-conclusion.aux 81-biblio.aux 90-appendix1.aux

distclean: clean
	rm -f soglasovanie-temy.pdf rpz.pdf

check:
	@if ! $(LATEX) --version >/dev/null 2>&1; then \
		echo "Ошибка: xelatex не найден."; \
		echo "Установите MacTeX: https://tug.org/mactex/"; \
		echo "или TinyTeX: https://yihui.org/tinytex/"; \
		echo "или добавьте путь к xelatex в PATH (например export PATH=\"/Library/TeX/texbin:\$$PATH\")"; \
		exit 1; \
	fi
	@echo "Используется: $(LATEX)"

.PHONY: all clean distclean check soglasovanie-temy.pdf rpz.pdf
