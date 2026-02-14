# Makefile for VKR LaTeX document (G7-32 GOST template)
# Compiler: XeLaTeX (required for Times New Roman via fontspec)

LATEX = xelatex
BIBTEX = bibtex
MAIN = main
FLAGS = -interaction=nonstopmode --shell-escape

# Default target: build PDF
all: $(MAIN).pdf

# Build PDF (run twice for TOC and references)
$(MAIN).pdf: $(MAIN).tex preamble.inc.tex chapters/*.tex bibliography.tex
	$(LATEX) $(FLAGS) $(MAIN).tex
	$(LATEX) $(FLAGS) $(MAIN).tex

# Single pass (faster, for drafts)
draft:
	$(LATEX) $(FLAGS) $(MAIN).tex

# Build with BibTeX (when using .bib files)
bib: $(MAIN).tex
	$(LATEX) $(FLAGS) $(MAIN).tex
	$(BIBTEX) $(MAIN)
	$(LATEX) $(FLAGS) $(MAIN).tex
	$(LATEX) $(FLAGS) $(MAIN).tex

# Clean auxiliary files
clean:
	rm -f $(MAIN).aux $(MAIN).log $(MAIN).out $(MAIN).toc
	rm -f $(MAIN).fls $(MAIN).fdb_latexmk $(MAIN).synctex.gz
	rm -f $(MAIN).bbl $(MAIN).blg
	rm -f chapters/*.aux
	rm -f 00-abstract.aux 12-intro.aux 80-conclusion.aux 90-appendix1.aux
	rm -rf _minted-$(MAIN)

# Clean everything including PDF
distclean: clean
	rm -f $(MAIN).pdf

# Watch for changes and rebuild (requires fswatch)
watch:
	@echo "Watching for changes... (requires fswatch)"
	fswatch -o $(MAIN).tex preamble.inc.tex chapters/*.tex bibliography.tex | xargs -n1 -I{} make draft

.PHONY: all draft bib clean distclean watch
