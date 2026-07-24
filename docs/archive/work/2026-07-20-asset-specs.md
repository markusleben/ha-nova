# Relaunch asset specs — generated images (v0.19.0)

> These specs are the *source of truth* for the four relaunch images.
> Hero + social are pure SVG (sources in `docs/archive/work/2026-07-20-*-composite.svg`;
> render with `rsvg-convert` at 2× + `sips` downscale). Pairing-flow +
> how-it-works composite the same star SVG onto AI backgrounds generated via
> `codex exec` (logged-in account) — their prompts below keep them
> reproducible.
>
> Files land in `assets/` but stay unreferenced by the published README until
> the v0.19.0 release-prep PR. The GitHub social-preview upload (repo settings)
> happens only at release time.
>
> The AI-generated background rasters referenced by the composite SVGs
> (`howitworks-bg-nostar.png`, `pairing-bg-noglow.png`,
> `skillsvstools-bg-v2.png`) are committed alongside them in this directory, so
> `rsvg-convert` reproduces every committed asset from a fresh checkout.

## Shared visual system (all four images)

- **One star everywhere (maintainer requirement 2026-07-20):** every image
  carries the SAME composite star — exact `logo.svg` path geometry (four
  equal concave arms + tick lines + the small core disc) — as a byte-identical
  SVG block, only translated/scaled per image. No model-drawn stars anywhere
  (image models cannot reproduce the mark consistently; verified over
  multiple attempts). `logo.svg`/`icon.svg` themselves stay untouched as the
  canonical marks. Backgrounds are generated star-free ("NO star/sparkle"
  clause) and the star is composited via the wrapper SVGs in `docs/archive/work/` +
  `rsvg-convert`.
- **Plain star, no glow, v3 (maintainer decisions 2026-07-20, two rounds):**
  glow styling (halo + clipped core glow) read artifact-y at 1:1 (ghost rim,
  milky ring) → removed. The remaining artifacts at zoom were (a) the grainy
  AI glow cloud behind the star, (b) the muddy grey-green midzone of the
  plain cyan→amber blend, (c) the tick dashes reading as glitches when
  enlarged. Fixes: star fill is now a 4-stop vertical gradient routed through
  brightness (`#18BCF2 → 45% #8FD9E8 → 70% #FFD9A0 → #FFB347` — no olive
  valley, middle reads naturally luminous), ticks dropped in artwork
  (logo.svg keeps them), and no AI pixels behind the star anywhere.
- **Hero + social are FULL VECTOR now** (no AI raster at all): deterministic
  cosmos gradient + starfield dots + SVG system-font typography + the star
  block — zero grain at any zoom, reproducible from the SVG sources alone
  (the `*-composite.svg` filenames are historical; those two contain no
  raster). AI imagery remains only where it is real illustration
  (pairing-flow, how-it-works), and there never directly behind the star:
  the how-it-works node has the dark inset; the pairing background is
  regenerated without any glow spot.
- **Render pipeline:** one star block shared across all four sources, all
  rendered at 2× supersampling (`rsvg-convert -w 2W -h 2H` → `sips -z H W`)
  to minimize banding/aliasing.

- Background: dark cosmos, gradient `#0A0E1A → #0D1525 → #0A1628`, sparse tiny
  white stars at low opacity (~30%).
- The NOVA star: one large four-point star (concave "sparkle" silhouette, like
  `assets/logo.svg`), linear gradient cyan `#18BCF2` → warm amber `#FFB347`,
  soft white/warm glowing core.
- Typography: clean system-style sans, white `#E8EDF2` for headlines, muted
  blue-grey `#B8CCE0` / `#6B8BA8` for secondary text. Few, large, exactly
  specified labels — image models mangle small text.
- Tagline everywhere: **"One code to connect. Every change checked."**
- Must read well on GitHub light AND dark backgrounds (the image carries its
  own dark background, so both work).
- No watermarks, no invented extra text, no fake UI chrome beyond what the
  spec asks for.
- Text check on every attempt: exact spelling of all specified strings;
  re-roll on any glitch (up to 3 attempts) before falling back to the hybrid
  path (generated background + text overlay composited via `rsvg-convert`).

## Image 1 — Hero banner (`assets/hero-banner.png`)

- Size: 1600×400 (wide README header). If the generator only offers fixed
  sizes, generate 1536×1024 landscape composed for a wide center crop, then
  crop/resize with `sips`.
- Composition: star left of center with ambient glow; right side a
  left-aligned text block: wordmark **HA NOVA** (bold, wide letter-spacing,
  `#E8EDF2`) and beneath it the tagline (lighter weight, `#B8CCE0`).
- Text (exact, nothing else): `HA NOVA` + `One code to connect. Every change checked.`
- Role in README: topmost visual, above badges and the `<h2>` headline.
- Alt text: "HA NOVA — a four-point star, and the line: one code to connect,
  every change checked".

### Final prompt (v1 probe)

Dark cosmic hero banner, wide 4:1 crop composition. Background: deep space
gradient from #0A0E1A to #0A1628 with sparse tiny white stars at low opacity.
Left of center: one large elegant four-point star (concave sparkle silhouette),
smooth linear gradient from cyan #18BCF2 (top left) to warm amber #FFB347
(bottom right), soft glowing white-warm core, subtle ambient glow. Right half,
left-aligned modern sans-serif text block: bold wordmark "HA NOVA" in near-white
#E8EDF2 with wide letter spacing, and below it in lighter weight, muted
blue-grey #B8CCE0: "One code to connect. Every change checked." Exactly this
text, correctly spelled, crisp and legible; no other text. Style: minimal,
premium, clean; no clutter, no watermark, no UI elements.

