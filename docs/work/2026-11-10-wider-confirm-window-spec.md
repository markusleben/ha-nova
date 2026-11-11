# Wider, clearer confirmation window (MAR-135)

Status: active

Date: 2026-11-10. Owner: CLI confirmation window (`cli/confirm_window.go`, `cli/confirm_window_darwin.go`, `cli/confirm_say.go`, `cli/confirm_batch_text.go`). Target: 0.30.

## Today

- macOS shows a typed plan's window with `/usr/bin/osascript` `display dialog`. Its text column is fixed at about 50 characters, so a long plan is a tall, narrow block. Markus's real-window test (10.11.): nothing cut, but "sehr unübersichtlich … die Breite des Fensters würde es viel lesbarer machen und evtl. auch mal eine Leerzeile".
- Window text = instruction lines, one blank line, then the plan's prompt. A single plan's prompt is one run of sentences: what happens, why it needs the user, "nothing has run yet". Raw and batch prompts put their list on separate lines, then the reasons right below, with no blank line.
- Budget (`windowFits`, #1083/#1085): at most 44 lines of 46 characters, word-wrapped, CJK double, on every OS. Over it the plan is refused (`PLAN_TOO_LONG` / `BATCH_TOO_LONG`) with the split size N, never shown cut.
- Windows uses `MessageBoxTimeoutW`. Its width is the smallest that fits of the longest line, 278 dialog units (about 470 px, roughly 70 characters), and 5/8, 6/8, or 7/8 of the work area. It has no scroll. Linux uses zenity (untested on a real desktop).

## Change

1. **macOS window: `osascript -l JavaScript` with an `NSAlert`.** This ships with macOS, so there's no new binary to sign. The script stays a fixed list of `-e` lines, and the message, wait, and button labels still arrive only as arguments (`argv`). The environment stays empty and the path stays absolute.
   - The text goes in a non-editable `NSTextView` 560 pt wide (system font 13 pt, about 85 characters a line), as the alert's accessory view. The alert's bold title is "HA NOVA", with the caution icon. `setActivationPolicy(1)` (accessory) means there's no Dock icon.
   - Safety net: if the text needs more height than the screen's visible frame leaves (minus 200 pt for the alert's frame), the view scrolls instead of being cut. The scroller uses the legacy style and never hides, so more text below is always visible. The budget keeps this from happening for ordinary text on a 1280×800 screen or larger.
   - Buttons: Cancel is added first, so it is the default and Return cancels. Confirm has no key equivalent. Escape probably does nothing with a localized Cancel label; that is safe, and Markus checks it visually.
   - Answers keep today's stdout protocol: `button Confirm` (second button), `cancel` (first button), and `timeout` for any other modal result. The `timeout` comes from an `abortModal` scheduled for `wait` seconds in the modal run-loop mode, which replaces the old `giving up after`.
   - The script reads the screen first. Without a window server, as in a sandbox, that throws and the script exits non-zero without the `ha-nova-window-showing` line, which gives `windowFailed`, the same as a script that doesn't compile today (proof: the exact script under `sandbox-exec` with `deny mach-lookup`). Everything after that runs inside `try`. Any other error, before or after the log, prints `error …`, which means no answer, fail closed. So a future AppKit or bridge change can't silently move every unlock to a weaker channel. The marker goes to stderr, where `nativeWindow` reads it; a test pins that.
   - `darwinAnswer`'s `error -1713` case (from `display dialog`) is gone, so that output now means no answer, which is stricter. Answer codes keep their meaning: Confirm=1, Cancel=2, failed=3, closed=4.
   - Activation: `NSRunningApplication.currentApplication.activateWithOptions(NSApplicationActivateAllWindows)` brings the window in front. Measured: frontmost app = osascript.
2. **Blank lines between sections, in every window (and form) text:** the instruction lines (what the buttons mean, whether the phone asks too), then what happens (one sentence or the list), then the consequences (why it needs the user, copies, "nothing has run yet"). Each block is separated by one empty line.
   - Single, config, and raw plans: `sayBody` returns the action and the rest separately. `confirmationPrompt` joins them with `"\n\n"`, and `confirmationSay` (chat) joins them as before.
   - Batches: `batchText` returns the list and the tail separately. The plan's prompt joins them with `"\n\n"`, and the chat `say` with `"\n"`.
   - No new text. Every word still comes from `cli/confirm_strings.*.json`, and names still enter only through `sayText`.
