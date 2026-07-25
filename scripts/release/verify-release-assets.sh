#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "[verify-release-assets] ERROR: $*" >&2
  exit 1
}

if [[ "$#" -ne 1 || ! "$1" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc[1-9][0-9]*)?$ ]]; then
  fail "usage: bash scripts/release/verify-release-assets.sh <vX.Y.Z[-rcN]>"
fi

command -v gh >/dev/null 2>&1 || fail "required command not found: gh"
command -v jq >/dev/null 2>&1 || fail "required command not found: jq"

tag="$1"
repository="markusleben/ha-nova"
release_json="$(gh release view "$tag" --repo "$repository" --json tagName,isDraft,isPrerelease,assets)" \
  || fail "could not read release ${repository}@${tag}"

required_assets=(
  checksums.txt
  ha-nova-darwin-amd64
  ha-nova-darwin-arm64
  ha-nova-linux-amd64
  ha-nova-linux-arm64
  ha-nova-windows-amd64.exe
  ha-nova-installer-bundle-linux-amd64.tar.gz
  ha-nova-installer-bundle-linux-amd64.tar.gz.sha256
  ha-nova-installer-bundle-linux-arm64.tar.gz
  ha-nova-installer-bundle-linux-arm64.tar.gz.sha256
  ha-nova-installer-bundle-macos-amd64.tar.gz
  ha-nova-installer-bundle-macos-amd64.tar.gz.sha256
  ha-nova-installer-bundle-macos-arm64.tar.gz
  ha-nova-installer-bundle-macos-arm64.tar.gz.sha256
  ha-nova-installer-bundle-windows-amd64.zip
  ha-nova-installer-bundle-windows-amd64.zip.sha256
)
expected_json="$(printf '%s\n' "${required_assets[@]}" | jq -R . | jq -s 'sort')"

jq -e \
  --arg tag "$tag" \
  --argjson expected "$expected_json" '
    .tagName == $tag
    and (.assets | length) == ($expected | length)
    and ([.assets[].name] | sort) == $expected
    and all(.assets[];
      .state == "uploaded"
      and (.size | type == "number" and . > 0)
      and (.digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
    )
  ' >/dev/null <<<"$release_json" \
  || fail "release ${repository}@${tag} does not contain the exact complete verified asset set"

echo "[verify-release-assets] OK: ${repository}@${tag} (${#required_assets[@]} complete SHA-256-attested assets)"
