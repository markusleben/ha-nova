#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
OUTPUT_DIR="${DIST_DIR}/winget"
RAW_VERSION="${1:-$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${ROOT_DIR}/version.json" | head -1)}"
REPO_SLUG="${2:-markusleben/ha-nova}"
PACKAGE_IDENTIFIER="${PACKAGE_IDENTIFIER:-markusleben.ha-nova}"
WINDOWS_BUNDLE_NAME="ha-nova-installer-bundle-windows-amd64.zip"
WINDOWS_BUNDLE_PATH="${DIST_DIR}/install-bundles/${WINDOWS_BUNDLE_NAME}"
WINDOWS_BUNDLE_SHA_PATH="${WINDOWS_BUNDLE_PATH}.sha256"
UPSTREAM_REPO="${UPSTREAM_REPO:-microsoft/winget-pkgs}"
FORK_REPO="${FORK_REPO:-markusleben/winget-pkgs}"
UPSTREAM_BASE_BRANCH="${UPSTREAM_BASE_BRANCH:-master}"

normalize_version() {
  local raw="$1"
  printf '%s\n' "${raw#v}"
}

VERSION="$(normalize_version "${RAW_VERSION}")"
RELEASE_TAG="${3:-v${VERSION}}"
ARCHIVE_PATH="${OUTPUT_DIR}/ha-nova-winget-manifest-v${VERSION}.zip"
STAGE_ROOT="${OUTPUT_DIR}/submission/${PACKAGE_IDENTIFIER}/${VERSION}"
CHECKLIST_PATH="${STAGE_ROOT}/winget-pkgs-maintainer-checklist.md"
PR_BODY_PATH="${STAGE_ROOT}/winget-pkgs-pr-body.md"
COPY_PATH_FILE="${STAGE_ROOT}/winget-pkgs-copy-path.txt"
COMMANDS_PATH="${STAGE_ROOT}/winget-pkgs-gh-commands.md"

log() {
  echo "[prepare-winget-pkgs-submission] $*"
}

die() {
  echo "[prepare-winget-pkgs-submission] ERROR: $*" >&2
  exit 1
}

compute_sha256() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print toupper($1)}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file}" | awk '{print toupper($1)}'
    return 0
  fi
  die "sha256sum or shasum is required to verify the staged winget submission."
}

manifest_root() {
  local first_segment package_suffix
  first_segment="${PACKAGE_IDENTIFIER%%.*}"
  first_segment="${first_segment,,}"
  first_segment="${first_segment:0:1}"
  package_suffix="${PACKAGE_IDENTIFIER#*.}"
  printf '%s/manifests/%s/%s/%s/%s\n' "${STAGE_ROOT}" "${first_segment}" "${PACKAGE_IDENTIFIER%%.*}" "${package_suffix}" "${VERSION}"
}

