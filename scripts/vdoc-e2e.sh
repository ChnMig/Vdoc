#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

require_live_env() {
  local missing=()
  local key
  for key in \
    VDOC_TEST_DATABASE_DSN \
    VDOC_TEST_STORAGE_ENDPOINT \
    VDOC_TEST_STORAGE_BUCKET \
    VDOC_TEST_STORAGE_ACCESS_KEY \
    VDOC_TEST_STORAGE_SECRET_KEY; do
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

case "${1:-all}" in
  happy)
    go test ./... -run '^TestVdocV01EndToEnd$' -count=1 -v
    ;;
  failure)
    go test ./... -run '^TestVdocV01FailureMatrix$' -count=1 -v
    ;;
  live)
    require_live_env
    VDOC_E2E_LIVE=1 go test ./... -run '^TestVdocV01EndToEndLivePersistence$' -count=1 -v
    ;;
  all)
    go test ./... -run '^(TestVdocV01EndToEnd|TestVdocV01FailureMatrix)$' -count=1 -v
    ;;
  *)
    printf 'usage: %s [happy|failure|live|all]\n' "$0" >&2
    exit 2
    ;;
esac
