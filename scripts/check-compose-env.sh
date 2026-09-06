#!/usr/bin/env bash
#
# check-compose-env.sh — fail when a service reads an env var that
# docker-compose.yml never passes to its container.
#
# The compose file is used only for ${} interpolation of .env; nothing in .env
# reaches a container unless the service's `environment:` block names it. Every
# var a service reads but compose omits is therefore dead in every compose
# deploy — that is how APP_ENV, PUBLIC_URL, the LEGAL_* block and the Telegram
# approval secret silently stopped working.
#
# Scans each service's Go sources (excluding _test.go) for os.Getenv /
# os.LookupEnv / getEnv* call sites and diffs the names against the keys of that
# service's compose `environment:` block.
#
# Usage: ./scripts/check-compose-env.sh   (wired into `make lint-compose-env`)

set -euo pipefail

cd "$(dirname "$0")/.."

COMPOSE_FILE="docker-compose.yml"

# service:source-dir pairs. Keep in sync with the compose services that run a
# OneVoice Go binary.
SERVICES=(
  "api:services/api"
  "orchestrator:services/orchestrator"
  "agent-telegram:services/agent-telegram"
  "agent-vk:services/agent-vk"
  "agent-yandex-business:services/agent-yandex-business"
  "agent-google-business:services/agent-google-business"
)

# Env vars deliberately NOT plumbed through compose:
#   ALLOW_NO_RATE_LIMIT — operator escape hatch that disables the LLM cost
#     guard; must stay an explicit, out-of-band opt-in.
#   METRICS_PORT — read by the standalone cmd/rekey CLI, not by the api server.
# Anything whose name contains TEST is test-harness-only and skipped too.
IGNORE="ALLOW_NO_RATE_LIMIT METRICS_PORT"

# compose_env_keys prints the environment keys of one compose service.
compose_env_keys() {
  awk -v svc="  $1:" '
    $0 == svc { in_svc = 1; next }
    in_svc && /^  [^ ]/ { in_svc = 0 }
    in_svc && /^    environment:/ { in_env = 1; next }
    in_env && /^    [^ ]/ { in_env = 0 }
    in_env && /^      [A-Za-z_][A-Za-z0-9_]*:/ {
      key = $1
      sub(/:$/, "", key)
      print key
    }
  ' "$COMPOSE_FILE"
}

# go_env_names prints the env var names read by the Go sources under one dir.
go_env_names() {
  grep -rhoE --include='*.go' --exclude='*_test.go' \
    -e 'os\.(Getenv|LookupEnv)\("[A-Z0-9_]+"' \
    -e '[Gg]etEnv[A-Za-z]*\("[A-Z0-9_]+"' \
    "$1" \
    | grep -oE '"[A-Z0-9_]+"' \
    | tr -d '"' \
    | grep -v TEST \
    | sort -u
}

status=0
for entry in "${SERVICES[@]}"; do
  svc="${entry%%:*}"
  dir="${entry##*:}"

  keys="$(compose_env_keys "$svc" | sort -u)"
  if [ -z "$keys" ]; then
    echo "❌ $svc: no environment: block found in $COMPOSE_FILE"
    status=1
    continue
  fi

  missing=""
  while read -r name; do
    [ -n "$name" ] || continue
    case " $IGNORE " in *" $name "*) continue ;; esac
    if ! printf '%s\n' "$keys" | grep -qx "$name"; then
      missing="$missing $name"
    fi
  done <<EOF
$(go_env_names "$dir")
EOF

  if [ -n "$missing" ]; then
    echo "❌ $svc reads env vars that $COMPOSE_FILE never passes to the container:"
    for name in $missing; do
      echo "     $name"
    done
    status=1
  else
    echo "✓ $svc: every env var it reads is passed by compose"
  fi
done

if [ "$status" -ne 0 ]; then
  echo
  echo "Add the missing keys to the service's environment: block (\${VAR:-} for optional ones),"
  echo "or add a documented entry to IGNORE in scripts/check-compose-env.sh."
fi
exit "$status"
