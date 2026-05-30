# OneVoice dev mTLS — operator convenience wrappers.
#
# These targets thin-wrap the scripts/ entry points so an operator working
# inside infra/mtls/ca/ can run `make certs` / `make clean-certs` without
# remembering the full path.  See infra/mtls/README.md for the full runbook.

ROOT := $(shell git rev-parse --show-toplevel 2>/dev/null)
ifeq ($(strip $(ROOT)),)
ROOT := $(shell cd ../../.. && pwd)
endif

.PHONY: certs clean-certs check

certs: ## Generate dev CA + per-service leaf certs (idempotent)
	@bash $(ROOT)/scripts/gen-mtls-certs.sh

check: ## Verify every leaf cert against the dev CA + expiry window
	@bash $(ROOT)/scripts/check-mtls-certs.sh

clean-certs: ## Remove all generated cert material (next `make certs` rebuilds)
	@find $(ROOT)/infra/mtls/certs -mindepth 1 ! -name '.gitignore' -delete
	@echo "infra/mtls/certs/ cleaned (kept .gitignore)"