- Model/attempt: OpenAI built-in `image_gen` via `codex exec` (imagegen skill,
  gpt-image-2 backend). Iterations: v1 (star tips cropped — maintainer asked
  for the star fully in frame with four equal arms), v2 (fully in frame but
  vertically elongated), v3 (hard geometry constraints — model still cannot
  produce equal arms; codex confirmed after 3 attempts).
- **Final pipeline (full vector):** after two hybrid rounds (AI background +
  composited star) still showed grain/mud at 1:1, the hero became a pure SVG —
  vector cosmos gradient + starfield dots + v3 star block + system-font
  typography. Source: `docs/archive/work/2026-07-20-hero-banner-composite.svg`
  (filename historical; contains no raster). The v1–v3 prompt/iteration notes
  above are kept as history only. Status: approved 2026-07-20.

## Image 2 — Pairing flow (`assets/pairing-flow.png`)

- 1600×500, three panels on a continuous cosmos strip: (1) terminal window
  with an abstract glowing prompt line (no readable words), (2) NOVA-page card
  with sparkle star, pill button "Connect a device", glowing code digits
  "428 316", (3) device card with laptop, gradient checkmark, revoke toggle.
  Big numerals 1/2/3. Only readable text: `1 2 3`, `Connect a device`,
  `428 316`. Sits directly under the "Three steps, one code" list, which
  carries the captions — the image stays nearly text-free by design.
- Result: built-in `image_gen` via `codex exec`. Iterations: model-drawn star
  (inconsistent) → star-free bg with glow spot (star washed out) → final:
  star-free AND glow-free bg, regenerated with the how-it-works background
  attached via `-i` as a style reference for a coherent look; the v3 star
  block is composited onto the flat dark card, no vignette needed
  (`docs/archive/work/2026-07-20-pairing-flow-composite.svg`). Text constraint held
  exactly in every generation. Status: approved 2026-07-20.

## Image 3 — How it works (`assets/how-it-works-v2.png`)

- 1600×600 three-zone diagram: laptop + markdown "Skills" stack (Your
  machine) → padlocked cyan link → NOVA Relay node (four-point star, code
  chip "428 316" for continuity with image 2) → short amber link → Home
  Assistant house. Only readable text: `Your machine`, `Skills`,
  `NOVA Relay`, `428 316`, `Home Assistant`.
- Result: built-in `image_gen` via `codex exec`. First version carried a
  model-drawn star — background regenerated star-free (empty glowing node,
  1 attempt), then a dark "window into space" inset drawn into the node and
  the canonical star block composited inside
  (`docs/archive/work/2026-07-20-how-it-works-composite.svg`). Labels exact in every
  generation. New file name on purpose; replaces `how-it-works.png` only in
  the release-prep PR. Status: approved 2026-07-20.

## Image 5 — Skills vs. tools (`assets/skills-vs-tools.png`)

- 1600×500 split illustration for the README's "Why not an MCP server?"
  section. LEFT: laptop + session window flooded by a cascade of identical
  tool tiles from a tall stack, a flat amber key lying beside the laptop
  (credential parked on the client); label "The whole catalog, every
  session." RIGHT: same laptop, ONE markdown sheet gliding into the session,
  server block with padlock, code chip "428 316", the canonical v3 star
  composited above the server; label "One skill, on demand."
- Only readable text: the two labels + `428 316`. Style-referenced (`-i`)
  against the how-it-works background for coherence; background generated
  star-free, star composited via wrapper SVG at 2× supersampling.
- Honesty check: "token parked on the laptop" and "whole catalog per session"
  are documented category defaults on the comparison page; the image
  illustrates exactly what the adjacent table claims.
- Result: bg via `codex exec` (style-referenced, 2 attempts), then a
  truth-hardening edit pass (verified against ha-mcp's live README
  2026-07-20): the amber key beside the left laptop was REMOVED (ha-mcp's
  recommended install is in-process, "no access token to manage" — a
  parked-credential metaphor is not category-true anymore) and the left label
  became "The whole catalog, by default." (their README states verbatim that
  the full catalog is listed by default; deferred-loading Claude clients pull
  tools on demand, so the absolute "every session" claim was dropped).
  Canonical v3 star composited above the server
  (`docs/archive/work/2026-07-20-skills-vs-tools-composite.svg`), 2× supersampled.
  Status: pending maintainer review.

## Image 4 — Social preview (1280×640)

- Same hybrid pipeline as image 1: codex generates a star-free 1280×640 card
  background (cosmos, soft glow left at ~27%/48%, wordmark + tagline right,
  ≥60px safe margins, subtle cyan→amber accent line along the bottom; 3
  attempts), then the exact vector star is composited into the glow via
  `docs/archive/work/2026-07-20-social-preview-composite.svg` + `rsvg-convert`.
  Replaces the old "AI that reads before it writes" tagline with
  "One code to connect. Every change checked."
- The release-prep PR replaces `assets/social-preview.png` content directly
  (the `-v2` filename was only the pre-PR working copy). The GitHub repo
  settings upload of this file still happens **only at release time**
  (instantly public). Status: approved 2026-07-20.
