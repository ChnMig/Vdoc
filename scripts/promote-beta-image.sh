#!/usr/bin/env bash
set -euo pipefail

tag="${1:?Usage: promote-beta-image.sh RELEASE_TAG}"
# SemVer without build metadata: '+' is not valid in a Docker image tag.
release_version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$'
beta_version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-beta\.(0|[1-9][0-9]*)$'
[[ "$tag" =~ $release_version_pattern ]] || exit 1
if [[ ! "$tag" =~ $beta_version_pattern ]]; then
  printf 'Skipping beta for release %s\n' "$tag"
  exit 0
fi
[[ -n "${GH_TOKEN:-}" && -n "${GITHUB_ACTOR:-}" && -n "${GITHUB_REPOSITORY:-}" ]] || {
  echo 'Registry promotion requires the release workflow credentials' >&2
  exit 1
}

# 与发布 job 的仓库级串行锁配合，旧 beta 补发或重跑不能让 beta 倒退。
releases="$(gh api --paginate --slurp "repos/$GITHUB_REPOSITORY/releases")"
beta="$(jq --arg pattern "$beta_version_pattern" '[.[][] | select(.draft == false and .prerelease == true) | select(.tag_name | test($pattern))]' <<<"$releases")"
jq -e --arg tag "$tag" 'any(.[]; .tag_name == $tag)' <<<"$beta" >/dev/null || {
  echo 'Only an already published beta prerelease can become beta' >&2
  exit 1
}
# 无前导零的整数按长度、再按字典序比较，避免大版本号的浮点精度损失。
newest="$(jq -r 'sort_by(.tag_name | ltrimstr("v") | sub("-beta\\."; ".") | split(".") | map([length, .])) | last | .tag_name' <<<"$beta")"
if [[ "$tag" != "$newest" ]]; then
  printf 'Skipping older beta release %s; newest beta release is %s\n' "$tag" "$newest"
  exit 0
fi

registry="ghcr.io/$(printf '%s' "$GITHUB_REPOSITORY" | tr '[:upper:]' '[:lower:]')"
stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
export DOCKER_CONFIG="$stage/auth"
mkdir -p "$DOCKER_CONFIG"
printf '%s' "$GH_TOKEN" | docker login ghcr.io --username "$GITHUB_ACTOR" --password-stdin >/dev/null
digest="$(docker buildx imagetools inspect "$registry:$tag" --format '{{.Manifest.Digest}}')"
[[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo 'Released image has no valid index digest' >&2
  exit 1
}
docker buildx imagetools inspect "$registry@$digest" --raw |
  jq -e '[.manifests[] | select(.platform.os == "linux") | .platform.architecture] | sort == ["amd64", "arm64"]' >/dev/null || {
    echo 'Released image must support Linux amd64 and arm64' >&2
    exit 1
  }
docker buildx imagetools create --tag "$registry:beta" "$registry@$digest"
[[ "$(docker buildx imagetools inspect "$registry:beta" --format '{{.Manifest.Digest}}')" == "$digest" ]] || {
  echo 'Published beta digest differs from the beta release' >&2
  exit 1
}
printf 'Promoted %s:beta to %s (%s)\n' "$registry" "$tag" "$digest"