3. **Budget, measured again for the new window:** `windowWrap` 46 → 64 and `windowMaxLines` 44 → 32, on every OS. `windowFixedLines` stays 12; it measures 8 to 10 at the new wrap, and the larger value only makes split plans slightly smaller.
   - Measured headless with the same `NSTextView` at 560 pt / 13 pt (line height 16 pt):

     | Text | Real lines | Estimated lines (wrap 64) |
     |---|---|---|
     | 8 imports, DE/EN | 12 | 18 / 16 |
     | 23 unlocks (the most that fit), DE/EN | 24 | 32 |
     | 40 all-caps names | 12 | 14 |
     | 40 "WMWM" names | 10 | 10 |

     The estimate covers ordinary text; a run of only wide letters can exceed it, and the scroller covers that.
   - Height: the alert's frame takes about 190 pt. At the ceiling, 32 lines × 16 pt = 512 pt, so a 1280×800 Mac (about 720 pt visible) never needs the scroller for ordinary text.
   - Windows: Fable showed that short hard-newline lines (a batch list) don't get shorter when the box widens, and that 36 lines (about 675 px with the frame) would be cut on 1920×1080 at 150 % (a 672 px work area, the default on 13–14" laptops). 32 lines come to about 615 px. The old 44-line budget could overflow there.
   - Effect: capacity is about the old one (32 × 64 = 2,048 characters, against 44 × 46 = 2,024): 23 long unlocks fit as before, a batch of 20 deletes with 60-character names is still refused with the split size, and the window is wider, with blank lines. All-caps names wrap at about 55–60 characters on Windows (278 DLU averages a–z and A–Z); those lines wrap and the box widens when tall.
   - The ratchet test pins the new values in the correct direction. The old check `windowWrap < 46` fired when the budget got stricter, not looser.
4. **Every OS, only what it ships (Markus, 10.11.: "auf jedem System immer funktionieren … keine speziellen Libs").** There are no Go GUI libraries, no cgo, and no new binary or package. Every OS gets the same three blocks (the blank lines come from the shared text) and the same budget.
   - **macOS:** `/usr/bin/osascript` with JXA and AppKit, as described above.
   - **Windows:** no code change. It keeps `MessageBoxTimeoutW` from user32, in process, with its guards (OK is off for 1.5 s, the box is brought to the front and kept on top, and it is withdrawn by thread). The box already wraps at 278 dialog units, about 470 px or 65–70 characters, against about 50 for the old macOS dialog. When the text would be taller than the work area, the box widens to 5/8, 6/8, then 7/8 of the screen. So it isn't narrow the way the old macOS dialog was.
     - A PowerShell WinForms or WPF window was considered and not taken. It would be a separate process started by path, and every in-process guard would have to be rebuilt in a script, for about 15 more characters a line.
     - Markus takes one look at the `long` fixture and a 20-member batch (short lines) on the Win11 VM, at 1366×768 and at 1920×1080 with 150 %, to confirm the box isn't cut. No lane covers the native window. If it is cut, `windowMaxLines` drops to the measured value.
   - **Linux:** `/usr/bin/zenity --question` as today (the only dialog it uses, by absolute path, with an allowlisted environment), now with `--width=600` so it is as wide as the macOS window. Zenity's `--no-markup` and `--default-cancel` stay.
     - Where zenity is missing or there is no display, `nativeWindowAvailable` is false and the plan never becomes a window plan: the linked phone, a form, or a chat code asks, as today. Zenity reporting at start that it can't open the display is `windowFailed`, as today. Nothing new is installed or required.
     - Zenity is still untested on a real desktop, and it doesn't scroll (GTK lines are about 20 px, so 32 lines come to about 640 px plus the frame). Until Markus has looked at the `long` fixture on the Ubuntu VM, Linux is best effort. A machine without zenity has no confirmation window at all, but a confirmation is still available through the existing channels.
5. **CI:** `TestDarwinWindowScript` (the script compiles with `osacompile`, takes text only as an argument, Cancel comes first, Confirm has no key, and it logs right before showing) and `TestDarwinWindowAnswers` join the `macos-native-secret-boundary` job's run list. Until now no darwin window test ran in CI.

## Not changed

- Channels, fallbacks, the phone, forms, and chat codes. A window that can't open at all still falls back to the phone, a form, or a code, as `docs/reference/safety.md` states. Astra flagged this pre-existing fallback against the "any error fails closed" goal. It is out of scope here and is not widened: only a failure before the window shows reaches it.
- Texts in `confirm_strings.*.json`.

