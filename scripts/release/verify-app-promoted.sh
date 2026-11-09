#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: bash scripts/release/verify-app-promoted.sh <tag> <git-rev>

For a final tag vX.Y.Z: fails when an App build source changed between the
commit that brought the App version at <git-rev> (nova/config.yaml) into
history and <git-rev>, or when nova/config.yaml changed beyond its version or
nova/version.json beyond skill_version. Land the App promote PR first
(docs/releasing.md → App Images). An rc tag vX.Y.Z-rcN is skipped: it runs on the landing commit,
before the bump. Needs full git history.
EOF
}

fail() {
  echo "[verify-app-promoted] ERROR: $*" >&2
  exit 1
}

[[ "$#" -eq 2 ]] || { usage; exit 2; }
tag="$1"
rev="$2"
semver='(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)'
if [[ "$tag" =~ ^v${semver}-rc[1-9][0-9]*$ ]]; then
  echo "[verify-app-promoted] skip: ${tag} is an rc tag"
  exit 0
fi
[[ "$tag" =~ ^v${semver}$ ]] || fail "tag must be vX.Y.Z or vX.Y.Z-rcN, got ${tag}"

# shellcheck source=app-image-lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/app-image-lib.sh"
git -C "$APP_IMAGE_ROOT" cat-file -e "${rev}^{commit}" 2>/dev/null || fail "${rev} is not a commit"
[[ "$(git -C "$APP_IMAGE_ROOT" rev-parse --is-shallow-repository)" == "false" ]] \
  || fail "needs full git history (checkout with fetch-depth: 0)"

version="$(app_config_version "$rev")"
[[ "$version" =~ ^${semver}$ ]] || fail "nova/config.yaml version at ${rev} is not X.Y.Z: '${version}'"
introduced="$(git -C "$APP_IMAGE_ROOT" log --reverse --format=%H \
  -G "^version:[[:space:]]*\"?${version//./\\.}\"?[[:space:]]*$" "$rev" -- nova/config.yaml)"
introduced="${introduced%%$'\n'*}"
[[ -n "$introduced" ]] || fail "no commit introduces App ${version} in nova/config.yaml"

paths=()
while IFS= read -r path; do paths+=("$path"); done < <(app_source_paths)
changed="$(git -C "$APP_IMAGE_ROOT" diff --name-only "$introduced" "$rev" -- "${paths[@]}")"
nl=$'\n'
changed_beyond() { # <file> <line pattern a release changes anyway>
  git -C "$APP_IMAGE_ROOT" diff -U0 "$introduced" "$rev" -- "$1" \
    | sed -nE "\#^(\+\+\+|---) #d; \#^[+-]$2#d; \#^[+-]#p"
}
[[ -z "$(changed_beyond nova/config.yaml 'version:')" ]] || changed="${changed:+${changed}${nl}}nova/config.yaml"
[[ -z "$(changed_beyond nova/version.json '[[:space:]]*"skill_version":')" ]] || changed="${changed:+${changed}${nl}}nova/version.json"
if [[ -n "$changed" ]]; then
  fail "App sources changed since App ${version} (${introduced:0:12}) without an App bump; land the App promote PR before ${tag}: $(tr '\n' ' ' <<<"$changed")"
fi
echo "[verify-app-promoted] OK: ${tag} ships App ${version}; no App source changed since ${introduced:0:12}"
