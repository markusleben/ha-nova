#!/usr/bin/env bash
#
# render.sh — turn a compressed+cut cast into the final animated WebP.
#
#   cast -> vhs replay (Chromium: correct color emoji, calibrated grid)
#        [-> picture-in-picture pass, camera demo only]
#        -> gif2webp -> size gate -> QA frames
#
# All timeline surgery (start/end cut, spinner compression) happens in cast
# space via compress-cast.py — the replay clock drifts against the cast clock,
# so mapping cast times onto rendered frames is a losing game. The tape's
# trailing sleep freezes the last content frame, which provides the closing
# hold for free.
#
# Usage:
#   render.sh <cast> <out.webp> [--max-kb N] [--hold S]
#             [--pip-image FILE] [--pip-at S] [--pip-label TEXT]
#
# --pip-at defaults to the snapshot marker in <cast>.markers.json.
set -euo pipefail

DEMO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$DEMO_DIR/geometry.env"

CAST="${1:?usage: render.sh <cast> <out.webp> [options]}"
OUT="${2:?usage: render.sh <cast> <out.webp> [options]}"
shift 2

MAX_KB=3072 HOLD=4 PIP_IMAGE="" PIP_AT="" PIP_LABEL=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --max-kb) MAX_KB="$2"; shift 2 ;;
    --hold) HOLD="$2"; shift 2 ;;
    --pip-image) PIP_IMAGE="$2"; shift 2 ;;
    --pip-at) PIP_AT="$2"; shift 2 ;;
    --pip-label) PIP_LABEL="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done

MARKERS="$CAST.markers.json"
marker() { jq -r ".$1 // empty" "$MARKERS" 2>/dev/null || true; }

