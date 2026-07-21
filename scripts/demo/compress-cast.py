#!/usr/bin/env python3
"""compress-cast.py — tighten a real asciinema recording without faking it.

Approach: a uniform time-lapse toward --target seconds, with driver-declared
protection. The recording driver logs its own typing windows (epoch pairs in
<cast>.protect, written by lib.sh) — those keep their natural cadence, and
intervals right after a content marker (preview card, options line, result
card) keep a reading hold. Everything else — thinking pauses, spinner phases,
tool latency, streamed output — is scaled uniformly. Every frame shown really
happened; only time shrinks. (Content-based busy-detection was tried first
and lost: spinner tick shapes and periodic full repaints vary too much across
claude versions to classify reliably. The spinner also ticks ~10x/s, so
interval-length heuristics cannot tell thinking from typing.)

Also:
- E-mail addresses are redacted byte-level (ANSI-sequence-aware — a naive
  regex eats SGR terminators and garbles the replay; verified). The claude
  welcome header prints the account e-mail and survives every repaint.
- The tail is cut in event space: everything from the typed "/exit" onwards
  (teardown, resume screen) is dropped. Trailing cuts are safe; LEADING cuts
  drop terminal-state setup later bytes depend on and garble the replay.
- Emits <out>.markers.json with post-compression timestamps for render.sh
  (camera picture-in-picture timing).
- A final no-op event is appended: asciinema play can swallow the very last
  event's rendering otherwise.
"""
import argparse
import json
import re
import sys

EMAIL_RE = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}")
ANSI_RE = re.compile(
    r"\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[()][0-9A-Za-z]|[@-_])"
)
MARKER_PATTERNS = {
    "preview_card": "Preview:",
    "options_line": "Options:",
    "result_card": "Saved",
    "revert_offer": "revert",
    "delete_card": "Delete",
    "snapshot": "snapshot",
    "exit_typed": "/exit",
}
# Intervals following these markers keep at least READ_HOLD seconds — the
# viewer must be able to actually read cards and menus (user requirement:
# fast-forward the waiting, hold the decisions).
READ_MARKERS = ("preview_card", "options_line", "result_card", "revert_offer",
                "delete_card")
# Menu screens repaint often; a new hold is granted when one appears more
# than MENU_GAP real seconds after the previous hold point.
MENU_PATTERNS = ("Enter to select", "Ready to submit")
MENU_GAP = 8.0
READ_HOLD = 3.0


def _mask(m):
    s = m.group(0)
    repl = "demo@example.com"
    return (repl + " " * len(s))[: len(s)] if len(s) >= len(repl) else "*" * len(s)


def redact(data):
    out, pos = [], 0
    for m in ANSI_RE.finditer(data):
        out.append(EMAIL_RE.sub(_mask, data[pos:m.start()]))
        out.append(m.group(0))
        pos = m.end()
    out.append(EMAIL_RE.sub(_mask, data[pos:]))
    return "".join(out)


def load(path):
    with open(path) as f:
        lines = [l for l in f if l.strip()]
    return json.loads(lines[0]), [json.loads(l) for l in lines[1:]]


def find_marker_indices(events, prompt_hint):
    idx = {}
    for i, (_, _, data) in enumerate(events):
        if prompt_hint and "first_input" not in idx and prompt_hint[:24] in data:
            idx["first_input"] = i
        for name, pat in MARKER_PATTERNS.items():
            if name not in idx and pat in data:
                idx[name] = i
    return idx


def cut_tail(events, cut_end):
    """Drop trailing events (typed /exit, teardown, resume screen)."""
    if cut_end is None:
        return events
    kept = [e for e in events if e[0] <= cut_end]
    return kept or events


PGUP, PGDN = "\x1b[5~", "\x1b[6~"


def windows_from_input_events(events):
    """Derive protection windows from captured input events ("i" type,
    recorded via `asciinema rec --capture-input`) — on the cast's own clock,
    immune to the wall-clock drift that killed the sidecar approach.

    Returns (windows, cut_end):
    - "type" windows: clusters of >= 3 printable keystrokes ending in Enter
      (the typed prompts; menu picks like "1"/Tab don't qualify)
    - "read" windows: pauses > 1.5s between consecutive PageDown events —
      the reading holds of read_back(); they also anchor the reading cursor
    - cut_end: start of the final "/exit" cluster (tail cut), or None
    """
    ins = [(t, d) for t, ty, d in events if ty == "i"]
    windows, cut_end = [], None

    cluster = []  # [(t, ch)]
    for t, d in ins:
        if d == "\r":
            if len(cluster) >= 3:
                text = "".join(ch for _, ch in cluster)
                if text.startswith("/exit"):
                    cut_end = cluster[0][0] - 0.3
                else:
                    windows.append((cluster[0][0] - 0.2, t + 0.2, "type"))
            cluster = []
        elif len(d) == 1 and d.isprintable():
            if cluster and t - cluster[-1][0] > 0.8:
                cluster = []
            cluster.append((t, d))
        else:
            cluster = []

    # Any PageUp/PageDown marks an (abandoned) TUI scroll attempt — the TUI
    # auto-jumps back to the bottom on every repaint, so scrolling is not
    # usable for reading holds. Cut the recording before the first one.
    pages = [t for t, d in ins if d in (PGUP, PGDN)]
    if pages:
        first_page = min(pages) - 0.3
        cut_end = min(cut_end, first_page) if cut_end else first_page

    return windows, cut_end


