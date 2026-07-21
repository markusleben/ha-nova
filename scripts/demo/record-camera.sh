#!/usr/bin/env bash
#
# record-camera.sh — record the camera flow: fetch a live frame, describe it.
# Read-only. Also preserves the exact snapshot Claude looked at, so render.sh
# can blend it in as picture-in-picture (honesty rule: only the image from
# THIS session may be shown).
#
# Usage: record-camera.sh [--take N]
# Output: .artifacts/demo/camera/take-N.cast (+ take-N-snapshot.<ext>)
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

TAKE=1
[[ "${1:-}" == "--take" ]] && TAKE="$2"
CAST="$ARTIFACTS_DIR/camera/take-$TAKE.cast"

PROMPT="Check the garage camera — did I leave the door open?"

require_tools
setup_workspace
start_session
STAMP="$(mktemp)"
start_recording "$CAST"

type_text "$PROMPT"
submit
wait_idle 300
sleep 4 # closing hold on the answer

end_recording "$CAST"
kill_session

# Preserve the snapshot fetched during THIS take (newest image since STAMP).
snapshot=$(find "/private/tmp/claude-$(id -u)" /tmp -type f \
  \( -name '*.jpg' -o -name '*.jpeg' -o -name '*.png' \) \
  -newer "$STAMP" 2>/dev/null | xargs -I{} stat -f '%m {}' {} 2>/dev/null \
  | sort -rn | head -1 | cut -d' ' -f2-)
rm -f "$STAMP"

if [[ -n "$snapshot" ]]; then
  cp "$snapshot" "$ARTIFACTS_DIR/camera/take-$TAKE-snapshot.${snapshot##*.}"
  echo "Snapshot preserved: $ARTIFACTS_DIR/camera/take-$TAKE-snapshot.${snapshot##*.}"
else
  echo "WARNING: no session snapshot found — PiP pass will not be possible." >&2
fi

echo "Recorded $CAST"
echo "NEXT: compress-cast.py + scrub-check.sh; review the snapshot image itself"
