#!/usr/bin/env bash
set -euo pipefail

BLOCKED=(
  ".env"
  ".env.local"
  "docs/breadcrumbs.md"
  "docs/choices.md"
)

FOUND=()
for f in "${BLOCKED[@]}"; do
  if git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
    FOUND+=("$f")
  fi
done

if [[ ${#FOUND[@]} -gt 0 ]]; then
  echo "[verify-blocked-files] ERROR: blocked files tracked in git: ${FOUND[*]}" >&2
  exit 1
fi

echo "[verify-blocked-files] OK: no blocked tracked files"
