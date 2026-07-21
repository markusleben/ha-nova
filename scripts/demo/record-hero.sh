#!/usr/bin/env bash
#
# record-hero.sh — record the hero flow: plain-English request -> Preview Card
# -> apply -> Result Card with revert offer.
#
# Usage: record-hero.sh [--take N]
# Output: .artifacts/demo/hero/take-N.cast
#
# The created automation never fires during recording (sunset trigger). Run
# cleanup-hero.sh after EVERY take; this script reminds you and verifies.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

TAKE=1
[[ "${1:-}" == "--take" ]] && TAKE="$2"
CAST="$ARTIFACTS_DIR/hero/take-$TAKE.cast"

PROMPT="When the sun sets, turn on the pool deck lights — but only if someone is home."

require_tools
setup_workspace
start_session
start_recording "$CAST"

type_text "$PROMPT"
submit
# The write skill may ask clarifying questions before the preview — as a
# native menu (walk the Recommended route) or as a plain-text question about
# optional enhancements (answer "skip"). Both outcomes are authentic takes.
for round in 1 2 3; do
  wait_idle 420
  screen_history | grep -qF "Preview:" && break
  if screen | grep -qF "Enter to select"; then
    answer_menu_recommended
    continue
  fi
  if ((round < 3)); then
    sleep 1.5 # let viewers read the question
    type_text "skip"
    submit
  fi
done
expect_in_history "Preview:"
expect_in_history "Options:"
sleep 2 # let viewers read the card before we answer

type_text "apply"
submit
wait_idle 420
expect_in_history "Saved and checked"
sleep 3 # closing hold; the answer itself replays at streaming pace

end_recording "$CAST"
kill_session

echo "Recorded $CAST"
echo "NEXT: bash $DEMO_DIR/cleanup-hero.sh   # delete the demo automation (every take!)"
echo "THEN: compress-cast.py + scrub-check.sh (see docs/reference/demo-recording.md)"
