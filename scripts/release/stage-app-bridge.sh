#!/usr/bin/env bash
# Stages the chat-app bridge for one NOVA App image arch into <dir> (normally
# nova/bridge, copied to /opt/ha-nova by nova/Dockerfile): the ha-nova CLI for
# linux (no Cloud route: it reaches the Relay inside the container) plus the
# skills, both from THIS tree, so the image is version-locked to them.
#   bash scripts/release/stage-app-bridge.sh <amd64|aarch64> <dir>
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
[[ "$#" -eq 2 ]] || { echo "usage: stage-app-bridge.sh <amd64|aarch64> <dir>" >&2; exit 2; }
case "$1" in
  amd64) goarch=amd64 ;;
  aarch64) goarch=arm64 ;;
  *) echo "unsupported App arch: $1" >&2; exit 1 ;;
esac
out="$(cd "$2" && pwd)"
version="$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$ROOT/version.json" | head -1)"
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc[1-9][0-9]*)?$ ]] \
  || { echo "version.json skill_version '${version}' is not X.Y.Z[-rcN]" >&2; exit 1; }

rm -rf "$out/ha-nova" "$out/skills" "$out/version.json" "$out/bundle.json"
(cd "$ROOT/cli" && CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" \
  go build -trimpath -tags cloudremote_disabled -ldflags="-s -w -X main.Version=${version}" -o "$out/ha-nova" .)
cp -R "$ROOT/skills" "$out/skills"
cp "$ROOT/version.json" "$out/version.json"
# bundle.json marks /opt/ha-nova as the CLI's source root (skills/ next to it).
printf '{"bundle_format_version":1,"version":"%s","os":"linux","arch":"%s","binary_name":"ha-nova"}\n' \
  "$version" "$goarch" > "$out/bundle.json"
echo "staged ha-nova ${version} linux/${goarch} + skills in ${out}"
