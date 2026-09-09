#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
release_tag="${1:-}"
output_dir="${2:-$ROOT_DIR/dist}"
if [[ "$output_dir" != /* ]]; then
  output_dir="$ROOT_DIR/$output_dir"
fi
[[ "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || {
  printf 'Use make release-package RELEASE_TAG=vMAJOR.MINOR.PATCH (optionally with a prerelease suffix)\n' >&2
  exit 1
}

make -C "$ROOT_DIR" build-cross \
  DIST_DIR="$output_dir" \
  VERSION="$release_tag" \
  GIT_COMMIT="$(git -C "$ROOT_DIR" rev-parse HEAD)" \
  BUILD_TIME="$(git -C "$ROOT_DIR" show -s --format=%cI HEAD)"
(
  cd "$output_dir"
  shopt -s nullglob
  archives=(*.tar.gz *.zip)
  [[ ${#archives[@]} -gt 0 ]]
  shasum -a 256 "${archives[@]}" >SHA256SUMS
  shasum -a 256 -c SHA256SUMS
)