CAST_END=$(python3 -c "
import json
print(json.loads([l for l in open('$CAST') if l.strip()][-1])[0])")

if [[ -n "$PIP_IMAGE" && -z "$PIP_AT" ]]; then
  PIP_AT=$(marker snapshot)
  [[ -z "$PIP_AT" ]] && { echo "ERROR: --pip-at not given and no snapshot marker" >&2; exit 1; }
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cp "$CAST" "$WORK/replay.cast"

# --- 1. vhs replay ------------------------------------------------------------
# The replay content starts ~1s into the GIF (Show offset + clear + player
# startup). Only the PiP overlay timing needs this constant; slop there is
# hidden by the fade-in.
LEAD=1.0
TAPE_SLEEP=$(python3 -c "print(round($CAST_END + $HOLD + 2.5, 1))")
# NOTE: paths inside the tape must be RELATIVE — vhs' parser chokes on the
# dots that mktemp puts into absolute paths (and exits 0 while doing so).
cat >"$WORK/replay.tape" <<EOF
Output full.gif
Set Shell bash
Set FontSize $VHS_FONT_SIZE
Set FontFamily "$VHS_FONT_FAMILY"
Set Width $VHS_WIDTH
Set Height $VHS_HEIGHT
Set Padding $VHS_PADDING
Set Theme "$VHS_THEME"
Set Framerate 15
Hide
Type "clear && sleep 0.4 && asciinema play -q replay.cast && sleep $HOLD"
Enter
Sleep 0.4
Show
Sleep $TAPE_SLEEP
EOF
(cd "$WORK" && vhs replay.tape >/dev/null 2>&1) || true
[[ -s "$WORK/full.gif" ]] || { echo "ERROR: vhs produced no GIF" >&2; exit 1; }

# vhs drops frames under load without adjusting delays, compressing the GIF
# clock against the cast clock (30% observed at 50fps; Framerate 15 keeps it
# small). Measure the residual drift and scale overlay timings by it.
GIF_DUR=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$WORK/full.gif")
CLOCK_SCALE=$(python3 -c "
expected = $CAST_END + $LEAD + $HOLD
print(round(min(1.0, ($GIF_DUR - 0.5) / expected), 4))")
echo "GIF ${GIF_DUR}s vs. erwartete $(python3 -c "print(round($CAST_END + $LEAD + $HOLD, 1))")s — clock scale $CLOCK_SCALE"

# --- 2. optional picture-in-picture -------------------------------------------
FINAL_GIF="$WORK/full.gif"
if [[ -n "$PIP_IMAGE" ]]; then
  PIP_SHOW=$(python3 -c "print(max(0, ($PIP_AT + $LEAD) * $CLOCK_SCALE))")
  PIP_W=$(python3 -c "print(int($VHS_WIDTH * 0.36))")
  # Homebrew ffmpeg ships without drawtext (no freetype) — the caption is a
  # nice-to-have; fall back to the plain framed snapshot.
  CAPTION=""
  if ffmpeg -hide_banner -filters 2>/dev/null | grep -q " drawtext "; then
    label="${PIP_LABEL:-live snapshot}"
    CAPTION="pad=iw:ih+26:0:0:color=white,\
drawtext=fontfile=$HOME/Library/Fonts/JetBrainsMono-Regular.ttf:text='$label':\
fontsize=16:fontcolor=black:x=(w-text_w)/2:y=h-22,"
  fi
  GRAPH="[1:v]scale=$PIP_W:-1,pad=iw+8:ih+8:4:4:color=white,\
${CAPTION}format=rgba,\
fade=t=in:st=$PIP_SHOW:d=0.4:alpha=1[snap];\
[0:v][snap]overlay=W-w-20:20:enable='gte(t,$PIP_SHOW)'"
  ffmpeg -hide_banner -loglevel error -y -i "$WORK/full.gif" \
    -loop 1 -t "$GIF_DUR" -i "$PIP_IMAGE" -filter_complex "$GRAPH" \
    -c:v ffv1 "$WORK/pip.mkv"
  ffmpeg -hide_banner -loglevel error -y -i "$WORK/pip.mkv" \
    -vf "palettegen=stats_mode=diff" "$WORK/palette.png"
  ffmpeg -hide_banner -loglevel error -y -i "$WORK/pip.mkv" -i "$WORK/palette.png" \
    -lavfi "paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" \
    -f gif "$WORK/pip.gif"
  FINAL_GIF="$WORK/pip.gif"
fi

# --- 2b. reading cursor -------------------------------------------------------
# During driver-logged "read" windows a small pointer sweeps left-to-right
# across two text lines per page — the skim gesture from the original demo
# (which was a real screen recording). Purely cosmetic overlay.
CURSOR_FILTER=$(python3 - "$MARKERS" "$VHS_WIDTH" "$VHS_HEIGHT" "$LEAD" "$CLOCK_SCALE" <<'PY'
import json, sys
try:
    m = json.load(open(sys.argv[1]))
except Exception:
    print("")
    raise SystemExit
W, H = int(sys.argv[2]), int(sys.argv[3])
LEAD, SCALE = float(sys.argv[4]), float(sys.argv[5])
wins = [w for w in m.get("windows", []) if w[2] in ("read", "answer")]
x0, x1 = 70, W - 230
parts, prev, idx = [], "0:v", 0
for a, b, _ in wins:
    A = (a + LEAD) * SCALE + 0.2
    B = (b + LEAD) * SCALE - 0.2
    if B - A < 2.0:
        continue
    lanes_n = max(2, int((B - A) / 3.2))
    ys = [int(H * (0.22 + 0.5 * j / max(1, lanes_n - 1))) for j in range(lanes_n)]
    seg = (B - A) / lanes_n
    for j, y in enumerate(ys):
        s, e = A + j * seg, A + (j + 1) * seg
        x = f"{x0}+{x1 - x0}*(t-{s:.2f})/{seg:.2f}"
        parts.append((prev, x, y, s, e, f"c{idx}"))
        prev, idx = f"c{idx}", idx + 1
if not parts:
    print("")
    raise SystemExit
chain = []
for k, (src, x, y, s, e, out) in enumerate(parts):
    label = "" if k == len(parts) - 1 else f"[{out}]"
    chain.append(f"[{src}][1:v]overlay=x='{x}':y={y}:enable='between(t,{s:.2f},{e:.2f})'{label}")
print(";".join(chain))
PY
)
if [[ -n "$CURSOR_FILTER" ]]; then
  python3 -c "import PIL" 2>/dev/null \
    || { echo "ERROR: the cursor overlay needs Pillow: python3 -m pip install Pillow" >&2; exit 1; }
  python3 - "$WORK/cursor.png" <<'PY'
import sys
from PIL import Image, ImageDraw
img = Image.new("RGBA", (22, 30), (0, 0, 0, 0))
d = ImageDraw.Draw(img)
# macOS-style arrow: white fill, black outline
pts = [(2, 2), (2, 24), (8, 18), (12, 27), (16, 25), (12, 16), (20, 16)]
d.polygon(pts, fill=(250, 250, 250, 235), outline=(20, 20, 20, 255))
img.save(sys.argv[1])
PY
  ffmpeg -hide_banner -loglevel error -y -i "$FINAL_GIF" -i "$WORK/cursor.png" \
    -filter_complex "$CURSOR_FILTER" -c:v ffv1 "$WORK/cursor.mkv"
  ffmpeg -hide_banner -loglevel error -y -i "$WORK/cursor.mkv" \
    -vf "palettegen=stats_mode=diff" "$WORK/cpal.png"
  ffmpeg -hide_banner -loglevel error -y -i "$WORK/cursor.mkv" -i "$WORK/cpal.png" \
    -lavfi "paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" \
    -f gif "$WORK/cursor.gif"
  FINAL_GIF="$WORK/cursor.gif"
fi

# --- 3. webp + size gate (with automatic fps/quality fallback) ----------------
mkdir -p "$(dirname "$OUT")"
gif2webp -q 65 -m 6 "$FINAL_GIF" -o "$OUT" >/dev/null 2>&1

reduce_and_convert() { # fps quality outfile-gif — reduces FINAL_GIF (keeps PiP)
  # -fps_mode cfr keeps duplicate frames: ffmpeg's GIF muxer otherwise drops
  # them WITHOUT extending delays, silently compressing the clock (verified).
  ffmpeg -hide_banner -loglevel error -y -i "$FINAL_GIF" -filter_complex \
    "fps=$1,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" \
    -fps_mode cfr -f gif "$3"
  gif2webp -q "$2" -m 6 "$3" -o "$OUT" >/dev/null 2>&1
}

SIZE_KB=$(( $(stat -f %z "$OUT") / 1024 ))
if (( SIZE_KB > MAX_KB )); then
  echo "Over budget at q65 (${SIZE_KB} KB) — retrying with fps=14 and q55..."
  reduce_and_convert 14 55 "$WORK/reduced.gif"
  FINAL_GIF="$WORK/reduced.gif"
  SIZE_KB=$(( $(stat -f %z "$OUT") / 1024 ))
fi
if (( SIZE_KB > MAX_KB )); then
  echo "Still over budget (${SIZE_KB} KB) — final attempt with fps=10 and q45..."
  reduce_and_convert 10 45 "$WORK/reduced2.gif"
  FINAL_GIF="$WORK/reduced2.gif"
  SIZE_KB=$(( $(stat -f %z "$OUT") / 1024 ))
fi
# --- 3b. duration assert --------------------------------------------------------
# The whole pipeline stands on time fidelity; fail loudly if the final WebP's
# clock deviates from the rendered GIF's by more than 5%.
WEBP_DUR=$(python3 -c "
import re, subprocess
out = subprocess.run(['webpmux', '-info', '$OUT'], capture_output=True, text=True).stdout
durs = [int(m.group(1)) for m in
        re.finditer(r'^\s*\d+:\s+\d+\s+\d+\s+\w+\s+\d+\s+\d+\s+(\d+)', out, re.M)]
print(round(sum(durs) / 1000, 1))")
python3 -c "
gif, webp = float('$GIF_DUR'), float('$WEBP_DUR')
dev = abs(gif - webp) / gif
print(f'Dauer-Check: GIF {gif}s vs WebP {webp}s ({dev:.1%} Abweichung)')
raise SystemExit(1 if dev > 0.05 else 0)" || { echo "ERROR: WebP clock drifted" >&2; exit 1; }

# --- 4. QA frames (before the size gate — content review must survive it) ------
QA_DIR="${OUT%.webp}-qa"
mkdir -p "$QA_DIR"
N=$(ffprobe -v error -select_streams v -count_frames -show_entries stream=nb_read_frames -of csv=p=0 "$FINAL_GIF")
ffmpeg -hide_banner -loglevel error -y -i "$FINAL_GIF" -vf "select='eq(n\,2)'" -frames:v 1 -update 1 "$QA_DIR/first.png"
ffmpeg -hide_banner -loglevel error -y -i "$FINAL_GIF" -vf "select='eq(n\,$((N / 2)))'" -frames:v 1 -update 1 "$QA_DIR/mid.png"
ffmpeg -hide_banner -loglevel error -y -i "$FINAL_GIF" -vf reverse -frames:v 1 -update 1 "$QA_DIR/last.png"

if (( SIZE_KB > MAX_KB )); then
  echo "ERROR: $OUT is ${SIZE_KB} KB (budget ${MAX_KB} KB) even after reduction." >&2
  echo "QA frames in $QA_DIR/ — knobs: compress-cast --target lower, gif2webp -q 45." >&2
  exit 1
fi

echo "Rendered $OUT (${SIZE_KB} KB, budget ${MAX_KB} KB) — QA frames in $QA_DIR/"
