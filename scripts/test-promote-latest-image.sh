#!/usr/bin/env bash
set -euo pipefail

root="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
mkdir -p "$stage/bin"
cat >"$stage/bin/gh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
cat "$TEST_RELEASES_FILE"
SH
cat >"$stage/bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$TEST_DOCKER_CALLS"
case "$*" in
  'login '*) cat >/dev/null ;;
  'buildx imagetools inspect '*)
    if [[ "$*" == *'--raw' ]]; then
      if [[ -n "${TEST_PLATFORMS:-}" ]]; then
        printf '%s\n' "$TEST_PLATFORMS"
      else
        printf '%s\n' '{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}},{"platform":{"os":"linux","architecture":"arm64"}}]}'
      fi
    elif [[ "$4" == *:latest ]]; then
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
export TEST_RELEASES_FILE="$stage/releases.json" TEST_DOCKER_CALLS="$stage/docker.calls"
export TEST_DIGEST="sha256:$(printf 'a%.0s' {1..64})"

releases() {
  printf '%s\n' "$1" >"$TEST_RELEASES_FILE"
  : >"$TEST_DOCKER_CALLS"
}
fail() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }
run() { bash "$root/scripts/promote-latest-image.sh" "$1" >"$stage/output" 2>&1; }

releases '[[{"tag_name":"v0.9.9","draft":false,"prerelease":false}],[{"tag_name":"v0.10.0","draft":false,"prerelease":false},{"tag_name":"v1.0.0","draft":true,"prerelease":false},{"tag_name":"v2.0.0-rc.1","draft":false,"prerelease":true}]]'
run v0.10.0 || { cat "$stage/output"; fail 'newest stable release was rejected'; }
grep -Fq "create --tag ghcr.io/chnmig/test:latest ghcr.io/chnmig/test@$TEST_DIGEST" "$TEST_DOCKER_CALLS" || fail 'promotion did not use the verified index digest'

: >"$TEST_DOCKER_CALLS"
run v0.9.9 || fail 'older stable release should skip promotion'
[[ ! -s "$TEST_DOCKER_CALLS" ]] || fail 'older stable release touched the registry'
run v2.0.0-rc.1 || fail 'prerelease should skip promotion'
[[ ! -s "$TEST_DOCKER_CALLS" ]] || fail 'prerelease touched the registry'
if run v3.0.0; then fail 'unpublished release was accepted'; fi
[[ ! -s "$TEST_DOCKER_CALLS" ]] || fail 'unpublished release touched the registry'

export TEST_PLATFORMS='{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}}]}'
if run v0.10.0; then fail 'single-platform image was accepted'; fi
if grep -Fq 'imagetools create' "$TEST_DOCKER_CALLS"; then fail 'invalid platform image was promoted'; fi
unset TEST_PLATFORMS
export TEST_FINAL_DIGEST="sha256:$(printf 'b%.0s' {1..64})"
if run v0.10.0; then fail 'incorrect published digest was accepted'; fi
printf 'PASS: latest promotion, numeric version ordering, older/prerelease/unpublished guards, platform validation and digest verification\n'
