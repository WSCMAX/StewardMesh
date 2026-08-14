#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd -- "$script_dir/../.." && pwd)"
cd "$repository_root"

postgres_admin_url="${STEWARDMESH_E2E_POSTGRES_ADMIN_URL:-postgres://stewardmesh:stewardmesh@127.0.0.1:5432/postgres?sslmode=disable}"
postgres_database_prefix="${STEWARDMESH_E2E_DATABASE_URL_PREFIX:-postgres://stewardmesh:stewardmesh@127.0.0.1:5432/}"
run_id="$$"
database_name="stewardmesh_e2e_phase_one_${run_id}"
database_url="${postgres_database_prefix}${database_name}?sslmode=disable"
organization_id="e2e-phase-one"
backend_url="http://127.0.0.1:18080"
browser_url="http://127.0.0.1:15173"
playwright_cli="$repository_root/web/node_modules/.bin/playwright-cli"
vite_cli="$repository_root/web/node_modules/.bin/vite"
playwright_config_template="$script_dir/playwright.config.json"
output_relative="output/playwright/phase-one-gate/run-${run_id}"
output_dir="$repository_root/$output_relative"
temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/stewardmesh-phase-one-e2e.XXXXXX")"
playwright_config="$temporary_dir/playwright.config.json"
backend_pid=""
vite_pid=""
database_created="false"
active_sessions=()