def in_windows(t, windows, kinds=("type",)):
    return any(a <= t <= b for a, b, k in windows if k in kinds)


def hold_points(events, marker_idx):
    """Event indices after which the timeline must pause for READ_HOLD."""
    holds = {marker_idx[m] for m in READ_MARKERS if m in marker_idx}
    last_hold_t = None
    for i, (t, _, data) in enumerate(events):
        if any(p in data for p in MENU_PATTERNS):
            if last_hold_t is None or t - last_hold_t > MENU_GAP:
                holds.add(i)
                last_hold_t = t
    return holds


def new_text_per_event(events, cols, rows):
    """Ground truth via terminal emulation: per event, how many characters of
    genuinely NEW visible text appear. A line only counts once it is STABLE
    across two consecutive events (i.e. finished streaming) — otherwise every
    growing prefix of a streamed line would count again. Digits are masked so
    spinner ticks and counters do not count as new text."""
    import pyte
    screen = pyte.Screen(cols, rows)
    stream = pyte.Stream(screen)
    # Word-level dedup: the TUI repaints the same content in many wrap and
    # indent variants (stream buffer vs. transcript vs. after scroll), so
    # line-level sets over-count massively. A word counts once, ever.
    word_re = re.compile(r"[A-Za-zÀ-ſ_./:-]{4,}")
    # Command echoes, heredocs and permission boxes are tool internals, not
    # reading material — they must fly by at cut pace.
    skip_line_re = re.compile(
        r"^\$ |<<'EOF'|^EOF\b|--data-file|--jq-file|--out |/scratchpad/"
        r"|^Bash command|Do you want to proceed|^1\. Yes|^2\. No"
        r"|Esc to cancel|Contains .*(quote|expansion)|^done\b|^echo |^cat "
        r"|relay (ws|core) |^for \w+ in |requires approval"
    )
    seen_words = set()
    prev_display = []
    out = []
    for _, _, data in events:
        try:
            stream.feed(data)
        except Exception:
            pass
        display = [l.strip() for l in screen.display]
        prev_set = set(prev_display)
        new = 0
        for s in display:
            if len(s) < 6 or s not in prev_set:
                continue
            # Terminal-integration junk (Warp notify OSC leaking through
            # tmux) is not reading material.
            if "warp://" in s or '"cwd"' in s:
                continue
            if skip_line_re.search(s):
                continue
            # Positive prose gate: reading time is only earned by sentence-
            # like lines. JSON payloads, jq filters and heredocs structurally
            # fail this (few pure words, or shell/JSON punctuation).
            if sum(1 for w in s.split()
                   if w.strip(".,:;()!?·—-\"'`").isalpha()) < 3:
                continue
            if any(c in s for c in "{}<>") or "--" in s or "/private/" in s:
                continue
            for w in word_re.findall(s):
                if len(w) > 15 and "/" in w:
                    continue  # paths/ids — nobody reads those
                if w not in seen_words:
                    seen_words.add(w)
                    new += min(len(w), 12) + 1
        prev_display = display
        out.append(new)
    return out


