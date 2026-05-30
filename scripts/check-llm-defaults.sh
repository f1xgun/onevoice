#!/usr/bin/env bash
# Phase 24 LLM default-model lint guard.
#
# Asserts .env.example and docker-compose.yml carry the Anthropic Sonnet 4.6
# default + Haiku 4.5 cheap fallbacks established by Plan 24-03. A future PR
# that silently reverts these defaults to satisfy a personal dev setup fails
# CI before merging.
#
# Wired into `make lint-all`. See
# .planning/phases/24-llm-quality-streaming-wins/24-03-PLAN.md for context.
set -euo pipefail

fail=0

check_pattern() {
  local file="$1"
  local pattern="$2"
  local label="$3"
  if ! grep -qE "$pattern" "$file"; then
    echo "FAIL: $label not found in $file"
    echo "      expected pattern: $pattern"
    fail=1
  fi
}

check_pattern ".env.example" '^LLM_MODEL=anthropic/claude-sonnet-4-6$' "LLM_MODEL default"
check_pattern ".env.example" '^TITLER_MODEL=anthropic/claude-haiku-4-5$' "TITLER_MODEL default"
check_pattern ".env.example" '^DRAFT_REPLY_MODEL=anthropic/claude-haiku-4-5$' "DRAFT_REPLY_MODEL default"

check_pattern "docker-compose.yml" 'LLM_MODEL: \$\{LLM_MODEL:-anthropic/claude-sonnet-4-6\}' "docker-compose.yml LLM_MODEL fallback"
check_pattern "docker-compose.yml" 'TITLER_MODEL: \$\{TITLER_MODEL:-anthropic/claude-haiku-4-5\}' "docker-compose.yml TITLER_MODEL fallback"
check_pattern "docker-compose.yml" 'DRAFT_REPLY_MODEL: \$\{DRAFT_REPLY_MODEL:-anthropic/claude-haiku-4-5\}' "docker-compose.yml DRAFT_REPLY_MODEL fallback"

if [[ $fail -ne 0 ]]; then
  echo ""
  echo "Phase 24 LLM default-model lint guard failed."
  echo "Re-set the defaults per .planning/phases/24-llm-quality-streaming-wins/24-03-PLAN.md"
  exit 1
fi

echo "LLM default-model lint guard: OK"
