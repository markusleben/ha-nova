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
    --jq '[.workflow_runs[] | {tag: .head_branch, attempts: .run_attempt, ok: (.conclusion == "success")}]' \
    | python3 -c "
import json, sys
from collections import Counter
hi, lo = Counter(), Counter()
for chunk in sys.stdin.read().strip().splitlines():
    for run in json.loads(chunk):
        hi[run['tag']] += run['attempts']          # upper bound: every attempt smoked
        lo[run['tag']] += 1 if run['ok'] else 0    # lower bound: only successful runs
print(json.dumps({'hi': dict(hi), 'lo': dict(lo)}))
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

attempts = {"hi": {}, "lo": {}}
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
    from datetime import datetime, timezone
    FRESH_DAYS = 7
    now = datetime.now(timezone.utc)

    def net_range(r):
        # Third-party estimate as a RANGE: the CI subtraction is bounded by
        # "every attempt smoked" (hi subtraction -> low estimate) and "only
        # successful runs smoked" (lo subtraction -> high estimate).
        tag = r["tag_name"]
        b = bundles_by_os(r)
        own = ledger.get(tag, {})
        ci_hi = attempts["hi"].get(tag, 0)
        ci_lo = attempts["lo"].get(tag, 0)
        lo = {k: max(0, b[k] - ci_hi - int(own.get(k, 0))) for k in b}
        hi = {k: max(0, b[k] - ci_lo - int(own.get(k, 0))) for k in b}
        return lo, hi, ci_lo, ci_hi, own

    def fmt(lo, hi):
        return f"{lo}" if lo == hi else f"{lo}-{hi}"

    print()
    print(f"{'RELEASE':<16} {'macOS':>6} {'Linux':>6} {'Win':>6}   (NET third-party estimate: bundles - CI - own ledger)")
    stable_ranges = []
    for r in rels:
        lo, hi, ci_lo, ci_hi, own = net_range(r)
        published = datetime.fromisoformat(r["published_at"].replace("Z", "+00:00"))
        fresh = (now - published).days < FRESH_DAYS
        mark = " (rc)" if r["prerelease"] else (" (fresh)" if fresh else "")
        ci_s = f"{ci_lo}" if ci_lo == ci_hi else f"{ci_lo}-{ci_hi}"
        print(f"{r['tag_name']:<16} {fmt(lo['macos'], hi['macos']):>6} {fmt(lo['linux'], hi['linux']):>6} {fmt(lo['windows'], hi['windows']):>6}{mark}"
              + f"   [ci={ci_s}" + (f", own={own}" if own else "") + "]")
        if not r["prerelease"]:
            stable_ranges.append({"lo": lo, "hi": hi, "fresh": fresh})

    settled = [x for x in stable_ranges if not x["fresh"]]
    if settled:
        recent = settled[:3]
        def median(vals):
            return sorted(vals)[len(vals) // 2]
        base_lo = {k: median([x["lo"][k] for x in recent]) for k in ("macos", "linux", "windows")}
        base_hi = {k: median([x["hi"][k] for x in recent]) for k in ("macos", "linux", "windows")}
        print()
        print(f"Active third-party base (median of last {len(recent)} settled stables; fresh releases excluded):")
        print(f"  macOS ~{fmt(base_lo['macos'], base_hi['macos'])}, Linux ~{fmt(base_lo['linux'], base_hi['linux'])}, Windows ~{fmt(base_lo['windows'], base_hi['windows'])}")
    cum_lo = {k: sum(x["lo"][k] for x in stable_ranges) for k in ("macos", "linux", "windows")}
    cum_hi = {k: sum(x["hi"][k] for x in stable_ranges) for k in ("macos", "linux", "windows")}
    print(f"Cumulative third-party acquisitions ({len(stable_ranges)} stables): "
          f"macOS {fmt(cum_lo['macos'], cum_hi['macos'])}, Linux {fmt(cum_lo['linux'], cum_hi['linux'])}, Windows {fmt(cum_lo['windows'], cum_hi['windows'])}")
    print("Installed-but-not-updating users appear in the cumulative line only —")
    print("they never re-download, so the active line cannot see them.")
    if not ledger_path:
        print("(no own-activity ledger found — own proof installs are still included)")

print()
print("Note: downloads != users (CI, retries, updates all count); uninstalls are")
print("invisible by design. Steady per-release NET values are the active base;")
print("cumulative values are acquisition history.")
PYEOF