## Reviews of the diff (before the PR, before the every-OS requirement)

- Fail-open and sibling sweep (Sonnet): no P1. Its P2 was that any JS error before the log reached the fallback. Fixed with the `try` above. Every reader of `Prompt` treats it as an opaque string, `rawNoCopies` and `batchNoCopies` still match, and `Prompt` is not in the plan hash.
- UX (Sonnet): no P1. P2s fixed: the scroll view is widened by the scroller's width so it never covers the text's right edge, and the drift notice (`verb_apply.go`) gets a block of its own in the window and the form.
- Local Codex CLI (gpt-6-luna; gpt-6.1-sol isn't available for this account): one P1, that the "showing" marker goes to stdout. Measured wrong: JXA's `console.log` writes to stderr. A test now pins it.

## Reviews of the every-OS delta

- Astra (medium): agree with changes. It agrees to keep MessageBox ("PowerShell is not inherently required"), to keep the no-zenity routing, and with `--width=600`. Its P1, that Windows has no fit check, is the same gap the old 44×46 budget had. This change narrows it (32 lines, measured against 150 % scaling) but doesn't close it. A fit check in the box (Confirm stays off if the box is taller than the work area) is offered as a follow-up, because it is untestable here without the VM. Its P2 is the Linux look, now part of the VM check.
- Fable (medium): agree with changes. Its P1, that 36 short lines get cut at 150 %, is fixed by 32 lines. Its P2 is the Ubuntu look, now required, with Linux best effort until then.

## Reviews of this spec

- Astra (gpt-6-astra, medium): agree with changes. Adopted: separate the action and the reasons in `sayBody` (raw plans), measure the final texts and wide glyphs, give a real-window check list, and remove the "only blank lines" claim for Windows. Kept a single budget for every OS (Fable agrees) because the Windows numbers above support it. The fallback point is noted under "Not changed".
- Fable (claude-fable-5-2, medium): agree with changes. Adopted: a legacy scroller that never hides (P1: overlay scrollers hid the tail), rewording "never undercounts", the Windows look, the `sayBody` split, the `-1713` change, and the Escape note. Fable says 520 pt would read slightly better; 560 is kept for "clearly wider", and it is one number to change.


## Rework after Markus's real-window test (10.11.)

Markus on the wide window (statistics, DE): "besser als vorher aber immer noch sehr viel Text und schlecht auf einen Blick zu erfassen". The width stays. What changes is the content, so it can be read at a glance (Captain's direction):

1. **Title = the action as one question**, the alert's bold `messageText` on macOS (it was "HA NOVA"):
   - One name: the goal as a question, never cut ("„Haustür“ entriegeln?").
   - Several names: a count where the verb says what they are ("Diese 23 entriegeln?", `window.these`), with any service data ("… mit Helligkeit 50"), and the names below. Names of a kind (scripts, Zigbee devices, apps) keep it: "Das Skript „A“, „B“, „C“ und 2 weitere ausführen?", with every name below.
   - Raw plans: one step, that step's goal. Steps that are one run of statistics imports: "6.671 Stundenwerte in 7 Statistiken importieren?" (`raw.import_title`). One run of deletes: its names, as the chat says them ("„A“, „B“, „C“ und 2 weitere löschen?"). Anything else: today's "8 Schritte in Home Assistant bestätigen?".
   - Batch: the phone's title ("4 Automationen löschen?").
   - Config plans (create, change, delete, restore): the goal; no second line restates it.
2. **What: one line per item.** Several names are bullets ("• „Haustür“"), every name as `sayText` quotes it, never "N more" (the budget refuses a plan that doesn't fit). A raw run of imports adds "Zeitraum: Nov. 2024 – Jan. 2026". A single target keeps today's sentence (its area and its change "verriegelt → entriegelt"). Raw plans with different kinds of steps keep their numbered lines, each with its detail. Batch keeps its numbered lines (the order matters) without the "Ich nehme gleich …" head.
   - Not taken: factoring a shared prefix out of the names ("„Klimagerät … Energie“:" above "• Arbeitszimmer EG"). KISS and Astra both advised against it: `sayText` already cuts names at 60 characters, so factored fragments of two cut names can look alike, and an unquoted fragment can read like an instruction. Plain quoted bullets already give one line per item.
3. **Why: short sentences.** Every typed reason (no "and N more" cap, as chat has), without "…, deshalb musst du das bestätigen" ("Das entriegelt eine Tür. Das wirkt auf 23 Geräte auf einmal."), then the copy note where it applies. "Noch wurde nichts ausgeführt" goes from the window and the form: the open window is that state. `raw.why.statistics_import` becomes "Das überschreibt schon gespeicherte Werte derselben Stunden" (phone and chat too). Batch: "Eine Bestätigung gilt für alle 4." and the order note, without the status line.
4. **Footer: one small line at the bottom** (11 pt, secondary colour on macOS): "HA NOVA · Angefordert von deinem KI-Assistenten · Dein Handy fragt auch; die erste Antwort zählt." (`window.footer` / `window.footer_both`, replacing `window.asked` / `window.both`). "Bestätige nur, wenn du das Folgende willst" goes; the title is a question with Cancel as the default.
5. **The confirm button names the action** on macOS and Linux, like the phone's (`actionGoal.button`: "Entriegeln", "Importieren" only where the verb has its own button, else "Bestätigen"). A raw plan of alike steps (imports, deletes) gets that button on the phone too, under the phone's unchanged question. Cancel stays first and the default; Confirm is still matched by position, never by label.

The plan's `Prompt` (window and form, sealed in the plan) carries title, what, and why as three blocks: `title\n\nwhat\n\nwhy`; the form shows the same. The window takes the first block as its title and adds the footer. A drift notice goes after the title, not before it. The chat `say` is unchanged.

Per OS, still only what it ships:
- **macOS:** argv = title, body, footer, wait, button, cancel (data only, read once into names). The footer is styled in the same text view by range. A title longer than one line is measured (an `NSTextField` at the same width) and leaves the text that much less room before it scrolls.
- **Linux:** zenity `--title=HA NOVA`, text = title, blank, body, blank, footer (`--no-markup` stays, so no bold), `--ok-label` = the action's button.
- **Windows:** caption "HA NOVA", the same plain text; OK and Cancel stay Windows' own. Renaming OK after the box laid out its buttons could clip a longer label, and that can't be checked without the VM: a known limit.

Budget: unchanged (64 × 32). `windowFits` counts title, blank, body, blank, footer at 64. Measured: statistics and 23 unlocks both fit.

Not changed: answer codes, the fallback, the chat `say`, the phone.

Reviews of the rework diff:
- Adversarial (Sonnet, fail-open + sibling sweep + UX): no P1; nothing that runs is dropped, argv stays data, the script still fails closed, the copy-note swaps still match, `windowFits` counts title, body, and footer. Fixed: "these N" without a kind ("Remove these 3?") now keeps the kind for framed names and raw deletes; the window's language is `textLang` (an empty plan language no longer reaches the Cancel label). Accepted: the phone button for alike raw steps (above).
- Local Codex CLI (gpt-6-luna): no finding.

Reviews of this design:
- KISS (Sonnet): drop the factoring, derive titles from `actionGoal`, keep Windows unchanged beyond the text, keep the batch change minimal. Adopted, except: the title keeps service data ("Diese 3 mit Helligkeit 50 einschalten?"), as it is one existing call (`sayWithData`), and the footer is small by range (the Captain asked for it small; checked on the real window).
- Astra (gpt-6-astra, medium): agree with changes. Adopted: every typed reason in the window (P1: the chat's cap at 8 must not apply), no factoring of cut names (P2), and the title's real height on macOS (P2). It confirms that dropping "Bestätige nur, wenn …" and "Noch wurde nichts ausgeführt" doesn't weaken consent: a question, Cancel as default, and an action-named button still ask.

## Verify

- Unit: the zenity arguments (`--width=600`, Cancel the default, no markup; runs on the Linux CI job). The window and form texts have blank lines between their blocks (single, raw, batch; DE and EN), and the chat `say` is unchanged. The budget ratchet. The 8-import and 23-unlock fixtures. The text that showed cut in the old window is still refused. The batch budget. The answer mapping. The script test above.
- Exact script, run through Go on Markus's Mac: under the sandbox it returns `windowFailed`; on the desktop with wait 0 it logs that it is showing and answers `timeout` (no answer).
- Real window: the Captain's harness (`HANOVA_CAPTAIN_WINDOW=statistics|long`, new binary in `~/.capy/work/ha-nova-captain/winbin-mar135/`) and `TestRealWindow`. Markus checks it visually; agents don't click.
