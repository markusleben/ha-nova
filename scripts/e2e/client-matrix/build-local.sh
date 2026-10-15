#!/usr/bin/env bash
# Build release-shaped install bundles from this checkout's committed HEAD with
# the repo's own release script (scripts/release/build-install-bundle.sh).
#
#   build-local.sh <out-dir> <from-version>     prints the bundle version
#
# - Source = `git archive HEAD` (uncommitted edits are NOT included).
# - When version.json is not newer than <from-version>, the copy is stamped to
#   the next patch with scripts/bump-version.sh (as a release-prep PR would),
#   so `ha-nova update --version` really swaps the runtime.
# - Binaries: the release flags (-tags cloudremote_official, -X main.Version).
# - Cloud evidence is signed with a throwaway Ed25519 key, so Cloud provenance
#   never verifies and Cloud Remote stays off in these bundles (by design: the
#   lanes test the local relay path only).
set -euo pipefail

OUT="${1:?out dir}"
FROM="${2:?from version}"
FROM="${FROM#v}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SRC="$OUT/src"
DIST="$OUT/dist"

mkdir -p "$SRC" "$DIST"
git -C "$ROOT" archive HEAD | tar -x -C "$SRC"
# The release script derives provenance from HEAD^{tree}; give the copy one.
git -C "$SRC" init -q
git -C "$SRC" add -A
git -C "$SRC" -c user.name=client-matrix -c user.email=client-matrix@invalid \
  commit -qm "client-matrix local build"

VERSION="$(node -e '
  const [cur, from] = process.argv.slice(1).map((s) => s.replace(/-.*/, "").split(".").map(Number));
  const newer = cur[0] - from[0] || cur[1] - from[1] || cur[2] - from[2];
  process.stdout.write(newer > 0 ? cur.join(".") : `${from[0]}.${from[1]}.${from[2] + 1}`);
' "$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$SRC/version.json")" "$FROM")"
# Stamp like a release-prep PR would (version.json, plugin + marketplace manifests).
bash "$SRC/scripts/bump-version.sh" "$VERSION" >&2

for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64; do
  os="${target%/*}" arch="${target#*/}" suffix=""
  [[ "$os" == windows ]] && suffix=".exe"
  (cd "$SRC/cli" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -tags cloudremote_official \
      -ldflags="-s -w -X main.Version=${VERSION}" \
      -o "$DIST/ha-nova-${os}-${arch}${suffix}" .) >&2
done

# ponytail: throwaway key, never the production signing secret.
HA_NOVA_CLOUD_RELEASE_SIGNING_KEY_PEM="$(node -e '
  const { generateKeyPairSync } = require("node:crypto");
  process.stdout.write(generateKeyPairSync("ed25519").privateKey.export({ type: "pkcs8", format: "pem" }));
')" DIST_DIR="$DIST" bash "$SRC/scripts/release/build-install-bundle.sh" "$VERSION" >&2

printf '%s\n' "$VERSION"
