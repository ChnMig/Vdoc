#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

LIVE_ENV_KEYS=(
  VDOC_TEST_DATABASE_DSN
  VDOC_TEST_STORAGE_ENDPOINT
  VDOC_TEST_STORAGE_BUCKET
  VDOC_TEST_STORAGE_ACCESS_KEY
  VDOC_TEST_STORAGE_SECRET_KEY
)

usage() {
  cat <<'EOF'
usage: ./scripts/vdoc-e2e.sh <mode> [options]

Modes:
  happy                         Run the in-memory happy-path E2E test.
  failure                       Run the in-memory failure-matrix E2E test.
  all                           Run happy and failure E2E tests. Default mode.
  live                          Require VDOC_TEST_* env and run live PostgreSQL/RustFS E2E.
  live-check                    Validate required VDOC_TEST_* env without running Go tests.
  live-compose [options]        Derive VDOC_TEST_* env from a root Docker Compose env file.
  help                          Show this help.

Options for live-compose:
  --env-file <path>             Compose env file to read. Defaults to ../.env.
  --check-only                  Validate derived env without running Go tests.
  VDOC_TEST_POSTGRES_DB         Disposable test DB name. Defaults to vdoc_e2e.

Required live env:
  VDOC_TEST_DATABASE_DSN
  VDOC_TEST_STORAGE_ENDPOINT
  VDOC_TEST_STORAGE_BUCKET
  VDOC_TEST_STORAGE_ACCESS_KEY
  VDOC_TEST_STORAGE_SECRET_KEY
EOF
}

require_live_env() {
  local missing=()
  local key
  for key in "${LIVE_ENV_KEYS[@]}"; do
    if [ -z "${!key:-}" ]; then
      missing+=("$key")
    fi
  done

  if [ "${#missing[@]}" -gt 0 ]; then
    printf 'missing required live E2E environment variables:\n' >&2
    printf '  %s\n' "${missing[@]}" >&2
    printf 'See ../PILOT_RUNBOOK.md or tests/e2e/README.md for setup.\n' >&2
    exit 2
  fi
}

