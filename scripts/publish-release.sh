#!/usr/bin/env bash
set -euo pipefail

tag="${1:?Usage: publish-release.sh RELEASE_TAG VERIFIED_ASSET_DIR}"
asset_dir="${2:?Usage: publish-release.sh RELEASE_TAG VERIFIED_ASSET_DIR}"
release_version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$'
[[ "$tag" =~ $release_version_pattern ]] || exit 1
repo="${GH_REPO:-${GITHUB_REPOSITORY:-}}"
[[ -n "$repo" ]] || { echo 'GitHub release publishing requires the repository' >&2; exit 1; }
[[ -d "$asset_dir" ]] || { echo 'Verified release asset directory is missing' >&2; exit 1; }
shopt -s nullglob dotglob
assets=("$asset_dir"/*)
[[ ${#assets[@]} -gt 0 ]] || { echo 'Verified release assets are empty' >&2; exit 1; }
for asset in "${assets[@]}"; do
  [[ -f "$asset" && ! -L "$asset" && "${asset##*/}" =~ ^[A-Za-z0-9._-]+$ ]] || {
    echo 'Verified release assets must be regular files with safe names' >&2
    exit 1
  }
done
prerelease=false
[[ "$tag" != *-* ]] || prerelease=true
stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT

# 只有成功读取整个 release 列表后，才能认定 tag 尚未发布；API 错误不等于不存在。
if ! gh api --paginate --slurp "repos/$repo/releases" >"$stage/releases.json" 2>"$stage/api-error"; then
  echo 'Could not read GitHub releases; refusing to create or reuse a release' >&2
  exit 1
fi
jq -e 'type == "array" and all(.[]; type == "array")' "$stage/releases.json" >/dev/null
jq --arg tag "$tag" '[.[][] | select(.tag_name == $tag)]' "$stage/releases.json" >"$stage/matching.json"
count="$(jq 'length' "$stage/matching.json")"
if [[ "$count" == 0 ]]; then
  options=(--verify-tag --generate-notes --title "$tag")
  [[ "$prerelease" == false ]] || options+=(--prerelease)
  gh release create "$tag" "${assets[@]}" --repo "$repo" "${options[@]}"
  exit 0
fi
[[ "$count" == 1 ]] && jq -e --arg tag "$tag" --argjson prerelease "$prerelease" \
  '.[0] | .tag_name == $tag and .draft == false and .prerelease == $prerelease and (.id | type == "number" and . > 0 and floor == .)' \
  "$stage/matching.json" >/dev/null || {
    echo 'Existing release tag, publication state or prerelease classification differs' >&2
    exit 1
  }
release_id="$(jq -r '.[0].id' "$stage/matching.json")"
if ! gh api --paginate --slurp "repos/$repo/releases/$release_id/assets" >"$stage/asset-pages.json" 2>"$stage/api-error"; then
  echo 'Could not read existing release assets; refusing to reuse the release' >&2
  exit 1
fi
jq -e 'type == "array" and all(.[]; type == "array")' "$stage/asset-pages.json" >/dev/null
jq '[.[][]]' "$stage/asset-pages.json" >"$stage/assets.json"
printf '%s\n' "${assets[@]##*/}" | jq -R . | jq -s sort >"$stage/local-names.json"
jq -e --slurpfile expected "$stage/local-names.json" \
  'all(.[]; .state == "uploaded" and (.id | type == "number" and . > 0 and floor == .)) and (map(.name) | sort) == $expected[0]' \
  "$stage/assets.json" >/dev/null || {
    echo 'Existing release asset names or upload state differ from the verified assets' >&2
    exit 1
  }
while IFS=$'\t' read -r asset_id name; do
  if ! gh api "repos/$repo/releases/assets/$asset_id" -H 'Accept: application/octet-stream' >"$stage/$asset_id" 2>"$stage/api-error"; then
    echo 'Could not download an existing release asset; refusing to reuse the release' >&2
    exit 1
  fi
  cmp -s "$asset_dir/$name" "$stage/$asset_id" || {
    printf 'Existing release asset differs from the verified asset: %s\n' "$name" >&2
    exit 1
  }
done < <(jq -r '.[] | [.id, .name] | @tsv' "$stage/assets.json")
printf 'Reusing published release %s; all verified assets are identical\n' "$tag"
