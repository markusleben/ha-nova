#!/usr/bin/env bash
# The execution half of build-cloud-evidence.sh — sourced, not run.
# Verifies downloaded bundles (checksums, identity) and runs the per-platform
# internal-cloud-release-check, fail-closed. Nothing here writes GitHub state.
#
# Expects from the sourcing script: die(), step(), $work, $target_tree,
# $expected_version, $LINUX_SSH, $WINDOWS_SSH.
# shellcheck disable=SC2154  # those globals are assigned by the sourcing script
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  echo "cloud-evidence-provenance.sh is sourced by build-cloud-evidence.sh, not run directly" >&2
  exit 64
fi

# Both remotes print the bundle manifest between markers. Without them the
# extraction is guesswork: PowerShell wraps its progress stream in CLIXML and
# interleaves it with stdout, so "the first line that ends in }" is not the
# manifest.
BEGIN_MARK="<<<HA-NOVA-BUNDLE"
END_MARK="HA-NOVA-BUNDLE>>>"

extract_manifest() {  # <raw-output-file>
  # Strip CR first: Windows sends CRLF, so an exact match against the marker
  # compares "<<<HA-NOVA-BUNDLE\r" and never fires. The raw stream also carries
  # ssh's own warnings, which is precisely why the markers exist.
  tr -d '\r' <"$1" \
    | awk -v b="$BEGIN_MARK" -v e="$END_MARK" '$0==b{f=1;next} $0==e{f=0} f'
}

check_identity() {  # <manifest-file>
  node -e '
    const fs = require("node:fs");
    const [path, version, tree] = process.argv.slice(1);
    const raw = fs.readFileSync(path, "utf8").trim();
    if (!raw) throw new Error("no bundle manifest in the remote output");
    const bundle = JSON.parse(raw);
    if (bundle.version !== version || bundle.cloud_release?.source_tree_sha !== tree) {
      throw new Error(
        `bundle identity mismatch: ${bundle.version} / ${bundle.cloud_release?.source_tree_sha}`,
      );
    }
  ' "$1" "$expected_version" "$target_tree"
}

