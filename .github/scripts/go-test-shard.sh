#!/usr/bin/env bash
# One shard of a Go test run in CI (.github/workflows/ci.yml). Lists the tests
# of every package in cli/ that match <list-regexp>, deals them over <total>
# shards by the durations in .github/go-test-durations.txt, and runs shard
# <index> (0-based). Before anything runs
# it proves the split: given to go test -list, each shard's -run pattern must
# select exactly its own tests, and together the shards must hold every listed
# test exactly once, package-qualified. Anything else fails the shard.
#
# Usage: go-test-shard.sh <index> <total> <list-regexp> [go test flags...]
# The flags go to every go test call, so the listing builds the same test
# binary the run uses. Every shard of a run must get the same arguments on the
# same OS and Go version, so that all of them list the same tests.
set -euo pipefail

if ! [[ $# -ge 3 && $1 =~ ^[0-9]+$ && $2 =~ ^[1-9][0-9]*$ ]] || (($1 >= $2)); then
  echo "usage: go-test-shard.sh <index> <total> <list-regexp> [go test flags...]" >&2
  exit 2
fi
index=$1 total=$2 pattern=$3
shift 3
flags=("$@")
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
durations="$root/.github/go-test-durations.txt"
cd "$root/cli"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export LC_ALL=C

# list <regexp> <package>...: "<package> <name>" per listed name, sorted.
# go test -list prints a package's names, then "ok <package> <time>"; a
# package without test files prints "? <package> [no test files]".
list() {
  local regexp=$1
  shift
  go test ${flags[@]+"${flags[@]}"} -list "$regexp" "$@" </dev/null >"$work/raw"
  awk '
    /^ok[ \t]/ { for (i = 0; i < n; i++) print $2, names[i]; n = 0; next }
    /^\?[ \t]/ { next }
    /^[A-Za-z_][A-Za-z0-9_]*$/ { names[n++] = $0; next }
    { print "go-test-shard: unexpected go test -list line: " $0 > "/dev/stderr"; bad = 1 }
    END { exit bad || n }
  ' "$work/raw" | sort
}

list "$pattern" ./... >"$work/all"
count=$(wc -l <"$work/all")
if ((count < total)); then
  echo "go-test-shard: $count tests for $total shards" >&2
  exit 1
fi

# Longest test first, each to the shard with the least time so far (ties: by
# name, then the lowest shard), so every shard computes the same split.
awk 'FNR == NR {
    if (/^#/ || NF == 0) next
    if (NF != 3 || $3 !~ /^[0-9]+(\.[0-9]+)?$/) { print "go-test-shard: bad line in go-test-durations.txt: " $0 > "/dev/stderr"; exit 1 }
    seconds[$1 " " $2] = $3
    next
  }
  { key = $1 " " $2; print (key in seconds ? seconds[key] : 0.1), $1, $2 }' "$durations" "$work/all" |
  sort -k1,1gr -k2,3 |
  awk -v total="$total" '{
    best = 0
    for (s = 1; s < total; s++) if (load[s] < load[best]) best = s
    load[best] += $1
    print best, $2, $3
  }' >"$work/dealt"

for ((shard = 0; shard < total; shard++)); do
  awk -v shard="$shard" '$1 == shard { print $2, $3 }' "$work/dealt" >"$work/shard-$shard"
  # One anchored alternation per package: ^(TestA|TestB)$.
  awk '{ names[$1] = ($1 in names ? names[$1] "|" : "") $2 }
    END { for (pkg in names) print pkg, "^(" names[pkg] ")$" }' "$work/shard-$shard" >"$work/run-$shard"
  while read -r pkg regexp; do
    list "$regexp" "$pkg"
  done <"$work/run-$shard" >>"$work/selected"
done
if ! sort "$work/selected" | diff - "$work/all" >&2; then
  echo "go-test-shard: the shards do not hold every test exactly once (< extra or twice, > in no shard)" >&2
  exit 1
fi

echo "go-test-shard: shard $((index + 1)) of $total runs $(($(wc -l <"$work/shard-$index"))) of $((count)) tests"
status=0
while read -r pkg regexp; do
  go test ${flags[@]+"${flags[@]}"} -run "$regexp" "$pkg" </dev/null || status=1
done <"$work/run-$index"
exit "$status"
