#!/usr/bin/env bash
#
# calibrate-geometry.sh — measure the exact pixel size at which vhs produces
# the target terminal grid, then write geometry.env.
#
# Why: vhs sizes its terminal in pixels; the resulting cols/rows depend on the
# font raster. Recording (tmux) and rendering (vhs) must agree on the grid
# EXACTLY or fullscreen-TUI replays garble. This probe asks vhs itself.
set -euo pipefail

DEMO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Default to the committed calibration so a bare re-run REFRESHES the
# existing grid instead of silently switching layout and asset dimensions.
DEMO_COLS=100 DEMO_ROWS=30 VHS_FONT_SIZE=16 VHS_PADDING=12
if [[ -f "$DEMO_DIR/geometry.env" ]]; then
  # shellcheck source=geometry.env
  source "$DEMO_DIR/geometry.env"
fi
TARGET_COLS="${1:-$DEMO_COLS}"
TARGET_ROWS="${2:-$DEMO_ROWS}"
FONT_SIZE="${3:-$VHS_FONT_SIZE}"
PADDING="$VHS_PADDING"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

probe() { # width height -> "cols rows"
  local w="$1" h="$2"
  # Paths inside the tape must be RELATIVE — vhs' parser chokes on the dots
  # that mktemp puts into absolute paths.
  cat >"$WORK/probe.tape" <<EOF
Output probe.gif
Set Shell bash
Set FontSize $FONT_SIZE
Set FontFamily "JetBrains Mono"
Set Width $w
Set Height $h
Set Padding $PADDING
Hide
Type "echo GEO \$(tput cols) \$(tput lines) > geo.txt"
Enter
Sleep 1.5
Show
Sleep 0.3
EOF
  (cd "$WORK" && vhs probe.tape >/dev/null 2>&1)
  awk '{print $2, $3}' "$WORK/geo.txt"
}

echo "Probing vhs cell metrics (font size $FONT_SIZE)..."
read -r cols rows <<<"$(probe 1000 680)"
cell_w=$(python3 -c "print((1000-2*$PADDING)/$cols)")
cell_h=$(python3 -c "print((680-2*$PADDING)/$rows)")

width=$(python3 -c "import math; print(math.ceil($TARGET_COLS*$cell_w)+2*$PADDING+2)")
height=$(python3 -c "import math; print(math.ceil($TARGET_ROWS*$cell_h)+2*$PADDING+2)")

for _ in 1 2 3 4; do
  read -r cols rows <<<"$(probe "$width" "$height")"
  echo "  ${width}x${height}px -> ${cols}x${rows}"
  [[ "$cols" == "$TARGET_COLS" && "$rows" == "$TARGET_ROWS" ]] && break
  ((cols < TARGET_COLS)) && width=$((width + 4))
  ((cols > TARGET_COLS)) && width=$((width - 4))
  ((rows < TARGET_ROWS)) && height=$((height + 4))
  ((rows > TARGET_ROWS)) && height=$((height - 4))
done

if [[ "$cols" != "$TARGET_COLS" || "$rows" != "$TARGET_ROWS" ]]; then
  echo "ERROR: could not converge on ${TARGET_COLS}x${TARGET_ROWS} (got ${cols}x${rows})." >&2
  exit 1
fi

cat >"$DEMO_DIR/geometry.env" <<EOF
# Calibrated vhs <-> tmux geometry. Regenerate with calibrate-geometry.sh.
# Measured $(date +%Y-%m-%d) with vhs $(vhs --version 2>/dev/null | awk '{print $3}').
# CRITICAL: recordings and renders must use the SAME cell grid, or the
# fullscreen-TUI replay garbles (verified failure mode).
DEMO_COLS=$TARGET_COLS
DEMO_ROWS=$TARGET_ROWS
VHS_WIDTH=$width
VHS_HEIGHT=$height
VHS_FONT_SIZE=$FONT_SIZE
VHS_PADDING=$PADDING
VHS_FONT_FAMILY="JetBrains Mono"
VHS_THEME="GitHub Dark"
EOF
echo "Wrote $DEMO_DIR/geometry.env (${width}x${height}px = ${TARGET_COLS}x${TARGET_ROWS})"
