#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT
mkdir "$test_dir/bin"

cat > "$test_dir/bin/go" <<'SH'
#!/usr/bin/env bash
set -eu
if [[ "$OAPI_TEST_CASE" == preprocess_failure ]]; then exit 41; fi
printf 'prepared\n' > "$4"
SH
cat > "$test_dir/bin/generator" <<'SH'
#!/usr/bin/env bash
set -eu
touch "$OAPI_TEST_GENERATED"
case "$OAPI_TEST_CASE" in
  generator_failure) printf 'partial\n' > "$OAPI_TEST_OUT"; exit 42 ;;
  drift) printf 'changed\n' > "$OAPI_TEST_OUT" ;;
  *) printf 'original\n' > "$OAPI_TEST_OUT" ;;
esac
SH
chmod +x "$test_dir/bin/go" "$test_dir/bin/generator"

for scenario in preprocess_failure generator_failure drift unchanged; do
  printf 'original\n' > "$test_dir/types.go"
  rm -f "$test_dir/prepared.yaml" "$test_dir/generated"
  status=0
  PATH="$test_dir/bin:$PATH" OAPI_TEST_CASE="$scenario" \
    OAPI_TEST_OUT="$test_dir/types.go" OAPI_TEST_GENERATED="$test_dir/generated" \
    make --no-print-directory -C "$repo_root" oapi-check \
    OAPI_BIN="$test_dir/bin/generator" OAPI_OUT="$test_dir/types.go" \
    OAPI_SPEC="$test_dir/source.yaml" OAPI_PREP="$test_dir/prepared.yaml" \
    > "$test_dir/result.log" 2>&1 || status=$?
  if [[ "$scenario" == unchanged ]]; then
    [[ "$status" == 0 ]] || { cat "$test_dir/result.log"; exit 1; }
  else
    [[ "$status" != 0 ]] || { echo "FAIL: $scenario reported success"; exit 1; }
  fi
  [[ "$(cat "$test_dir/types.go")" == original ]] || { echo "FAIL: $scenario changed tracked output"; exit 1; }
  [[ ! -e "$test_dir/prepared.yaml" ]] || { echo "FAIL: $scenario left an intermediate spec"; exit 1; }
  if [[ "$scenario" == preprocess_failure ]]; then
    [[ ! -e "$test_dir/generated" ]] || { echo 'FAIL: generator ran after preprocessing failed'; exit 1; }
  fi
  echo "PASS: $scenario"
done
