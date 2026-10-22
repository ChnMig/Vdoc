#!/usr/bin/env bash
set -euo pipefail

root="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
mkdir -p "$stage/bin" "$stage/verified" "$stage/remote"
cat >"$stage/bin/gh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$TEST_GH_CALLS"
if [[ "$1" == api ]]; then
  endpoint=""
  for argument in "${@:2}"; do
    [[ "$argument" != repos/* ]] || endpoint="$argument"
  done
  if [[ "${TEST_API_ERROR:-}" == all || "${TEST_API_ERROR:-}" == "$endpoint" ]]; then
    echo 'API request failed' >&2
    exit 1
  fi
  case "$endpoint" in
    repos/ChnMig/Test/releases) cat "$TEST_RELEASES_FILE" ;;
    repos/ChnMig/Test/releases/101/assets) cat "$TEST_ASSETS_FILE" ;;
    repos/ChnMig/Test/releases/assets/*) cat "$TEST_REMOTE_DIR/${endpoint##*/}" ;;
    *) exit 1 ;;
  esac
elif [[ "$1 $2" == 'release create' ]]; then
  printf 'Created %s\n' "$3"
else
  echo 'Unexpected release mutation' >&2
  exit 1
fi
SH
chmod +x "$stage/bin/gh"
export PATH="$stage/bin:$PATH"
export GH_TOKEN=disposable-test-token GH_REPO=ChnMig/Test
export TEST_RELEASES_FILE="$stage/releases.json" TEST_ASSETS_FILE="$stage/assets.json"
export TEST_GH_CALLS="$stage/gh.calls" TEST_REMOTE_DIR="$stage/remote"
unset TEST_API_ERROR
printf 'verified binary\n' >"$stage/verified/vdoc.tar.gz"
printf 'verified checksum\n' >"$stage/verified/SHA256SUMS"
cp "$stage/verified/vdoc.tar.gz" "$stage/remote/1"
cp "$stage/verified/SHA256SUMS" "$stage/remote/2"

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  [[ ! -f "$stage/output" ]] || cat "$stage/output" >&2
  [[ ! -f "$TEST_GH_CALLS" ]] || cat "$TEST_GH_CALLS" >&2
  exit 1
}
run() { bash "$root/scripts/publish-release.sh" "$1" "$stage/verified" >"$stage/output" 2>&1; }
reset_calls() { : >"$TEST_GH_CALLS"; }
no_mutation() {
  if grep -Eq '^release (create|edit|upload|delete)|--method (POST|PATCH|PUT|DELETE)| -X ' "$TEST_GH_CALLS"; then
    fail "$1 modified an existing release"
  fi
}
existing() {
  printf '[[{"id":101,"tag_name":"%s","draft":false,"prerelease":%s}]]\n' "$1" "$2" >"$TEST_RELEASES_FILE"
  printf '%s\n' '[[{"id":1,"name":"vdoc.tar.gz","state":"uploaded"}],[{"id":2,"name":"SHA256SUMS","state":"uploaded"}]]' >"$TEST_ASSETS_FILE"
  reset_calls
}
rejected() {
  if run "$1"; then fail "$2 was accepted"; fi
  no_mutation "$2"
}

for tag in v1.2.3 v1.2.3-beta.0 v1.2.3-rc.1; do
  printf '%s\n' '[[]]' >"$TEST_RELEASES_FILE"
  reset_calls
  run "$tag" || { cat "$stage/output"; fail "fresh release $tag was rejected"; }
  grep -Fq "release create $tag " "$TEST_GH_CALLS" || fail 'fresh release did not create the expected tag'
  grep -Fq " --repo ChnMig/Test --verify-tag --generate-notes --title $tag" "$TEST_GH_CALLS" || fail 'fresh release options differ'
  for asset in SHA256SUMS vdoc.tar.gz; do
    grep -Fq " $stage/verified/$asset " "$TEST_GH_CALLS" || fail 'fresh release omitted a verified asset'
  done
  if [[ "$tag" == *-* ]]; then
    grep -Fq -- '--prerelease' "$TEST_GH_CALLS" || fail 'fresh prerelease lacks prerelease flag'
  elif grep -Fq -- '--prerelease' "$TEST_GH_CALLS"; then
    fail 'fresh stable release was marked prerelease'
  fi
done

for pair in 'v1.2.3 false' 'v1.2.3-beta.1 true'; do
  read -r tag prerelease <<<"$pair"
  existing "$tag" "$prerelease"
  run "$tag" || { cat "$stage/output"; fail 'identical published release was not reusable'; }
  grep -Fq 'Reusing published release' "$stage/output" || fail 'existing release did not finish recovery'
  no_mutation 'identical existing release'
  grep -Fq 'repos/ChnMig/Test/releases/assets/1 -H Accept: application/octet-stream' "$TEST_GH_CALLS" || fail 'existing asset bytes were not checked'
  grep -Fq 'repos/ChnMig/Test/releases/assets/2 -H Accept: application/octet-stream' "$TEST_GH_CALLS" || fail 'all existing asset bytes were not checked'
done

existing v1.2.3-beta.1 true
printf 'verified binarx\n' >"$stage/remote/1"
rejected v1.2.3-beta.1 'asset byte drift'
cp "$stage/verified/vdoc.tar.gz" "$stage/remote/1"
for classification in false null; do
  existing v1.2.3-beta.1 "$classification"
  rejected v1.2.3-beta.1 'beta prerelease classification drift'
done
existing v1.2.3 true
rejected v1.2.3 'stable prerelease classification drift'
existing v1.2.3-beta.1 true
jq '.[0][0].draft = true' "$TEST_RELEASES_FILE" >"$stage/changed.json"
mv "$stage/changed.json" "$TEST_RELEASES_FILE"
rejected v1.2.3-beta.1 'draft release'

for variant in missing extra incomplete; do
  existing v1.2.3-beta.1 true
  case "$variant" in
    missing) jq '.[1] = []' "$TEST_ASSETS_FILE" ;;
    extra) jq '.[1] += [{"id":3,"name":"extra.txt","state":"uploaded"}]' "$TEST_ASSETS_FILE" ;;
    incomplete) jq '.[0][0].state = "new"' "$TEST_ASSETS_FILE" ;;
  esac >"$stage/changed.json"
  mv "$stage/changed.json" "$TEST_ASSETS_FILE"
  rejected v1.2.3-beta.1 "$variant release asset"
done

for endpoint in all repos/ChnMig/Test/releases/101/assets repos/ChnMig/Test/releases/assets/1; do
  existing v1.2.3-beta.1 true
  export TEST_API_ERROR="$endpoint"
  rejected v1.2.3-beta.1 'API, authentication or network error'
done
unset TEST_API_ERROR
printf 'not JSON\n' >"$TEST_RELEASES_FILE"
reset_calls
rejected v1.2.3-beta.1 'malformed API response'
reset_calls
rejected v1.2.3-beta.01 'invalid release tag'
[[ ! -s "$TEST_GH_CALLS" ]] || fail 'invalid tag called GitHub'
printf 'PASS: fresh stable/beta/rc creation, identical release recovery, asset/classification/draft/API guards and no overwrites\n'