postgres_database_name_from_dsn() {
  local dsn="$1"
  local authority database_path without_scheme
  [[ "$dsn" == postgres://* || "$dsn" == postgresql://* ]] || return 1

  without_scheme="${dsn#*://}"
  authority="${without_scheme%%/*}"
  [ -n "$authority" ] && [ "$authority" != "$without_scheme" ] || return 1

  database_path="${without_scheme#*/}"
  database_path="${database_path%%\?*}"
  database_path="${database_path%%#*}"
  [ -n "$database_path" ] && [[ "$database_path" != *%* && "$database_path" != */* ]] || return 1

  printf '%s' "$database_path"
  return 0
}

refuse_application_database() {
  local test_db="$1"
  local app_db="$2"
  printf 'refusing to run live E2E against application database (test database: %s; application database: %s)\n' "$test_db" "$app_db" >&2
  exit 2
}

require_live_database_not_app() {
  local app_db test_db
  app_db="${VDOC_POSTGRES_DB:-vdoc}"
  if ! test_db="$(postgres_database_name_from_dsn "$VDOC_TEST_DATABASE_DSN")"; then
    printf 'refusing to run live E2E because VDOC_TEST_DATABASE_DSN database name could not be determined\n' >&2
    exit 2
  fi
  if [ "$test_db" = "$app_db" ]; then
    refuse_application_database "$test_db" "$app_db"
  fi
}

require_live_compose_database_not_app() {
  local app_db
  app_db="$(env_or_default VDOC_POSTGRES_DB vdoc)"
  if [ "$VDOC_TEST_POSTGRES_DB" = "$app_db" ]; then
    refuse_application_database "$VDOC_TEST_POSTGRES_DB" "$app_db"
  fi
}

print_live_env_ok() {
  local label="$1"
  printf '%s for:\n' "$label"
  printf '  %s\n' "${LIVE_ENV_KEYS[@]}"
}

run_live_tests() {
  require_live_env
  require_live_database_not_app
  go test ./db/... -count=1 -v
  VDOC_E2E_LIVE=1 go test ./tests/e2e -run '^TestVdocV01EndToEndLivePersistence$' -count=1 -v
}

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

read_env_file() {
  local env_file="$1"
  if [ ! -f "$env_file" ]; then
    printf 'env file not found: %s\n' "$env_file" >&2
    exit 2
  fi

  local line key value
  while IFS= read -r line || [ -n "$line" ]; do
    line="$(trim "$line")"
    if [ -z "$line" ] || [[ "$line" == \#* ]]; then
      continue
    fi
    if [[ "$line" == export[[:space:]]* ]]; then
      line="$(trim "${line#export}")"
    fi
    if [[ "$line" != *=* ]]; then
      continue
    fi
    key="$(trim "${line%%=*}")"
    value="$(trim "${line#*=}")"
    if [[ "$value" == \"*\" && "$value" == *\" ]] || [[ "$value" == \'*\' && "$value" == *\' ]]; then
      value="${value:1:${#value}-2}"
    fi
    if [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] && is_compose_env_key_allowed "$key"; then
      export "$key=$value"
    fi
  done <"$env_file"
}

is_compose_env_key_allowed() {
  case "$1" in
    VDOC_POSTGRES_HOST_PORT|VDOC_POSTGRES_DB|VDOC_POSTGRES_USER|VDOC_POSTGRES_PASSWORD|VDOC_RUSTFS_HOST_PORT|VDOC_STORAGE_BUCKET|VDOC_STORAGE_ACCESS_KEY|VDOC_STORAGE_SECRET_KEY|VDOC_TEST_POSTGRES_DB|VDOC_TEST_STORAGE_USE_SSL|VDOC_TEST_STORAGE_PATH_STYLE)
      return 0
      ;;
  esac
  return 1
}

env_or_default() {
  local key="$1"
  local fallback="$2"
  if [ -n "${!key:-}" ]; then
    printf '%s' "${!key}"
    return
  fi
  printf '%s' "$fallback"
}

require_compose_env() {
  local missing=()
  local key
  for key in VDOC_POSTGRES_PASSWORD VDOC_STORAGE_ACCESS_KEY VDOC_STORAGE_SECRET_KEY; do
    if [ -z "${!key:-}" ]; then
      missing+=("$key")
    fi
  done

  if [ "${#missing[@]}" -gt 0 ]; then
    printf 'missing required Compose environment variables:\n' >&2
    printf '  %s\n' "${missing[@]}" >&2
    exit 2
  fi
}

derive_live_env_from_compose() {
  require_compose_env

  local postgres_host_port postgres_user rustfs_host_port
  postgres_host_port="$(env_or_default VDOC_POSTGRES_HOST_PORT 5432)"
  postgres_user="$(env_or_default VDOC_POSTGRES_USER vdoc)"
  rustfs_host_port="$(env_or_default VDOC_RUSTFS_HOST_PORT 9000)"
  export VDOC_TEST_POSTGRES_DB="$(env_or_default VDOC_TEST_POSTGRES_DB vdoc_e2e)"

  export VDOC_TEST_DATABASE_DSN="postgres://${postgres_user}:${VDOC_POSTGRES_PASSWORD}@127.0.0.1:${postgres_host_port}/${VDOC_TEST_POSTGRES_DB}?sslmode=disable"
  export VDOC_TEST_STORAGE_ENDPOINT="127.0.0.1:${rustfs_host_port}"
  export VDOC_TEST_STORAGE_BUCKET="$(env_or_default VDOC_STORAGE_BUCKET vdoc)"
  export VDOC_TEST_STORAGE_ACCESS_KEY="$VDOC_STORAGE_ACCESS_KEY"
  export VDOC_TEST_STORAGE_SECRET_KEY="$VDOC_STORAGE_SECRET_KEY"
  export VDOC_TEST_STORAGE_USE_SSL="$(env_or_default VDOC_TEST_STORAGE_USE_SSL false)"
  export VDOC_TEST_STORAGE_PATH_STYLE="$(env_or_default VDOC_TEST_STORAGE_PATH_STYLE true)"
}

live_compose() {
  local env_file="../.env"
  local check_only=0
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --env-file)
        if [ "$#" -lt 2 ]; then
          printf 'missing value for --env-file\n' >&2
          exit 2
        fi
        env_file="$2"
        shift 2
        ;;
      --check-only)
        check_only=1
        shift
        ;;
      -h|--help)
        usage
        return
        ;;
      *)
        printf 'unknown live-compose option: %s\n\n' "$1" >&2
        usage >&2
        exit 2
        ;;
    esac
  done

  read_env_file "$env_file"
  derive_live_env_from_compose
  require_live_compose_database_not_app
  require_live_env
  require_live_database_not_app
  if [ "$check_only" = "1" ]; then
    print_live_env_ok "live-compose check OK"
    printf '  VDOC_TEST_POSTGRES_DB=%s\n' "$VDOC_TEST_POSTGRES_DB"
    return
  fi
  run_live_tests
}

case "${1:-all}" in
  help|-h|--help)
    usage
    ;;
  happy)
    go test ./tests/e2e -run '^TestVdocV01EndToEnd$' -count=1 -v
    ;;
  failure)
    go test ./tests/e2e -run '^TestVdocV01FailureMatrix$' -count=1 -v
    ;;
  live)
    run_live_tests
    ;;
  live-check)
    require_live_env
    require_live_database_not_app
    print_live_env_ok "live-check OK"
    ;;
  live-compose)
    shift
    live_compose "$@"
    ;;
  all)
    go test ./tests/e2e -run '^(TestVdocV01EndToEnd|TestVdocV01FailureMatrix)$' -count=1 -v
    ;;
  *)
    printf 'unknown mode: %s\n\n' "$1" >&2
    usage >&2
    exit 2
    ;;
esac
