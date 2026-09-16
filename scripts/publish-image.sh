#!/usr/bin/env bash
set -euo pipefail

# 只发布同一工作流已验证的镜像归档；已有版本必须与本次内容一致。
ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
tag="${1:?Usage: publish-image.sh TAG RELEASE_DIRECTORY}"
assets="${2:?Usage: publish-image.sh TAG RELEASE_DIRECTORY}"
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$ ]] || exit 1
[[ -n "${GH_TOKEN:-}" && -n "${GITHUB_ACTOR:-}" && -n "${GITHUB_REPOSITORY:-}" ]] || {
  echo 'Registry publishing requires the release workflow credentials' >&2
  exit 1
}
commit="$(git -C "$ROOT_DIR" rev-parse HEAD)"
[[ "$(git -C "$ROOT_DIR" rev-parse "$tag^{commit}")" == "$commit" ]]
registry="ghcr.io/$(printf '%s' "$GITHUB_REPOSITORY" | tr '[:upper:]' '[:lower:]')"
image="vdoc-backend"
target="$registry:$tag"
stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
export DOCKER_CONFIG="$stage/auth"
mkdir -p "$DOCKER_CONFIG"
printf '%s' "$GH_TOKEN" | docker login ghcr.io --username "$GITHUB_ACTOR" --password-stdin >/dev/null

inspect_remote() {
  if docker buildx imagetools inspect "$1" --raw >"$stage/manifest.json" 2>"$stage/inspect.err"; then
    return 0
  fi
  if grep -Eqi 'manifest unknown|no such manifest|name unknown|not found' "$stage/inspect.err"; then
    return 1
  fi
  cat "$stage/inspect.err" >&2
  exit 1
}

for arch in amd64 arm64; do
  archive="${image}_${tag}_linux_${arch}.docker.tar.gz"
  (cd "$assets" && sha256sum --check "$archive.sha256")
  gzip -dc "$assets/$archive" | docker load >/dev/null
  local_id="$(docker image inspect "$image:$tag" --format '{{.Id}}')"
  [[ "$(docker image inspect "$local_id" --format '{{.Architecture}}')" == "$arch" ]]
  [[ "$(docker image inspect "$local_id" --format '{{index .Config.Labels "org.opencontainers.image.revision"}}')" == "$commit" ]]
  [[ "$(docker image inspect "$local_id" --format '{{index .Config.Labels "org.opencontainers.image.version"}}')" == "$tag" ]]
  remote="$target-$arch"
  if inspect_remote "$remote"; then
    docker pull --quiet --platform "linux/$arch" "$remote" >/dev/null
    [[ "$(docker image inspect "$remote" --format '{{.Id}}')" == "$local_id" ]] || {
      echo "Refusing to overwrite a different published image: $remote" >&2
      exit 1
    }
  else
    docker tag "$local_id" "$remote"
    docker push "$remote"
  fi
  docker buildx imagetools inspect "$remote" --format '{{.Manifest.Digest}}' >"$stage/$arch.digest"
done

jq -n --arg amd64 "$(cat "$stage/amd64.digest")" --arg arm64 "$(cat "$stage/arm64.digest")" \
  '[{os:"linux",architecture:"amd64",digest:$amd64},{os:"linux",architecture:"arm64",digest:$arm64}]' >"$stage/platforms.json"
if inspect_remote "$target"; then
  jq '[.manifests[] | {os:.platform.os,architecture:.platform.architecture,digest}] | sort_by(.architecture)' "$stage/manifest.json" >"$stage/existing.json"
  diff -u "$stage/platforms.json" "$stage/existing.json" || {
    echo "Refusing to overwrite a different published manifest: $target" >&2
    exit 1
  }
else
  docker buildx imagetools create --tag "$target" "$target-amd64" "$target-arm64"
fi

docker buildx imagetools inspect "$target" --raw |
  jq '[.manifests[] | {os:.platform.os,architecture:.platform.architecture,digest}] | sort_by(.architecture)' >"$stage/published.json"
diff -u "$stage/platforms.json" "$stage/published.json"
digest="$(docker buildx imagetools inspect "$target" --format '{{.Manifest.Digest}}')"
[[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]
jq -n --arg image "$target" --arg digest "$digest" --arg commit "$commit" \
  --slurpfile platforms "$stage/platforms.json" \
  '{image:$image,digest:$digest,commit:$commit,platforms:$platforms[0]}' >"$assets/container-image.json"
(cd "$assets" && sha256sum container-image.json >container-image.json.sha256)
printf 'Published %s@%s\n' "$target" "$digest"
