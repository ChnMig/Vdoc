#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

failures=0

pass() {
  printf 'PASS %s\n' "$1"
}

fail() {
  printf 'FAIL %s\n' "$1"
  failures=$((failures + 1))
}

check_absent() {
  local name="$1"
  local pattern="$2"
  shift 2
  local output
  if output=$(rg -n "$pattern" "$@" 2>/dev/null); then
    printf '%s\n' "$output"
    fail "$name"
  else
    pass "$name"
  fi
}

check_allowed_paths() {
  local name="$1"
  local pattern="$2"
  shift 2
  local output line path bad
  output=$(rg -n "$pattern" "$@" 2>/dev/null || true)
  bad=""
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    path="${line%%:*}"
    case "$path" in
      db/migrations.go|./db/migrations.go|db/pgdb/*|./db/pgdb/*)
        ;;
      *)
        bad+="$line"$'\n'
        ;;
    esac
  done <<< "$output"
  if [ -n "$bad" ]; then
    printf '%s' "$bad"
    fail "$name"
    return
  fi
  pass "$name"
  if [ -n "$output" ]; then
    printf 'Allowed matches for %s:\n%s\n' "$name" "$output"
  fi
}

check_allowed_sql_execution() {
  local output line path bad
  output=$(rg -n "\.(Exec|Raw)\(" . --glob '*.go' --glob '!**/*_test.go' 2>/dev/null || true)
  bad=""
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    path="${line%%:*}"
    case "$path" in
      db/migrations.go|./db/migrations.go|db/pgdb/vdoc/repo.go|./db/pgdb/vdoc/repo.go)
        ;;
      *)
        bad+="$line"$'\n'
        ;;
    esac
  done <<< "$output"
  if [ -n "$bad" ]; then
    printf '%s' "$bad"
    fail "raw SQL execution limited to migrations and GORM repository"
    return
  fi
  pass "raw SQL execution limited to migrations and GORM repository"
  if [ -n "$output" ]; then
    printf 'Allowed raw SQL matches:\n%s\n' "$output"
  fi
}

check_absent "api/app forbidden DB imports" 'gorm\.io|vdoc/db/pgdb|database/sql|github\.com/jackc/pgx' api/app --glob '*.go' --glob '!**/*_test.go'
check_absent "runtime vdoc_state dependency" 'vdoc_state' api domain services/vdoc db --glob '*.go' --glob '*.sql' --glob '!**/*_test.go'
check_absent "transport/domain/background runtime direct DB access" 'gorm\.Open|sql\.Open|pgdb\.Open|vdoc/db/pgdb|database/sql|github\.com/jackc/pgx' api/app services/vdoc domain --glob '*.go' --glob '!**/*_test.go'
check_allowed_paths "database/sql and pgx confined to db boundaries" 'database/sql|github\.com/jackc/pgx|\bpgx\b' . --glob '*.go' --glob '!**/*_test.go'
check_allowed_sql_execution
check_absent "v0.2 MCP publish tools not exposed" 'publish_api_schema|publish_api_version' api/app/v1/open/mcp docs/api/openapi.yaml --glob '!**/*_test.go'
check_absent "runtime TODO/FIXME/HACK markers" 'TODO|FIXME|HACK' api domain services/vdoc db/pgdb --glob '*.go' --glob '!**/*_test.go'
check_absent "stale prototype/scaffold docs wording" 'prototype|scaffold|脚手架' README.md README.zh-CN.md IMPROVEMENTS.md IMPROVEMENTS.zh-CN.md docs/api

go test ./services/vdoc -run 'TestInitDefaultStore(UsesInMemoryStoreWhenDatabaseDisabled|RequiresRepositoryWhenDatabaseEnabled)|TestDatabaseEnabledDefaultStoreRefreshesFromRepository|TestPostgresPersistenceSourceDoesNotUsePrototypeStateTable' -count=1 -v
go test ./db/pgdb/vdoc -run TestRepositorySourceDoesNotUsePrototypeStateTable -count=1 -v
go test ./api/app/v1/open -run TestOpenAPISpecMatchesRegisteredRoutes -count=1 -v

if [ "$failures" -ne 0 ]; then
  printf 'Layering audit failed with %d issue(s).\n' "$failures"
  exit 1
fi

printf 'Layering audit passed.\n'
