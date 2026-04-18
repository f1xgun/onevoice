#!/usr/bin/env bash
# check-doc-tool-drift.sh
#
# Fails if any *.md file references a tool name ({platform}__{action})
# that no longer exists as a string literal in Go code.
#
# The goal is to catch docs that quote specific tool names which have been
# renamed/removed. Docs *should* usually point at source of truth (see
# services/*/AGENTS.md) rather than enumerate tools, but when a specific
# name appears in prose (e.g. example commit messages, architecture text)
# it must match reality.
#
# Complements ast-index which indexes Go symbols (not string literals) —
# use `ast-index outline services/agent-XXX/internal/agent/handler.go` to
# inspect handler shape; this script covers the string-literal side.
#
# Uses git grep rather than ripgrep so it runs in any environment that has
# git (which includes lefthook, CI, and freshly-cloned worktrees).

set -euo pipefail

if ! git rev-parse --show-toplevel >/dev/null 2>&1; then
    echo "docs-check: not inside a git repository" >&2
    exit 2
fi
cd "$(git rev-parse --show-toplevel)"

PLATFORMS='telegram|vk|yandex_business|google_business'
# git grep uses POSIX ERE (no \b); the double-underscore separator makes
# \b unnecessary — matches anchor on `telegram__` etc. naturally.
TOOL_RE="(${PLATFORMS})__[a-zA-Z_][a-zA-Z0-9_]*"
LITERAL_RE="\"(${PLATFORMS})__[a-zA-Z_][a-zA-Z0-9_]*\""

# Tool names referenced in live project docs — exclude the thesis workspace,
# planning scratch, .claude workflow files, archived docs, and AUDIT.md.
docs=$(git grep -hEo "$TOOL_RE" -- \
    '*.md' \
    ':!vkr/' \
    ':!.planning/' \
    ':!.claude/' \
    ':!docs/archive/' \
    ':!AUDIT.md' \
    2>/dev/null | sort -u || true)

# Tool names that exist as string literals in Go source.
code=$(git grep -hEo "$LITERAL_RE" -- '*.go' \
    2>/dev/null | tr -d '"' | sort -u || true)

# Unknown = present in docs, absent from code.
unknown=$(comm -23 <(printf '%s\n' "$docs") <(printf '%s\n' "$code") | grep -v '^$' || true)

if [ -n "$unknown" ]; then
    echo "docs-check: FAIL — docs reference tool names not present in Go code:" >&2
    printf '    %s\n' $unknown >&2
    echo "" >&2
    first=$(printf '%s\n' "$unknown" | head -1)
    echo "To locate the stale reference(s):" >&2
    echo "    git grep -nE '$first' -- '*.md'" >&2
    echo "" >&2
    echo "If the tool was removed, update/delete the doc." >&2
    echo "If it was renamed, fix the doc." >&2
    echo "Prefer pointers to services/*/AGENTS.md over enumerating tools in prose." >&2
    exit 1
fi

docs_count=$([ -n "$docs" ] && printf '%s\n' "$docs" | wc -l | tr -d ' ' || echo 0)
code_count=$([ -n "$code" ] && printf '%s\n' "$code" | wc -l | tr -d ' ' || echo 0)
echo "docs-check: ok — ${docs_count} unique tool names in docs, ${code_count} in code, no drift"
