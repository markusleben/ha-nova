#!/usr/bin/env bash
#
# record-review.sh — record the review flow: audit automations, severity
# findings in plain language. Read-only on the Home Assistant side.
#
# Usage: record-review.sh [--take N] [--scope "light automations"]
# Output: .artifacts/demo/review/take-N.cast
#
# Run an UNRECORDED dry-run first and check which automation names surface;
# narrow the scope if findings are noisy or names are sensitive.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

TAKE=1 SCOPE="automations"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --take) TAKE="$2"; shift 2 ;;
    --scope) SCOPE="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done
CAST="$ARTIFACTS_DIR/review/take-$TAKE.cast"

PROMPT="Review my $SCOPE for problems."

require_tools
setup_workspace
start_session
start_recording "$CAST"

type_text "$PROMPT"
submit
wait_idle 420
if screen | grep -qF "Enter to select"; then
  answer_menu_recommended
  wait_idle 420
fi
sleep 3 # closing hold; the findings replay at streaming pace

end_recording "$CAST"
kill_session

echo "Recorded $CAST"
echo "NEXT: compress-cast.py + scrub-check.sh (see docs/reference/demo-recording.md)"