cleanup() {
  local exit_status=$?
  trap - EXIT INT TERM
  if (( ${#active_sessions[@]} > 0 )); then
    for session in "${active_sessions[@]}"; do
      "$playwright_cli" -s="$session" close >/dev/null 2>&1 || true
    done
  fi
  if [[ -n "$vite_pid" ]] && kill -0 "$vite_pid" 2>/dev/null; then
    kill "$vite_pid" 2>/dev/null || true
    wait "$vite_pid" 2>/dev/null || true
  fi
  if [[ -n "$backend_pid" ]] && kill -0 "$backend_pid" 2>/dev/null; then
    kill "$backend_pid" 2>/dev/null || true
    wait "$backend_pid" 2>/dev/null || true
  fi
  if [[ "$database_created" == "true" ]]; then
    if ! STEWARDMESH_E2E_ALLOW_FIXTURES="yes-I-understand-this-is-disposable" \
    STEWARDMESH_E2E_DATABASE_URL="$database_url" \
    STEWARDMESH_E2E_POSTGRES_ADMIN_URL="$postgres_admin_url" \
    STEWARDMESH_E2E_ORGANIZATION_ID="$organization_id" \
    "$temporary_dir/stewardmesh-e2e-fixture" drop-database; then
      printf 'WARNING: failed to drop validated disposable database %s\n' "$database_name" >&2
      if [[ "$exit_status" -eq 0 ]]; then
        exit_status=1
      fi
    fi
  fi
  rm -rf -- "$temporary_dir"
  exit "$exit_status"
}
trap cleanup EXIT INT TERM

for required_command in curl go node sed; do
  if ! command -v "$required_command" >/dev/null 2>&1; then
    printf 'phase-one E2E requires %s on PATH\n' "$required_command" >&2
    exit 2
  fi
done
if [[ ! "$postgres_database_prefix" =~ ^postgres(ql)?://.+/$ ]]; then
  printf 'STEWARDMESH_E2E_DATABASE_URL_PREFIX must be a PostgreSQL URL prefix ending in /\n' >&2
  exit 2
fi
if [[ ! -x "$playwright_cli" || ! -x "$vite_cli" ]]; then
  printf 'web dependencies are missing; run npm ci in web before the phase-one E2E gate\n' >&2
  exit 2
fi

mkdir -p "$output_dir"
sed "s#\"output/playwright/phase-one-gate\"#\"$output_relative\"#" "$playwright_config_template" >"$playwright_config"

go build -o "$temporary_dir/stewardmesh" ./cmd/stewardmesh
go build -o "$temporary_dir/stewardmesh-e2e-fixture" ./cmd/stewardmesh-e2e-fixture

STEWARDMESH_E2E_ALLOW_FIXTURES="yes-I-understand-this-is-disposable" \
STEWARDMESH_E2E_DATABASE_URL="$database_url" \
STEWARDMESH_E2E_POSTGRES_ADMIN_URL="$postgres_admin_url" \
STEWARDMESH_E2E_ORGANIZATION_ID="$organization_id" \
"$temporary_dir/stewardmesh-e2e-fixture" validate-target

STEWARDMESH_E2E_ALLOW_FIXTURES="yes-I-understand-this-is-disposable" \
STEWARDMESH_E2E_DATABASE_URL="$database_url" \
STEWARDMESH_E2E_POSTGRES_ADMIN_URL="$postgres_admin_url" \
STEWARDMESH_E2E_ORGANIZATION_ID="$organization_id" \
"$temporary_dir/stewardmesh-e2e-fixture" create-database
database_created="true"

STEWARDMESH_ADDR="127.0.0.1:18080" \
STEWARDMESH_ALLOWED_ORIGIN="$browser_url" \
STEWARDMESH_DATABASE_URL="$database_url" \
STEWARDMESH_REPOSITORY_DRIVER="postgres" \
STEWARDMESH_SESSION_COOKIE_SECURE="false" \
STEWARDMESH_ORGANIZATION_ID="$organization_id" \
STEWARDMESH_ORGANIZATION_NAME="StewardMesh Phase One E2E" \
STEWARDMESH_DATA_DIR="$temporary_dir/data" \
STEWARDMESH_BLOB_DIR="$temporary_dir/storage" \
"$temporary_dir/stewardmesh" >"$output_dir/backend.log" 2>&1 &
backend_pid=$!

STEWARDMESH_DEV_PROXY_TARGET="$backend_url" \
"$vite_cli" "$repository_root/web" --host 127.0.0.1 --port 15173 --strictPort >"$output_dir/vite.log" 2>&1 &
vite_pid=$!

wait_for_url() {
  local name=$1
  local url=$2
  local process_id=$3
  local attempt
  for attempt in {1..60}; do
    if ! kill -0 "$process_id" 2>/dev/null; then
      printf '%s stopped before becoming ready; inspect %s\n' "$name" "$output_dir" >&2
      return 1
    fi
    if curl --fail --silent --show-error --max-time 2 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  printf '%s did not become ready at %s\n' "$name" "$url" >&2
  return 1
}

wait_for_url "StewardMesh backend" "$backend_url/healthz" "$backend_pid"
wait_for_url "Vite frontend" "$browser_url" "$vite_pid"

run_scenario() {
  local name=$1
  local session="phase-one-${name}-${run_id}"
  local source="$script_dir/scenarios/${name}.js"
  active_sessions+=("$session")
  "$playwright_cli" -s="$session" open --config="$playwright_config" "$browser_url" >"$output_dir/${name}-open.log"
  "$playwright_cli" -s="$session" run-code --filename="$source" | tee "$output_dir/${name}.log"
  "$playwright_cli" -s="$session" close >>"$output_dir/${name}.log"
}

run_scenario "bootstrap-admin"

STEWARDMESH_E2E_ALLOW_FIXTURES="yes-I-understand-this-is-disposable" \
STEWARDMESH_E2E_DATABASE_URL="$database_url" \
STEWARDMESH_E2E_ORGANIZATION_ID="$organization_id" \
"$temporary_dir/stewardmesh-e2e-fixture" >"$output_dir/fixture.log" 2>&1

run_scenario "workspace-admin"
run_scenario "reader-denied-mobile"
run_scenario "atlas-codes-admin"

printf 'Phase-one Chromium E2E gate passed. Non-secret diagnostics: %s\n' "$output_dir"
