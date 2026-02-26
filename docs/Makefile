# Makefile для «Согласование темы» — сборка через ГОСТ-шаблон (vkr/latex-template)
# Используется TinyTeX/MacTeX как в других LaTeX Makefile проекта

TEMPLATE_DIR = ../vkr/latex-template
MAIN = soglasovanie-temy

# Сборка в папке шаблона, копирование PDF в docs
all: $(MAIN).pdf

$(MAIN).pdf:
	$(MAKE) -C $(TEMPLATE_DIR) soglasovanie-temy.pdf
	cp $(TEMPLATE_DIR)/$(MAIN).pdf .

draft:
	$(MAKE) -C $(TEMPLATE_DIR) soglasovanie-temy.pdf
	cp $(TEMPLATE_DIR)/$(MAIN).pdf .

clean:
	$(MAKE) -C $(TEMPLATE_DIR) clean
	rm -f $(MAIN).pdf

distclean: clean

check:
	$(MAKE) -C $(TEMPLATE_DIR) check

.PHONY: all draft clean distclean check
