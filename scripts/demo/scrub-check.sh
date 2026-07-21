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

DENYLIST=(
  "czerwonka"          # account identity (must be redacted by compress-cast)
  "gmail"
  "Bearer "
  "eyJ"                # JWT prefix
  "Authorization"
  "SUPERVISOR_TOKEN"
  "password"
)

asciinema convert -f txt --overwrite "$CAST" "$TXT"

fail=0
for pat in "${DENYLIST[@]}"; do
  if hits=$(grep -inF "$pat" "$TXT"); then
    echo "DENYLIST HIT [$pat]:" >&2
    echo "$hits" | head -5 >&2
    fail=1
  fi
done

if hits=$(grep -inE "[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}" "$TXT" | grep -v "demo@example.com"); then
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