write_artifacts() {
  local manifest_dir="$1"
  local installer_url="$2"
  local installer_sha="$3"
  local upstream_copy_path pr_branch fork_owner fork_dir
  local manifest_dir_placeholder pr_body_placeholder commands_placeholder

  upstream_copy_path="${manifest_dir#"${STAGE_ROOT}/"}"
  pr_branch="ha-nova-${VERSION}"
  fork_owner="${FORK_REPO%%/*}"
  fork_dir="${FORK_REPO##*/}"
  manifest_dir_placeholder="<staged-submission-root-on-your-host>/${upstream_copy_path}"
  pr_body_placeholder="<staged-submission-root-on-your-pr-host>/winget-pkgs-pr-body.md"
  commands_placeholder="<staged-submission-root-on-your-pr-host>/winget-pkgs-gh-commands.md"

  cat > "${COPY_PATH_FILE}" <<EOF
${upstream_copy_path}
EOF

  cat > "${PR_BODY_PATH}" <<EOF
## Summary

- Add ${PACKAGE_IDENTIFIER} version ${VERSION}
- Source release tag: ${RELEASE_TAG}
- Source installer URL: ${installer_url}

## Validation

- [ ] \`winget validate --manifest "<staged-manifest-dir-on-your-validation-host>"\` completed on Windows without warnings
- [x] Installer SHA256 matches both the generated release manifest and the staged Windows bundle bytes
- [ ] Initial published-source install/check-update/uninstall smoke will run after merge/publication
- [ ] Upgrade continuity smoke will run from an older published version once that lane exists

## Notes

- Portable install sourced from the tagged HA NOVA Windows bundle
- Public docs will stay on \`install.ps1\` until the published-source smoke passes
EOF

  cat > "${CHECKLIST_PATH}" <<EOF
# Winget Maintainer Checklist

Version: ${VERSION}
Package: ${PACKAGE_IDENTIFIER}
Release tag: ${RELEASE_TAG}

## Stage

1. Confirm the staged manifest root exists:
   \`${manifest_dir_placeholder}\`
2. Confirm the upstream copy path:
   \`${upstream_copy_path}\`
3. Confirm the installer URL points at the tagged release asset:
   \`${installer_url}\`
4. Confirm the installer SHA256 is present:
   \`${installer_sha}\`

## Validate

1. Run on Windows:
   \`winget validate --manifest "<staged-manifest-dir-on-your-validation-host>"\`
2. Treat warnings as failures for this handoff; rebuild the manifest from the release asset before opening any PR.

## Open PR

1. Fork \`microsoft/winget-pkgs\`.
2. Copy the staged files to:
   \`${upstream_copy_path}\`
3. Open a PR against \`${UPSTREAM_REPO}\` \`${UPSTREAM_BASE_BRANCH}\`.
4. Use the staged PR body from:
   \`${pr_body_placeholder}\`
5. Or use the exact command sequence from:
   \`${commands_placeholder}\`

## Wait For Publication

1. Wait until the package is visible from the public source:
   \`winget show --id ${PACKAGE_IDENTIFIER} --exact --source winget\`
2. Confirm the expected version is shown before any public doc flip.

## Initial Published-Source Proof

Required before the first public Windows doc flip.

Use a fresh Windows VM and keep it source-clean:
- no local harness
- no local manifest path
- no \`HA_NOVA_BUNDLE_URL\`
- no \`HA_NOVA_BUNDLE_SHA256_URL\`
- no \`HA_NOVA_VERSION\`
- do not enable or rely on \`LocalManifestFiles\`

Then run:
1. \`winget install --id ${PACKAGE_IDENTIFIER} --exact\`
2. \`ha-nova check-update\`
3. \`winget uninstall --id ${PACKAGE_IDENTIFIER} --exact\`
4. confirm \`ha-nova\` no longer resolves from the removed install

## Upgrade Continuity Proof

Required once an older published ${PACKAGE_IDENTIFIER} version exists.

Use a separate Windows snapshot with the previous published version already installed, then run:
1. \`winget show --id ${PACKAGE_IDENTIFIER} --exact --source winget\`
2. \`winget upgrade --id ${PACKAGE_IDENTIFIER} --exact\`
3. \`ha-nova check-update\`
4. \`winget uninstall --id ${PACKAGE_IDENTIFIER} --exact\`

If this is the first public \`winget\` publication and no older published version exists yet, record the upgrade continuity lane as pending and re-run it on the next published \`winget\` release before calling public \`winget upgrade\` proven.

## Public Flip

Only after the initial published-source proof passes:
1. switch public Windows docs from \`install.ps1\` to \`winget\` as the primary path
2. keep \`install.ps1\` as the fallback/recovery path
3. keep public \`winget upgrade\` wording conservative until the upgrade continuity proof exists
4. remove any active wording that still says the package is not live
EOF

  cat > "${COMMANDS_PATH}" <<EOF
# Winget GitHub Command Sequence

Set the staged submission root on the machine that will open the PR.
If Windows validation ran on a different host, copy the entire staged submission folder there first.

Then use either shell variant below after \`winget validate --manifest "<staged-manifest-dir-on-your-validation-host>"\` succeeds without warnings.

\`\`\`bash
STAGED_ROOT="<set-this-to-your-staged-submission-root>"
MANIFEST_DIR="\$STAGED_ROOT/${upstream_copy_path}"
PR_BODY="\$STAGED_ROOT/winget-pkgs-pr-body.md"

gh repo clone ${FORK_REPO}
cd ${fork_dir}
git switch -C ${pr_branch}
mkdir -p ${upstream_copy_path}
cp "\$MANIFEST_DIR"/*.yaml ${upstream_copy_path}/
git add ${upstream_copy_path}
git commit -m "Add ${PACKAGE_IDENTIFIER} version ${VERSION}"
git push --set-upstream origin ${pr_branch}
gh pr create \\
  --repo ${UPSTREAM_REPO} \\
  --base ${UPSTREAM_BASE_BRANCH} \\
  --head ${fork_owner}:${pr_branch} \\
  --title "Add ${PACKAGE_IDENTIFIER} version ${VERSION}" \\
  --body-file "\$PR_BODY"
\`\`\`

\`\`\`powershell
\$StagedRoot = "<set-this-to-your-staged-submission-root>"
\$ManifestDir = Join-Path \$StagedRoot '${upstream_copy_path}'
\$PrBody = Join-Path \$StagedRoot 'winget-pkgs-pr-body.md'

gh repo clone ${FORK_REPO}
Set-Location ${fork_dir}
git switch -C ${pr_branch}
New-Item -ItemType Directory -Force -Path '${upstream_copy_path}' | Out-Null
Copy-Item (Join-Path \$ManifestDir '*.yaml') '${upstream_copy_path}/'
git add ${upstream_copy_path}
git commit -m "Add ${PACKAGE_IDENTIFIER} version ${VERSION}"
git push --set-upstream origin ${pr_branch}
gh pr create \`
  --repo ${UPSTREAM_REPO} \`
  --base ${UPSTREAM_BASE_BRANCH} \`
  --head ${fork_owner}:${pr_branch} \`
  --title "Add ${PACKAGE_IDENTIFIER} version ${VERSION}" \`
  --body-file "\$PrBody"
\`\`\`

Expected upstream target:
- repo: \`${UPSTREAM_REPO}\`
- base branch: \`${UPSTREAM_BASE_BRANCH}\`
- fork: \`${FORK_REPO}\`
- copy path: \`${upstream_copy_path}\`
EOF
}

main() {
  [[ -n "${VERSION}" ]] || die "Could not determine HA NOVA version."
  [[ -f "${ARCHIVE_PATH}" ]] || die "Missing winget manifest archive: ${ARCHIVE_PATH}"
  [[ -f "${WINDOWS_BUNDLE_PATH}" ]] || die "Missing Windows install bundle for hash verification: ${WINDOWS_BUNDLE_PATH}"
  command -v unzip >/dev/null 2>&1 || die "unzip is required to stage the winget submission payload."

  local expected_url installer_manifest installer_url installer_sha manifest_dir bundle_sha sidecar_sha
  expected_url="https://github.com/${REPO_SLUG}/releases/download/${RELEASE_TAG}/${WINDOWS_BUNDLE_NAME}"

  rm -rf "${STAGE_ROOT}"
  mkdir -p "${STAGE_ROOT}"
  unzip -q "${ARCHIVE_PATH}" -d "${STAGE_ROOT}"

  manifest_dir="$(manifest_root)"
  installer_manifest="${manifest_dir}/${PACKAGE_IDENTIFIER}.installer.yaml"
  [[ -f "${installer_manifest}" ]] || die "Missing installer manifest after staging: ${installer_manifest}"

  installer_url="$(sed -n 's/^[[:space:]]*InstallerUrl:[[:space:]]*//p' "${installer_manifest}" | head -1)"
  installer_sha="$(sed -n 's/^[[:space:]]*InstallerSha256:[[:space:]]*//p' "${installer_manifest}" | head -1)"
  bundle_sha="$(compute_sha256 "${WINDOWS_BUNDLE_PATH}")"

  [[ "${installer_url}" == "${expected_url}" ]] || die "InstallerUrl mismatch. Expected ${expected_url}, got ${installer_url}. Rebuild the winget manifest without local override URLs before staging a public submission."
  [[ -n "${installer_sha}" ]] || die "InstallerSha256 missing from ${installer_manifest}"
  [[ "${installer_sha}" == "${bundle_sha}" ]] || die "InstallerSha256 mismatch. Expected ${bundle_sha} from ${WINDOWS_BUNDLE_PATH}, got ${installer_sha}."
  if [[ -f "${WINDOWS_BUNDLE_SHA_PATH}" ]]; then
    sidecar_sha="$(awk 'NF { print toupper($1); exit }' "${WINDOWS_BUNDLE_SHA_PATH}")"
    [[ -n "${sidecar_sha}" ]] || die "Bundle SHA sidecar is empty: ${WINDOWS_BUNDLE_SHA_PATH}"
    [[ "${sidecar_sha}" == "${bundle_sha}" ]] || die "Bundle SHA sidecar mismatch. Expected ${bundle_sha}, got ${sidecar_sha} from ${WINDOWS_BUNDLE_SHA_PATH}."
  fi

  write_artifacts "${manifest_dir}" "${installer_url}" "${installer_sha}"

  log "Staged manifest payload at ${manifest_dir}"
  cat <<EOF

Next steps:
  1. Validate on Windows:
       winget validate --manifest "<staged-manifest-dir-on-your-validation-host>"
       same-host default from this checkout: ${manifest_dir}
       require a warning-free success result before opening any PR
  2. Fork microsoft/winget-pkgs and copy the staged files to:
       $(cat "${COPY_PATH_FILE}")
  3. Open a PR against ${UPSTREAM_REPO} ${UPSTREAM_BASE_BRANCH} using:
       <staged-submission-root-on-your-pr-host>/winget-pkgs-pr-body.md
       <staged-submission-root-on-your-pr-host>/winget-pkgs-gh-commands.md
  4. After merge + publication, wait until the public source shows the expected version:
       winget show --id ${PACKAGE_IDENTIFIER} --exact --source winget
  5. Then prove the initial public lane in a fresh Windows VM:
       winget install --id ${PACKAGE_IDENTIFIER} --exact
       ha-nova check-update
       winget uninstall --id ${PACKAGE_IDENTIFIER} --exact
  6. Once an older published version exists, prove the upgrade lane from a separate snapshot:
       winget upgrade --id ${PACKAGE_IDENTIFIER} --exact
       ha-nova check-update
       winget uninstall --id ${PACKAGE_IDENTIFIER} --exact
  7. Only after step 5 switch public Windows install docs from install.ps1 to winget.

Staged installer URL:
  ${installer_url}

Generated helper files:
  staged submission root: ${STAGE_ROOT}
  checklist: winget-pkgs-maintainer-checklist.md
  pr body:   winget-pkgs-pr-body.md
  copy path: winget-pkgs-copy-path.txt
  commands:  winget-pkgs-gh-commands.md
EOF
}

main "$@"
