#!/usr/bin/env bash
# Maintainer-side adoption snapshot from GitHub release-asset download counts.
# Zero telemetry: GitHub already counts asset downloads publicly; this only
# aggregates them. Updates count automatically: `ha-nova update` downloads the
# target release's OS bundle from the public asset URL (cli/bundle_apply.go),
# so per-release curves show fresh installs plus update adoption per OS.
#
# Usage: bash scripts/dev/release-download-stats.sh [--releases N] [--estimate]
#
# --estimate subtracts the exactly-known noise from the bundle counts:
#   1. CI smoke baseline — every release.yml run ATTEMPT for a tag downloads
#      exactly 1 bundle per OS (install.sh downloads; the same-version update
#      does not; release-candidate.yml smokes from build artifacts and never
#      touches public assets). Attempts come from the Actions API per tag.
#   2. Own documented activity — optional local ledger
#      scripts/dev/own-activity.local.json (gitignored), shape:
#        {"v0.20.0": {"macos": 1, "linux": 0, "windows": 0}, ...}
#      Maintain it from the breadcrumbs proof notes.
# What remains is the best zero-telemetry estimate of real third-party
# acquisitions per release/OS. Reading: abandoned installs never download
# again, so the steady net value across recent stable releases IS the active
# third-party base; cumulative totals are acquisition history. Uninstalls are
# invisible by design (no telemetry) — this cannot go closer than that.
set -euo pipefail

RELEASES=10
ESTIMATE=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --releases) RELEASES="${2:?usage: --releases N}"; shift 2 ;;
    --estimate) ESTIMATE=1; shift ;;
    *) echo "unknown flag: $1" >&2; exit 1 ;;
  esac
done

REPO="markusleben/ha-nova"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

gh api "repos/${REPO}/releases?per_page=${RELEASES}" > "$TMP/releases.json"

if [[ "$ESTIMATE" == "1" ]]; then
  # Sum release.yml run attempts per tag (each attempt smokes <=1 bundle/OS;
  # attempts that failed before the smoke downloaded fewer — the subtraction
  # is therefore an upper bound, i.e. conservative for the third-party
  # estimate). The head_branch query param is IGNORED for tag-triggered runs,
  # so fetch everything once and group locally.
  gh api --paginate "repos/${REPO}/actions/workflows/release.yml/runs?per_page=100" \
    --jq '[.workflow_runs[] | {tag: .head_branch, attempts: .run_attempt}]' \
    | python3 -c "
import json, sys
from collections import Counter
counts = Counter()
for chunk in sys.stdin.read().strip().splitlines():
    for run in json.loads(chunk):
        counts[run['tag']] += run['attempts']
print(json.dumps(dict(counts)))
" > "$TMP/attempts.json"
fi

LEDGER="scripts/dev/own-activity.local.json"
[[ -f "$LEDGER" ]] || LEDGER=""

TMPDIR_ARG="$TMP" ESTIMATE_ARG="$ESTIMATE" LEDGER_ARG="$LEDGER" python3 - <<'PYEOF'
import json, os

tmp = os.environ["TMPDIR_ARG"]
estimate = os.environ["ESTIMATE_ARG"] == "1"
ledger_path = os.environ["LEDGER_ARG"]

rels = [r for r in json.load(open(f"{tmp}/releases.json")) if not r["draft"]]

OS_KEYS = [("macos-arm64", "macos"), ("macos-amd64", "macos"),
           ("linux-arm64", "linux"), ("linux-amd64", "linux"),
           ("windows-amd64", "windows")]

def bundles_by_os(release):
    out = {"macos": 0, "linux": 0, "windows": 0}
    for a in release["assets"]:
        name = a["name"]
        if not name.startswith("ha-nova-installer-bundle-") or name.endswith(".sha256"):
            continue
        for pattern, key in OS_KEYS:
            if pattern in name:
                out[key] += a["download_count"]
                break
    return out

attempts = {}
if estimate:
    attempts = json.load(open(f"{tmp}/attempts.json"))

ledger = json.load(open(ledger_path)) if ledger_path else {}

print(f"{'RELEASE':<16} {'DOWNLOADS':>9}  (all assets; prerelease marked)")
for r in rels:
    total = sum(a["download_count"] for a in r["assets"])
    mark = " (rc)" if r["prerelease"] else ""
    print(f"{r['tag_name']:<16} {total:>9}{mark}")

print()
print(f"{'RELEASE':<16} {'macOS':>6} {'Linux':>6} {'Win':>6}   (bundle downloads: updaters + fresh installs + CI)")
for r in rels:
    b = bundles_by_os(r)
    mark = " (rc)" if r["prerelease"] else ""
    print(f"{r['tag_name']:<16} {b['macos']:>6} {b['linux']:>6} {b['windows']:>6}{mark}")

if estimate:
    print()
    print(f"{'RELEASE':<16} {'macOS':>6} {'Linux':>6} {'Win':>6}   (NET estimate = bundles - CI attempts - own ledger)")
    nets = []
    for r in rels:
        tag = r["tag_name"]
        b = bundles_by_os(r)
        ci = attempts.get(tag, 0)
        own = ledger.get(tag, {})
        net = {k: max(0, b[k] - ci - int(own.get(k, 0))) for k in b}
        mark = " (rc)" if r["prerelease"] else ""
        print(f"{tag:<16} {net['macos']:>6} {net['linux']:>6} {net['windows']:>6}{mark}"
              + (f"   [ci={ci}" + (f", own={own}" if own else "") + "]"))
        if not r["prerelease"]:
            nets.append(net)
    if nets:
        recent = nets[:3]
        base = {k: round(sorted(n[k] for n in recent)[len(recent) // 2]) for k in ("macos", "linux", "windows")}
        cum = {k: sum(n[k] for n in nets) for k in ("macos", "linux", "windows")}
        print()
        print(f"Active third-party base (median net of last {len(recent)} stables): "
              f"macOS ~{base['macos']}, Linux ~{base['linux']}, Windows ~{base['windows']}")
        print(f"Cumulative third-party acquisitions ({len(nets)} stables): "
              f"macOS {cum['macos']}, Linux {cum['linux']}, Windows {cum['windows']}")
    if not ledger_path:
        print("(no own-activity ledger found — own proof installs are still included)")

print()
print("Note: downloads != users (CI, retries, updates all count); uninstalls are")
print("invisible by design. Steady per-release NET values are the active base;")
print("cumulative values are acquisition history.")
PYEOF
