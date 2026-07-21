# Demo Recording Pipeline

How animated terminal demos (`assets/demo-*.webp`) are produced for README
and launch material, and how to re-record them for a future release.
Everything shown is a REAL Claude Code session against a live Home
Assistant — only waiting time is compressed.

## Honesty rules

- No staged output. The cast is recorded from a live session; retakes are
  allowed, edits of content are not.
- Time compression only: spinner/thinking windows and idle gaps are shortened
  (`compress-cast.py`); every frame shown really happened.
- One disclosed exception: the account e-mail in claude's welcome header is
  replaced byte-level with `demo@example.com` (same length, PII redaction —
  nothing about the product is altered).
- The camera picture-in-picture may only show the snapshot fetched in that
  exact session (`record-camera.sh` preserves it).

## Prerequisites (macOS)

| Tool | Install | Why |
|------|---------|-----|
| tmux, asciinema, ffmpeg, jq | `brew install tmux asciinema ffmpeg jq` | drive + record + post-process |
| vhs (pulls ttyd) | `brew install vhs` | replay renderer (Chromium: correct color emoji) |
| gif2webp | `brew install webp` | GIF -> animated WebP (Homebrew ffmpeg ships without libwebp) |
| pyte, Pillow | `python3 -m pip install pyte Pillow` | terminal emulation in `compress-cast.py`; cursor-overlay drawing in `render.sh` |
| JetBrains Mono | `cp` the two TTFs from [JetBrainsMono releases](https://github.com/JetBrains/JetBrainsMono) to `~/Library/Fonts/` (Regular + Bold) | deterministic glyph metrics in vhs |

Plus: the `ha-nova` CLI onboarded against a live Home Assistant, and the
`claude` CLI with the ha-nova plugin installed.

## Pipeline

```
record-*.sh       real session in tmux (exact grid!), asciinema cast
compress-cast.py  e-mail redaction + tail cut (/exit onwards)
                  + busy-window rescale + gap capping + markers.json
scrub-check.sh    privacy gate on the compressed cast       [blocks take]
render.sh         vhs replay -> [PiP] -> gif2webp -> size gate -> QA frames
```

### Why the odd architecture

Verified constraints force it (see the 2026-07 refresh PR):

1. **Claude's spinner defeats idle limiting.** The TUI repaints ~10x/s while
   thinking, so `asciinema rec -i` / renderer idle limits never fire.
   `compress-cast.py` rescales those windows on the timestamp level instead.
2. **Terminal grid must match exactly.** The fullscreen TUI stream garbles if
   the replay terminal differs by even one column. `geometry.env` carries the
   calibrated vhs pixel size for the tmux grid; recalibrate with
   `calibrate-geometry.sh` after changing font or vhs versions.
3. **Only trailing cuts are safe.** Cutting events at the start drops
   terminal-state setup (scroll regions) that later bytes depend on; the
   session therefore starts at claude's launch and the welcome header's
   e-mail is neutralized by redaction instead (ANSI-sequence-aware — a naive
   regex eats SGR terminators and garbles the replay).
4. **agg cannot render color emoji on macOS** (Cards depend on them), and
   Homebrew ffmpeg has no libwebp. Hence vhs for rendering and gif2webp for
   the final conversion.

## Recording a new set

```bash
cd scripts/demo

# once per machine
bash calibrate-geometry.sh              # refresh geometry.env if needed

# once per recording day: warm-up (absorbs trust dialog + one-time notices)
mkdir -p ~/smart-home && cd ~/smart-home && claude   # say hi, /exit

# hero (creates + reverts a real automation; cleanup after EVERY take)
bash record-hero.sh --take 1
bash cleanup-hero.sh

# review (read-only; dry-run unrecorded first, watch for sensitive names)
bash record-review.sh --take 1 --scope "light automations"

# camera (read-only; preserves the session snapshot for the PiP)
bash record-camera.sh --take 1

# compress (redact + cut + tighten) -> scrub gate -> render
python3 compress-cast.py ../../.artifacts/demo/hero/take-1.cast hero.cast \
  --prompt-hint "When the sun sets"
bash scrub-check.sh hero.cast
bash render.sh hero.cast ../../assets/demo-hero.webp --max-kb 3072

python3 compress-cast.py ../../.artifacts/demo/camera/take-1.cast camera.cast \
  --prompt-hint "Check the garage camera"
bash scrub-check.sh camera.cast
bash render.sh camera.cast ../../assets/demo-camera.webp --max-kb 1536 \
  --pip-image ../../.artifacts/demo/camera/take-1-snapshot.jpg \
  --pip-label "camera.garage — live snapshot"
```

Selected final casts may be committed to `scripts/demo/casts/` so a future
theme or size change can re-render without re-recording — commit only casts
that passed the scrub gate, and re-record instead when the recorded UI has
since changed.

## Acceptance gates per asset

- Scrub gate passed; exported txt eyeballed once.
- Hero: Preview Card (behavior sentences on create; the before/after change
  table appears on updates) -> `apply` -> Result Card with revert offer.
  Clarifying-question menus and auto-confirmed approval prompts are fine —
  they are the real product — but no errors, English throughout.
- Review: >= 2 severity findings in plain language, no internal check codes,
  no sensitive automation names.
- Camera: snapshot fetch visible, PiP shows the session's real frame, clear
  verdict; nothing sensitive in image or description.
- Size budgets: hero <= 3 MB, review <= 1.5 MB, camera <= 1.5 MB.
