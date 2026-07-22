#!/usr/bin/env bash
# Maintainer-side adoption snapshot from GitHub release-asset download counts.
# Zero telemetry: GitHub already counts asset downloads; this only aggregates
# what is publicly visible on every release page. Updates count automatically:
# `ha-nova update` downloads the target release's OS bundle from the same
# public asset URL (cli/bundle_apply.go), so per-release curves show fresh
# installs plus update adoption per OS.
#
# Usage: bash scripts/dev/release-download-stats.sh [--releases N]
# Output: per-release totals plus a per-OS/arch breakdown across releases.
#
# Caveats (read before quoting numbers): downloads are not active users —
# CI runs, retries, and mirrors count too, and every `ha-nova update`
# downloads the new bundle (which makes per-release counts a rough
# update-adoption signal).
set -euo pipefail

RELEASES=10
if [[ "${1:-}" == "--releases" ]]; then
  RELEASES="${2:?usage: --releases N}"
fi

REPO="markusleben/ha-nova"

gh api "repos/${REPO}/releases?per_page=${RELEASES}" --jq '
  [ .[] | select(.draft | not) ] as $rels
  | ($rels
     | map({tag: .tag_name, prerelease, total: ([.assets[].download_count] | add // 0)})
    ) as $per_release
  | ($rels
     | [ .[].assets[] ]
     | map({name, count: .download_count})
     | map(. + {os_arch: (
         .name
         | if (test("^ha-nova-installer-bundle-") | not) or test("\\.sha256$")
           then "other (raw binaries, checksums, ...)"
           elif test("macos-arm64")   then "macOS arm64"
           elif test("macos-amd64")   then "macOS amd64"
           elif test("linux-arm64")   then "Linux arm64"
           elif test("linux-amd64")   then "Linux amd64"
           elif test("windows-amd64") then "Windows amd64"
           else "other (raw binaries, checksums, ...)"
           end)})
     | group_by(.os_arch)
     | map({os_arch: .[0].os_arch, downloads: ([.[].count] | add)})
     | sort_by(-.downloads)
    ) as $per_os
  | {per_release: $per_release, per_os_arch_bundles: ($per_os | map(select(.os_arch != "other (raw binaries, checksums, ...)")))}
' | python3 -c "
import json, sys
d = json.load(sys.stdin)
print(f\"{'RELEASE':<16} {'DOWNLOADS':>9}  (all assets; prerelease marked)\")
for r in d['per_release']:
    mark = ' (rc)' if r['prerelease'] else ''
    print(f\"{r['tag']:<16} {r['total']:>9}{mark}\")
print()
print(f\"{'OS/ARCH (bundles only)':<24} {'DOWNLOADS':>9}\")
for o in d['per_os_arch_bundles']:
    print(f\"{o['os_arch']:<24} {o['downloads']:>9}\")
print()
print('Note: downloads != users (CI, retries, updates all count).')
"