def compress(events, marker_idx, windows, new_chars, target, max_dt,
             typing_max, skim_rate):
    times = [e[0] for e in events]
    dts = [times[0]] + [times[k] - times[k - 1] for k in range(1, len(times))]
    hold_after = hold_points(events, marker_idx)

    # An interval is protected only when it lies fully inside a typing window
    # (both endpoints) — otherwise a long wait that merely ENDS at the first
    # keystroke would be exempted from scaling.
    protected = [
        in_windows(times[k], windows) and in_windows(times[k] - dts[k], windows)
        for k in range(len(dts))
    ]

    orig_dts = list(dts)

    # Typing keeps its cadence LOOK but not its full duration: squeeze each
    # typing window proportionally to at most typing_max seconds.
    for a, b, kind in windows:
        if kind != "type":
            continue
        ks = [k for k in range(len(dts)) if protected[k] and a <= times[k] <= b]
        w_dur = sum(dts[k] for k in ks)
        if w_dur > typing_max:
            tf = typing_max / w_dur
            for k in ks:
                dts[k] *= tf

    prot_sum = sum(dt for k, dt in enumerate(dts) if protected[k])
    free = [(k, dt) for k, dt in enumerate(dts) if not protected[k]]
    free_sum = sum(dt for _, dt in free)

    budget = max(target - prot_sum, 5.0)
    f = min(1.0, budget / free_sum) if free_sum > 0 else 1.0

    for k, dt in free:
        floor = READ_HOLD if (k - 1) in hold_after else 0.0
        dts[k] = min(max(dt * f, floor), max(max_dt, floor))

    # READING TIME (the core viewer requirement), measured on ground truth:
    # new_chars[k] is how much genuinely new text event k painted (terminal
    # emulation; digits masked so spinners/counters don't count). Every burst
    # of new text earns dwell time at skim rate — so the top of a multi-screen
    # answer stays visible before it scrolls off — and a pause after a burst
    # earns a reading hold proportional to what just appeared.
    for k in range(len(dts)):
        if new_chars[k] > 90:
            dts[k] = max(dts[k], min(new_chars[k] / skim_rate, 6.0))

    read_holds = []
    for k in range(len(dts) - 1):
        if orig_dts[k + 1] < 3.0:
            continue
        recent = 0
        j = k
        while j >= 0 and times[k] - times[j] <= 5.0:
            recent += new_chars[j]
            j -= 1
        if recent < 120:
            continue
        hold = min(max(1.5 + recent / 90.0, 2.5), 8.0)
        if hold > dts[k + 1]:
            dts[k + 1] = hold
        read_holds.append(k + 1)

    t = 0.0
    out = []
    new_times = []
    for k, (_, typ, data) in enumerate(events):
        t += dts[k]
        out.append([round(t, 4), typ, data])
        new_times.append(t)

    def remap(t_old):
        """Old-clock time -> new-clock time (nearest following event)."""
        for k in range(len(times)):
            if times[k] >= t_old:
                return new_times[k]
        return new_times[-1]

    # Reading windows for the cursor overlay: contiguous new-text regions
    # (gap < 4s on the original clock) plus the explicit holds.
    hold_windows = [
        (round(new_times[k] - dts[k], 2), round(new_times[k], 2), "read")
        for k in read_holds
    ]
    first = None
    last = None
    for k in range(len(dts)):
        if new_chars[k] > 90:
            if first is not None and times[k] - times[last] > 4.0:
                if new_times[last] - new_times[first] >= 2.5:
                    hold_windows.append((round(new_times[first] - dts[first], 2),
                                         round(new_times[last], 2), "read"))
                first = None
            if first is None:
                first = k
            last = k
    if first is not None and new_times[last] - new_times[first] >= 2.5:
        hold_windows.append((round(new_times[first] - dts[first], 2),
                             round(new_times[last], 2), "read"))
    return out, remap, hold_windows


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("cast_in")
    ap.add_argument("cast_out")
    ap.add_argument("--target", type=float, default=45.0,
                    help="rough target duration in seconds")
    ap.add_argument("--max-dt", type=float, default=0.4,
                    help="hard cap for any single non-hold interval — work "
                         "phases read as cuts, like the original demo")
    ap.add_argument("--typing-max", type=float, default=2.5,
                    help="squeeze each typing window to at most this")
    ap.add_argument("--skim-rate", type=float, default=40.0,
                    help="unique-word chars per second granted as dwell time")
    ap.add_argument("--prompt-hint", default="", help="first words of the typed prompt")
    ap.add_argument("--no-cut", action="store_true", help="keep the full session")
    args = ap.parse_args()

    header, events = load(args.cast_in)
    if not events:
        sys.exit("empty cast")

    before = events[-1][0]
    windows, cut_end = windows_from_input_events(events)
    # Input events served their purpose; only output events are replayed.
    events = [[t, typ, redact(data)] for t, typ, data in events if typ == "o"]
    if not args.no_cut:
        events = cut_tail(events, cut_end)
    # Clamp the open-ended answer window to the actual cast end.
    last_t = events[-1][0]
    windows = [(a, min(b, last_t), k) for a, b, k in windows if a < last_t]
    marker_idx = find_marker_indices(events, args.prompt_hint)
    cast_end = events[-1][0]
    new_chars = new_text_per_event(events, header.get("width", 106),
                                   header.get("height", 21))
    events, remap, hold_windows = compress(events, marker_idx, windows,
                                           new_chars, args.target,
                                           args.max_dt, args.typing_max,
                                           args.skim_rate)
    events.append([round(events[-1][0] + 0.5, 4), "o", "\x1b[0m"])

    with open(args.cast_out, "w") as f:
        f.write(json.dumps(header) + "\n")
        for e in events:
            f.write(json.dumps(e, ensure_ascii=False) + "\n")

    markers = {name: events[i][0] for name, i in marker_idx.items() if i < len(events)}
    markers["end"] = events[-1][0]
    # Windows on the compressed clock — render.sh anchors the reading cursor
    # to the "read" entries.
    markers["windows"] = [
        [round(remap(max(a, 0)), 2), round(remap(min(b, cast_end)), 2), k]
        for a, b, k in windows
        if a < cast_end
    ] + [list(w) for w in hold_windows]
    with open(args.cast_out + ".markers.json", "w") as f:
        json.dump(markers, f, indent=2, sort_keys=True)

    print(f"{args.cast_in}: {before:.1f}s -> {events[-1][0]:.1f}s "
          f"(markers: {', '.join(sorted(markers))})")


if __name__ == "__main__":
    main()
