#!/usr/bin/env bash
#
# scrub-check.sh — privacy gate for a COMPRESSED cast (run after
# compress-cast.py, which already redacts e-mail addresses byte-level).
#
# Exports the cast to plain text (this contains EVERYTHING that ever hit the
# screen, including content that scrolled away) and greps a denylist with
# zero tolerance. Any hit blocks the take. Always eyeball the exported text
# once per selected take — the denylist catches credentials and identity,
# not judgment calls.
#
# Usage: scrub-check.sh <compressed-cast>
set -euo pipefail

CAST="${1:?usage: scrub-check.sh <compressed-cast>}"
TXT="${CAST%.cast}.txt"
DEMO_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

DENYLIST=(
  "gmail"
  "Bearer "
  "eyJ"                # JWT prefix
  "Authorization"
  "SUPERVISOR_TOKEN"
  "password"
)

# Identity terms (account names, family names, street …) must never live in
# the repo — the denylist would otherwise publish what it protects. Keep one
# fixed-string pattern per line in the untracked scripts/demo/denylist.local.
if [[ -f "$DEMO_DIR/denylist.local" ]]; then
  while IFS= read -r pat; do
    [[ -n "$pat" && "$pat" != \#* ]] && DENYLIST+=("$pat")
  done < "$DEMO_DIR/denylist.local"
else
  echo "WARNING: $DEMO_DIR/denylist.local missing — identity terms are NOT checked." >&2
  echo "Create it with your account/family/location strings before a real take." >&2
fi

asciinema convert -f txt --overwrite "$CAST" "$TXT"

fail=0
for pat in "${DENYLIST[@]}"; do
  if hits=$(grep -inF "$pat" "$TXT"); then
    echo "DENYLIST HIT [$pat]:" >&2
    echo "$hits" | head -5 >&2
    fail=1
  fi
done

# Token-level, not line-level: a line holding both the allowed redacted
# address and a second leaked address must still fail.
if hits=$(grep -oinE "[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}" "$TXT" | grep -ivF "demo@example.com"); then
  echo "E-MAIL LEAK (redaction failed):" >&2
  echo "$hits" | head -5 >&2
  fail=1
fi

if (( fail )); then
  echo "SCRUB FAILED: $CAST — do not use this take." >&2
  exit 1
fi

echo "Scrub OK: $CAST"
echo "Reminder: eyeball $TXT once (names, addresses, anything sensitive)."
