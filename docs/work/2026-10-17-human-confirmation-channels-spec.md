# Human confirmation channels, part 1: the confirmation window (2026-10-17)

Status: active

Inputs: `docs/internal/2026-10-17-human-confirmation-channels.md` (matrix, hands-on results), `docs/work/2026-10-17-confirmation-hardening-spec.md` (#716: plan secret, form precedence, fallback E), `docs/work/2026-10-16-mcp-human-confirmation-spec.md` (form C).

Goal: a typed-tier `apply` (unlock, open a gate or garage, disarm, delete, reload) asks the human through the simplest channel the model can't forge, chosen automatically. Threat model and actors T1–T3 as in the hardening spec §1: this change closes T1/T2 paths where a window can open; T3 stays stated, not solved.

## 1. Channels and order

A typed plan's channel is set when the plan is made and is part of its code's MAC (`confirm_code.go`), so editing it in the plan file breaks the plan.

| Order | Channel | When | The human |
|---|---|---|---|
| 1 | C `form` | the plan is made inside `ha-nova mcp` for a client that declares form elicitation (not `--no-forms`) | types the four-digit code shown in the form |
| 2 | A `window` | otherwise, when this computer can show a window (§2) and the user has not turned it off (§4) | clicks Confirm in a native window |
| 3 | B phone | not built; `detectChannel` (`cli/confirm_window.go`) is the slot, before chat | — |
| 4 | E `chat` | everything else | types the code shown in chat; `say` says the assistant sees it too |

- A window plan is never confirmed in a form (`formPlan` skips it): applied inside `ha-nova mcp`, it opens its window too.
- A form or window plan never takes a code from argv: `apply --confirm` is ignored for it (`CONFIRM_IN_WINDOW`, `CONFIRM_IN_FORM`; no attempt is used). Its code never leaves the process: not in the envelope, `say`, the plan file, or the apply result.
- MCP without forms (Claude Desktop, `--no-forms`, a client that declares no elicitation) now detects like the CLI, so leaving out the capability or adding `--no-forms` no longer yields a code the model sees where a window can open.

## 2. Detection (CLI only; no flag, no environment variable picks a channel)

| OS | Available when | Window |
|---|---|---|
| macOS | `/bin/launchctl managername` prints `Aqua` (a logged-in desktop session; SSH and background sessions are not) | `/usr/bin/osascript` `display dialog`, by absolute path, with an empty environment; buttons Cancel (default, so Return cancels) and Confirm; message and wait passed as arguments, never as script source |
| Windows | the process is not in session 0 (SSH, services) and its desktop is `Default` (Codex's sandbox uses a private desktop no one sees) | `MessageBoxTimeoutW` in process (no program path, no environment): OK/Cancel, Cancel the default, topmost |
| Linux | `DISPLAY` or `WAYLAND_DISPLAY` is set and `/usr/bin/zenity` exists | zenity by absolute path (never PATH: a fake could "confirm"), with an allowlisted environment (no `LD_PRELOAD`, `GTK_MODULES`); Cancel the default. kdialog is not used: its continue/cancel mode makes Continue the default, so a KDE desktop without zenity uses chat |

Linux reads two environment variables because a dialog cannot reach a display without them; unsetting them only moves a plan to E (§5, T3).

## 3. Apply with a window plan

1. Before the claim, apply re-plans the stored request (as apply does) and opens the window only when the fresh hash, tier, and id match the plan and at least 15 s remain before expiry (less: `PLAN_EXPIRED`, plan again). The window shows that fresh text (`confirmText`: the plan's code-free prompt, a delete's copy note, "Requested by your AI assistant. Go ahead only if you want this."), never the plan file's own `prompt`.
2. After a second in the open window, stderr says "A confirmation window opened on this computer — confirm there." (hosts that show a running command's output show it).
3. Answers:
   - **Confirm:** the CLI derives the plan's code in process (`humanAnswer`), and apply then claims, re-plans, and checks as always: a change while the window was open is drift (exit 2, nothing sent, a new plan).
   - **Cancel:** `CONFIRM_DECLINED`, exit 4, nothing sent, the plan stays.
   - **No answer** (timeout, closed, killed): `CONFIRM_CANCELLED`, exit 4, nothing sent, the plan stays; applying again opens the window again. Its `next` also names the way out when no window appeared: Home Assistant itself, or `ha-nova setup confirm-window off`.
   - **Could not open:** fallback E, never silent: apply claims the window plan (so one window plan yields at most one chat plan), saves a new chat plan (same hash, a fresh code) in its place, and returns exit 3, `say` starting "The confirmation window couldn't open on this computer." and ending with #716's note, and a `next_step` naming the new plan.
   "Could not open" is only a window that provably never showed: macOS — osascript could not start, the script never logged that it reached the window (it did not compile: a sandbox, -2740), or `display dialog` refused at once (-1713); Windows — `MessageBoxTimeoutW` returned 0; Linux — zenity could not start or reported that it could not open or connect to the display. A window that showed and then ended is no answer, never a fallback, so killing it can't force E.
4. Timeout: 90 s from the CLI (a late Confirm still re-plans, sends, and verifies under Claude Code's 120 s default command timeout; Claude Code then backgrounds the command, it does not kill it), 40 s inside `ha-nova mcp` (under the common 60 s tool-call timeout); never past the plan's expiry. `next_step` tells the agent to give the command up to 2 minutes. The CLI times itself out (Codex does not kill a unified-exec process on Esc).
5. The answer comes only from the window's own child pipe (macOS, Linux) or call (Windows): never stdin (Codex `write_stdin` can type into a process), argv, or a file.

**Code in the window? No.** A process that can click the window (macOS with Accessibility or input-posting permission, Windows UI Automation, X11) can also read a code shown in it and type it, so a typed code adds a step for the human and no protection; buttons with Cancel as the default stop a stray Return.

## 4. Turning windows off

`ha-nova setup confirm-window off` writes `"confirm_window": "off"` at the top level of `config.json`; typed plans then use E. Turning off is confirmed in the window itself, so a model that runs the command can't complete it; where no window can open, nothing is written (E applies anyway, and an "off" written there would persist into later desktop sessions). `on` (the default) removes the key and needs no confirmation. If a window opens where no one sees it, the human sets the key by hand (the command prints the path after a window that got no answer). Editing `config.json` is a T3 action, as is every other config edit.

## 5. Honest limits

- **Same-user clicks:** on Windows and X11 any same-user process can click the window (a plain `BM_CLICK` message, `xdotool`): there A stops T1 slips and documented-command T2 paths, not a model that automates the desktop. macOS needs Accessibility or input-posting permission for that; a terminal granted Accessibility (window managers) makes it clickable. Wayland blocks global input injection, but zenity is a child of the model's shell: tracing it (Yama scope 1 allows own descendants) or a GTK module named in the user's GTK settings file can still forge Confirm (T3).
- **Computer use:** with a host's computer-use feature on, the model can click the window on every OS.
- **Sandboxes:** inside Codex's seatbelt or Claude Code's sandbox the window fails to open (verified with an approximated Claude Code profile here); apply then falls back to E and says so. In practice `ha-nova` already runs outside Codex's sandbox because it needs the network.
- **Forced fallback (T3):** a model that wraps `ha-nova` in its own OS sandbox or a process limit (`ulimit -u`, so the window program can't start), unsets `DISPLAY`/`WAYLAND_DISPLAY`, runs it from an SSH session, or points Linux `DISPLAY` at a virtual display it controls gets E or a window it can click. Each is the same class as reading the Relay credential (hardening spec §1.1), which defeats every channel on this computer.
- **Form C is not human-only in a host that also gives the model a shell:** the model can start `ha-nova mcp` itself, declare forms, and answer its own form (the code is in the form's message). Pre-existing (hardening spec §4); preferring A over C where both are available would close it for desktops and is a product decision for a follow-up.
- **Unattended screen:** a `tmux`/`screen` session started on the Mac's desktop and reattached over SSH still reports `Aqua`, so the window opens on the Mac's screen, where whoever is there can click it; the remote user sees no window and gets no answer.
- **What the window shows:** a summary (names, the action, the reasons), not a config diff or the server name; with several homes the window does not say which one.
- **A window no one sees** (a virtual display, behind a full-screen app) times out every time, the off switch included; the way out is editing `config.json` by hand (§4) or Home Assistant itself.
- **MCP:** the server answers no `ping` while a window is open (up to 40 s); a client that gives up on the tool call earlier never sees a confirmed result.
- **Untested:** Windows and Linux windows (no machines here; code paths kept minimal), Claude Desktop and other MCP hosts showing the window, a locked screen or full-screen app hiding it (the timeout then says "no answer").
- **Language:** the window text is English, like the forms.

## 6. Tests

- `cli/confirm_window_test.go` (fake window): confirm, cancel, no answer, a code from argv never replaces the window, an edited plan-file prompt is not shown, drift during the window sends nothing, could-not-open falls back to a chat plan with `say` and `next_step`, form precedence in MCP, MCP `--no-forms` gets the window (no code anywhere), and the off switch.
- `cli/confirm_window_darwin_test.go`: osascript result classification; `HA_NOVA_REAL_WINDOW=timeout|failed` opens the real window (manual only).
- Real macOS 26 check (this change): the window opened with an empty environment and timed out after 2 s (`timeout`); under the approximated Claude Code sandbox profile detection said available and opening failed (`windowFailed` → E).
