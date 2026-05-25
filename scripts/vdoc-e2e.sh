#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

case "${1:-all}" in
  happy)
    go test ./... -run TestVdocV01EndToEnd -count=1 -v
    ;;
  failure)
    go test ./... -run TestVdocV01FailureMatrix -count=1 -v
    ;;
  live)
    VDOC_E2E_LIVE=1 go test ./... -run TestVdocV01EndToEndLivePersistence -count=1 -v
    ;;
  all)
    go test ./... -run 'TestVdocV01(EndToEnd|FailureMatrix)' -count=1 -v
    ;;
  *)
    printf 'usage: %s [happy|failure|live|all]\n' "$0" >&2
    exit 2
    ;;
esac
