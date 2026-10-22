#!/usr/bin/env bash
set -euo pipefail

root="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
mkdir -p "$stage/bin"
cat >"$stage/bin/gh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$TEST_GH_CALLS"
cat "$TEST_RELEASES_FILE"
SH
cat >"$stage/bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$TEST_DOCKER_CALLS"
# beta 渠道的所有测试均禁止访问或改写 latest。
[[ "$*" != *:latest* ]] || exit 1
case "$*" in
  'login '*) cat >/dev/null ;;
  'buildx imagetools inspect '*)
    if [[ "$*" == *'--raw' ]]; then
      if [[ -n "${TEST_PLATFORMS:-}" ]]; then
        printf '%s\n' "$TEST_PLATFORMS"
      else
        printf '%s\n' '{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}},{"platform":{"os":"linux","architecture":"arm64"}}]}'
      fi
    elif [[ "$4" == *:beta ]]; then
      printf '%s\n' "${TEST_FINAL_DIGEST:-$TEST_DIGEST}"
    else
      printf '%s\n' "$TEST_DIGEST"
    fi ;;
  'buildx imagetools create '*) ;;
  *) exit 1 ;;
esac
SH
chmod +x "$stage/bin/gh" "$stage/bin/docker"
export PATH="$stage/bin:$PATH"
export GH_TOKEN=disposable-test-token GITHUB_ACTOR=test GITHUB_REPOSITORY=ChnMig/Test
export TEST_RELEASES_FILE="$stage/releases.json" TEST_DOCKER_CALLS="$stage/docker.calls" TEST_GH_CALLS="$stage/gh.calls"
export TEST_DIGEST="sha256:$(printf 'a%.0s' {1..64})"
unset TEST_PLATFORMS TEST_FINAL_DIGEST

releases() {
  printf '%s\n' "$1" >"$TEST_RELEASES_FILE"
  : >"$TEST_DOCKER_CALLS"
  : >"$TEST_GH_CALLS"
}
fail() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }
run() { bash "$root/scripts/promote-beta-image.sh" "$1" >"$stage/output" 2>&1; }
promoted() {
  run "$1" || { cat "$stage/output"; fail "beta promotion rejected $1"; }
  grep -Fq "create --tag ghcr.io/chnmig/test:beta ghcr.io/chnmig/test@$TEST_DIGEST" "$TEST_DOCKER_CALLS" || fail 'promotion did not use the verified index digest'
  if grep -Fq ':latest' "$TEST_DOCKER_CALLS"; then fail 'beta touched latest'; fi
}
no_registry() { [[ ! -s "$TEST_DOCKER_CALLS" ]] || fail "$1 touched the registry"; }
not_promoted() {
  if grep -Fq 'imagetools create' "$TEST_DOCKER_CALLS"; then fail "$1 was promoted"; fi
}

# 合法的稳定版及其他预发布渠道在读取 release 或 registry 前直接跳过。
releases '[]'
for tag in v1.2.3 v1.2.3-rc.1 v1.2.3-alpha.1 v1.2.3-beta v1.2.3-beta.-1 v1.2.3-beta.1.extra; do
  run "$tag" || fail "valid non-beta release $tag should skip promotion"
  no_registry "$tag"
  [[ ! -s "$TEST_GH_CALLS" ]] || fail 'non-beta release queried GitHub'
done
env -u GH_TOKEN -u GITHUB_ACTOR -u GITHUB_REPOSITORY bash "$root/scripts/promote-beta-image.sh" v1.2.3 >"$stage/output" 2>&1 || fail 'stable skip required credentials'

for tag in v01.2.3-beta.1 v1.02.3-beta.1 v1.2.03-beta.1 v1.2.3-beta.01 v1.2.3-beta.1+build 1.2.3-beta.1 v1.2.3-beta..1; do
  if run "$tag"; then fail "invalid version $tag was accepted"; fi
  no_registry "$tag"
  [[ ! -s "$TEST_GH_CALLS" ]] || fail 'invalid version queried GitHub'
done

releases '[[{"tag_name":"v0.10.0-beta.9","draft":false,"prerelease":true}],[{"tag_name":"v0.10.0-beta.10","draft":false,"prerelease":true},{"tag_name":"v9.0.0","draft":false,"prerelease":false},{"tag_name":"v10.0.0-rc.1","draft":false,"prerelease":true},{"tag_name":"v11.0.0-beta.1","draft":true,"prerelease":true},{"tag_name":"v12.0.0-beta.1","draft":false,"prerelease":false}]]'
promoted v0.10.0-beta.10
: >"$TEST_DOCKER_CALLS"
bash "$root/scripts/promote-latest-image.sh" v0.10.0-beta.10 >"$stage/output" 2>&1 || fail 'latest did not skip a beta prerelease'
no_registry 'beta release in latest promotion'
run v0.10.0-beta.9 || fail 'older beta should skip promotion'
no_registry 'older beta'
for tag in v11.0.0-beta.1 v12.0.0-beta.1 v13.0.0-beta.1; do
  if run "$tag"; then fail "draft, misclassified or unpublished beta $tag was accepted"; fi
  no_registry "$tag"
done

for pair in 'v0.9.9-beta.99 v0.10.0-beta.0' 'v1.2.9-beta.99 v1.2.10-beta.0' 'v9.99.99-beta.99 v10.0.0-beta.0' 'v1.0.0-beta.9007199254740992 v1.0.0-beta.9007199254740993'; do
  read -r old_tag new_tag <<<"$pair"
  releases "[[{\"tag_name\":\"$new_tag\",\"draft\":false,\"prerelease\":true},{\"tag_name\":\"$old_tag\",\"draft\":false,\"prerelease\":true}]]"
  run "$old_tag" || fail 'older base version should skip promotion'
  no_registry 'older base version'
  promoted "$new_tag"
done

releases '[[{"tag_name":"v1.2.3-beta.0","draft":false,"prerelease":true}]]'
export TEST_PLATFORMS='{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}}]}'
if run v1.2.3-beta.0; then fail 'single-platform image was accepted'; fi
not_promoted 'single-platform image'
releases '[[{"tag_name":"v1.2.3-beta.0","draft":false,"prerelease":true}]]'
export TEST_PLATFORMS='{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}},{"platform":{"os":"windows","architecture":"arm64"}}]}'
if run v1.2.3-beta.0; then fail 'non-Linux arm64 image was accepted'; fi
not_promoted 'non-Linux arm64 image'
unset TEST_PLATFORMS

releases '[[{"tag_name":"v1.2.3-beta.0","draft":false,"prerelease":true}]]'
export TEST_DIGEST=invalid-digest
if run v1.2.3-beta.0; then fail 'invalid released digest was accepted'; fi
not_promoted 'invalid digest image'
export TEST_DIGEST="sha256:$(printf 'a%.0s' {1..64})"
releases '[[{"tag_name":"v1.2.3-beta.0","draft":false,"prerelease":true}]]'
export TEST_FINAL_DIGEST="sha256:$(printf 'b%.0s' {1..64})"
if run v1.2.3-beta.0; then fail 'incorrect published beta digest was accepted'; fi
if grep -Fq ':latest' "$TEST_DOCKER_CALLS"; then fail 'beta touched latest'; fi
printf 'PASS: beta isolation, numeric ordering, older/draft/unpublished/misclassified guards, platform validation and digest verification\n'
