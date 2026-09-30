#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
tag="${1:-}"
arch="${2:-}"
output="${3:-$ROOT_DIR/.artifacts/images}"
# SemVer without build metadata: '+' is not valid in a Docker image tag.
release_version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$'
[[ "$tag" =~ $release_version_pattern ]] || { echo 'Invalid release tag' >&2; exit 1; }
[[ "$arch" == amd64 || "$arch" == arm64 ]] || { echo 'Use amd64 or arm64' >&2; exit 1; }
[[ -z "$(git -C "$ROOT_DIR" status --porcelain=v1 --untracked-files=all)" ]] || { echo 'Commit source changes before packaging a release image' >&2; exit 1; }
commit="$(git -C "$ROOT_DIR" rev-parse HEAD)"
build_time="$(git -C "$ROOT_DIR" show -s --format=%cI HEAD)"
image="vdoc-backend:$tag"
mkdir -p "$output"
output="$(cd "$output" && pwd -P)"
docker build --platform "linux/$arch" --tag "$image" \
  --build-arg "VERSION=$tag" --build-arg "GIT_COMMIT=$commit" \
  --build-arg "BUILD_TIME=$build_time" "$ROOT_DIR"
docker run --rm --platform "linux/$arch" "$image" --version | grep -F "$commit"
[[ "$(docker image inspect "$image" --format '{{.Architecture}}')" == "$arch" ]]
archive="vdoc-backend_${tag}_linux_${arch}.docker.tar.gz"
docker save "$image" | gzip -n >"$output/$archive"
(cd "$output" && shasum -a 256 "$archive" >"$archive.sha256")
printf 'Prepared %s\n' "$output/$archive"
