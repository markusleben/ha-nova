#!/usr/bin/env bash
# Path-scoped CI (.github/workflows/ci.yml): which areas a change touches and
# which jobs must run. Fail-safe: a workflow file, an unknown path, an empty
# diff, or --all selects every job; a git or mapping error fails the script.
#
# Usage: changed-areas.sh --all | changed-areas.sh <base> <head>
# Reads JOB_AREAS: JSON object, job id -> non-empty list of areas.
# Writes to $GITHUB_OUTPUT (stdout when unset): <area>=true|false per area and
# run=<JSON object, job id -> true|false>.
set -euo pipefail

areas=(go relay integration skills docs workflows)
hits=" "
all=false

classify() {
  case "$1" in
    .github/*) hits+="workflows "; all=true ;;
    cli/*) hits+="go " ;;
    nova/integration/* | tests/ha_integration/*) hits+="integration " ;;
    nova/* | package.json | package-lock.json) hits+="relay " ;;
    skills/* | tests/skills/*) hits+="skills " ;;
    docs/*) hits+="docs " ;;
    */*) echo "unknown path, running every job: $1"; all=true ;;
    *.md) hits+="docs " ;;
    *) echo "unknown path, running every job: $1"; all=true ;;
  esac
}

if [[ $# -eq 1 && $1 == --all ]]; then
  echo "running every job"
  all=true
elif [[ $# -eq 2 ]]; then
  list="$(mktemp)"
  trap 'rm -f "$list"' EXIT
  git diff -z --name-only --no-renames "$1" "$2" -- >"$list"
  count=0
  while IFS= read -r -d '' file; do
    count=$((count + 1))
    classify "$file"
  done <"$list"
  echo "changed files: $count"
  if [[ $count -eq 0 ]]; then
    echo "empty diff, running every job"
    all=true
  fi
else
  echo "usage: changed-areas.sh --all | changed-areas.sh <base> <head>" >&2
  exit 2
fi

flags="$(jq -cn --arg hits "$hits" --argjson all "$all" \
  '$ARGS.positional | map(. as $a | {key: $a, value: ($all or ($hits | contains(" \($a) ")))}) | from_entries' \
  --args "${areas[@]}")"

run="$(jq -ce --argjson on "$flags" '
  if type != "object" or length == 0 then error("JOB_AREAS must be a non-empty object") else . end
  | map_values(
      if type != "array" or length == 0 then error("each job needs a non-empty list of areas")
      elif any(.[]; . as $a | $on | has($a) | not) then error("unknown area in \(.)")
      else any(.[]; $on[.]) end)
' <<<"${JOB_AREAS:?JOB_AREAS is required}")"

out="${GITHUB_OUTPUT:-/dev/stdout}"
{
  jq -r 'to_entries[] | "\(.key)=\(.value)"' <<<"$flags"
  echo "run=$run"
} >>"$out"
echo "areas: $flags"
echo "jobs: $run"