# CI verified these checksums before upload; recomputing them after the
# download binds the exact local bytes the provenance runs will execute.
# Iterate the BUNDLES and demand a checksum for each — iterating the .sha256
# files instead would silently leave an uncovered archive unverified.
verify_bundle_checksums() {  # (reads $work/bundles)
  local bundle checksum_count=0
  for bundle in "$work"/bundles/*; do
    case "$bundle" in *.sha256) continue ;; esac
    [ -f "$bundle" ] || die "the artifact contains no files"
    [ -f "$bundle.sha256" ] \
      || die "$(basename "$bundle") has no .sha256 in the artifact — refusing an unverifiable bundle"
    (cd "$work/bundles" && shasum -a 256 --check "$(basename "$bundle").sha256" >/dev/null) \
      || die "checksum mismatch for $(basename "$bundle") — the download does not match what CI built"
    checksum_count=$((checksum_count + 1))
  done
  [ "$checksum_count" -gt 0 ] || die "the artifact contains no bundles"
  echo "  $checksum_count bundle checksum(s) verified"
}

# The provenance runs EXECUTE the candidate binary. Prove from the local bytes
# that every bundle is the resolved target first — a manual recovery dispatch
# for the wrong PR, or a PR that moved between the local resolution and the
# workflow's own, must die here, not after running a foreign build on three
# machines.
verify_bundles_identity() {  # (reads $work/bundles)
  local bundle manifest
  for bundle in "$work"/bundles/*; do
    case "$bundle" in *.sha256) continue ;; esac
    manifest="$work/pre-identity.$(basename "$bundle").json"
    case "$bundle" in
      *.tar.gz)
        tar -xzOf "$bundle" ha-nova/bundle.json >"$manifest" 2>/dev/null \
          || die "$(basename "$bundle"): cannot read ha-nova/bundle.json from the archive" ;;
      *.zip)
        unzip -p "$bundle" ha-nova/bundle.json >"$manifest" 2>/dev/null \
          || die "$(basename "$bundle"): cannot read ha-nova/bundle.json from the archive" ;;
      *) die "unexpected file in the artifact: $(basename "$bundle")" ;;
    esac
    check_identity "$manifest" \
      || die "$(basename "$bundle"): bundle identity does not match the resolved target — refusing to execute it"
  done
  echo "  every bundle matches tree ${target_tree}"
}

provenance_unix() {  # <label> <archive> <ssh-host|"">
  local label="$1" archive="$2" host="$3"
  local script="
    set -euo pipefail
    d=\"\$(mktemp -d)\"; mkdir -p \"\$d/home/.local/share\"
    tar -xzf \"\$1\" -C \"\$d/home/.local/share\"
    echo '$BEGIN_MARK'
    cat \"\$d/home/.local/share/ha-nova/bundle.json\"
    echo
    echo '$END_MARK'
    # Unix builds resolve their install root from HOME, so the check passes
    # only from an installed layout — calling the extracted binary in place
    # fails with 'official Cloud release provenance is not enabled'.
    HOME=\"\$d/home\" HA_NOVA_NO_CENSUS=1 \"\$d/home/.local/share/ha-nova/ha-nova\" internal-cloud-release-check
    rm -rf \"\$d\"
  "
  if [ -z "$host" ]; then
    bash -c "$script" _ "$archive" >"$work/$label.out" 2>&1
  else
    # Invocation-unique remote name: two concurrent runs against the same lab
    # host must not swap each other's archive between the scp and the ssh.
    local remote_archive="ha-nova-candidate-$$.tar.gz"
    scp -q -o BatchMode=yes "$archive" "$host:$remote_archive" \
      || die "$label: cannot copy the bundle to $host"
    # `bash -s` has no $0 slot: arguments land on $1 directly, unlike the
    # `bash -c "$script" _ "$archive"` form used for the local run.
    local status=0
    ssh -o BatchMode=yes "$host" "bash -s $remote_archive" <<<"$script" \
      >"$work/$label.out" 2>&1 || status=$?
    # Capture BEFORE cleaning up. Called in an `|| die` list, errexit is off in
    # here, so a cleanup that succeeds would otherwise become the function's
    # status and a failed provenance run would read as a pass.
    ssh -o BatchMode=yes "$host" "rm -f $remote_archive" >/dev/null 2>&1 || true
    return $status
  fi
}

provenance_windows() {  # <archive>
  local archive="$1"
  # Invocation-unique remote name, same reason as the unix path.
  local remote_zip="ha-nova-candidate-$$.zip"
  scp -q -o BatchMode=yes "$archive" "$WINDOWS_SSH:$remote_zip" \
    || die "windows: cannot copy the bundle to $WINDOWS_SSH"
  # -EncodedCommand, not -Command: a multi-line script does not survive the
  # ssh argument boundary and PowerShell answers with its usage text.
  local ps encoded
  ps="\$ErrorActionPreference='Stop'
\$ProgressPreference='SilentlyContinue'
\$d = Join-Path \$env:TEMP ('ha-nova-' + [guid]::NewGuid())
Expand-Archive -LiteralPath $remote_zip -DestinationPath \$d
\$root = Join-Path \$d 'ha-nova'
Write-Output '$BEGIN_MARK'
Get-Content (Join-Path \$root 'bundle.json') -Raw
Write-Output '$END_MARK'
\$env:HA_NOVA_NO_CENSUS = '1'
& (Join-Path \$root 'ha-nova.exe') internal-cloud-release-check
if (\$LASTEXITCODE -ne 0) { throw 'provenance check failed' }
Remove-Item -Recurse -Force \$d"
  encoded="$(printf '%s' "$ps" | iconv -f UTF-8 -t UTF-16LE | base64 | tr -d '\n')" \
    || die "windows: cannot encode the provenance script"
  local shell="${HA_NOVA_WINDOWS_PWSH:-powershell}"
  local status=0
  ssh -o BatchMode=yes "$WINDOWS_SSH" "$shell -NoProfile -EncodedCommand $encoded" \
    >"$work/windows.out" 2>&1 || status=$?
  ssh -o BatchMode=yes "$WINDOWS_SSH" "cmd /c \"del $remote_zip\"" >/dev/null 2>&1 || true
  return $status
}
