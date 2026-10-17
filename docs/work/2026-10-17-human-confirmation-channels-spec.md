# Human confirmation channels: the confirmation window (part 1) and the phone (part 2) (2026-10-17)

Status: active

Inputs: `docs/internal/2026-10-17-human-confirmation-channels.md` (matrix, hands-on results), `docs/work/2026-10-17-confirmation-hardening-spec.md` (#716: plan secret, channel binding, fallback E), `docs/work/2026-10-16-mcp-human-confirmation-spec.md` (form C).

Goal: a typed-tier `apply` (unlock, open a gate or garage, disarm, delete, reload) asks the human through the simplest channel the model can't forge, chosen automatically. Threat model and actors T1–T3 as in the hardening spec §1: this change closes T1/T2 paths where a window can open; T3 stays stated, not solved.

## 1. Channels and order

A typed plan's channel is set when the plan is made and is part of its code's MAC (`confirm_code.go`), so editing it in the plan file breaks the plan.

| Order | Channel | When | The human |
|---|---|---|---|
| 1 | A `window` | this computer can show a window (§2) and the user has not turned it off (§4) — in the CLI and inside `ha-nova mcp` alike | clicks Confirm in a native window |
| 2 | B `phone` | no window can open (or windows are off), the Home Assistant owner has a phone with the Companion app, and the Relay is not the owner (§7) | taps Confirm in a notification on their phone (unlocked: Face ID, fingerprint, or passcode) |
| 3 | C `form` | otherwise, when the plan is made inside `ha-nova mcp` for a client that declares form elicitation (not `--no-forms`): remote, headless, and later phone connectors | types the four-digit code shown in the form |
| 4 | E `chat` | everything else | types the code shown in chat; `say` says the assistant sees it too |

- Why the window comes first (decision 2026-10-17, coordinator, logged in `docs/choices.md`): a model with a shell can start `ha-nova mcp` itself, declare forms, and answer its own form (the code is in the form's message); it cannot answer an OS window without computer use or accessibility rights. The approved order was C before A; it was flipped in this PR before merge.
- A window that can't open falls back to the phone where one is found (§7), else to the form in a forms session, else to chat.
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

`ha-nova setup confirm-window off` writes `"confirm_window": "off"` at the top level of `config.json`; typed plans then use the form where the MCP client shows one, else E. Turning off is confirmed in the window itself, so a model that runs the command can't complete it; where no window can open, nothing is written (E applies anyway, and an "off" written there would persist into later desktop sessions). `on` (the default) removes the key and needs no confirmation. If a window opens where no one sees it, the human sets the key by hand (the command prints the path after a window that got no answer). Editing `config.json` is a T3 action, as is every other config edit.

## 5. Honest limits

- **Same-user clicks:** on Windows and X11 any same-user process can click the window (a plain `BM_CLICK` message, `xdotool`): there A stops T1 slips and documented-command T2 paths, not a model that automates the desktop. macOS needs Accessibility or input-posting permission for that; a terminal granted Accessibility (window managers) makes it clickable. Wayland blocks global input injection, but zenity is a child of the model's shell: tracing it (Yama scope 1 allows own descendants) or a GTK module named in the user's GTK settings file can still forge Confirm (T3).
- **Computer use:** with a host's computer-use feature on, the model can click the window on every OS.
- **Sandboxes:** inside Codex's seatbelt or Claude Code's sandbox the window fails to open (verified with an approximated Claude Code profile here); apply then falls back to E and says so. In practice `ha-nova` already runs outside Codex's sandbox because it needs the network.
- **Forced fallback (T3):** a model that wraps `ha-nova` in its own OS sandbox or a process limit (`ulimit -u`, so the window program can't start), unsets `DISPLAY`/`WAYLAND_DISPLAY`, runs it from an SSH session, or points Linux `DISPLAY` at a virtual display it controls gets E or a window it can click. Each is the same class as reading the Relay credential (hardening spec §1.1), which defeats every channel on this computer.
- **Form C is not human-only in a host that also gives the model a shell:** the model can start `ha-nova mcp` itself, declare forms, and answer its own form (the code is in the form's message). With the window first this is closed where a window can open; it stays where none can (headless or SSH hosts with a shell, a sandboxed command, the windows turned off).
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

## 7. Part 2: the phone (B)

Inputs: the channels report §2 B and §3 B (sniff and forge paths reproduced on a throwaway HA).

### 7.1 Order: window → phone → form → chat (decision 2026-10-17, logged in `docs/choices.md`)

The phone comes before the form: a model with a shell can start `ha-nova mcp` itself, declare forms, and answer its own form (the code is in the form's message); it can't tap a phone. It comes after the window because the window needs no phone, no push round trip, and no Companion app, and is equally out of a same-user model's reach on macOS. A window that can't open moves the plan to the phone where one is found (`windowFallback`), else to the form or chat as before.

### 7.2 Who confirms, and where

- **The Home Assistant owner.** In the App only the owner can arm a pairing (`nova/src/security/owner-check.ts`), so "the user this computer is paired for" is the owner; the Relay records no other user. The owner is one fixed account that the Relay's user can't become, and that a model can't add (a new HA user never counts).
- **Their phones:** the owner's `person` → its device trackers from `mobile_app` → each device's legacy `notify.mobile_app_<slug>` service. The slug comes from Home Assistant itself (one `POST /api/template` with a fixed template and the owner id as a template variable, never as source), so names like "Markus’s iPhone" match exactly; a target counts only when that notify service exists. Reads: `auth/current_user`, `config/auth/list`, the template, `/api/services` — a fixed allowlist (`checkPhoneRead`), not the model-facing read allowlist.
- **Available only when** an owner exists, the Relay's own user (`auth/current_user`) is not the owner, and at least one target exists. App mode: the Relay is the Supervisor user → available. A standalone Relay on the owner's long-lived token → unavailable (a forged tap would carry the owner's id); on a separate HA user's token → available.
- **Several phones:** all of them get it; the first decisive answer wins; then every phone that got it is sent `clear_notification` with the same `tag` (also after Cancel and no answer), in parallel and for at most 3 s, so cleanup never delays apply.

### 7.3 The notification

`notify.mobile_app_<slug>` with title "HA NOVA: hold or expand for Confirm" (a plain tap only opens the app), the plan's code-free text (`confirmText`, cut at 600 characters) plus "Requested by your AI assistant. Confirm only if you want this.", and `data`: `tag: ha-nova-<plan id>-<8 random hex>` (fresh per attempt, so a late clear of an earlier attempt can't remove it), actions Confirm (`destructive`, `authenticationRequired`: the phone must be unlocked on iOS and Android 12+; older Android confirms from the lock screen) and Cancel, iOS `push.interruption-level: time-sensitive` and `apns_headers.apns-expiration` (the wait's end), Android `priority: high`, `channel: HA NOVA`, `importance: high`, `timeout` (seconds left). No `ttl: 0` (the push relay drops it).

The action ids are `HANOVA_OK_<32 hex>` / `HANOVA_NO_<32 hex>`, drawn per apply from `crypto/rand`, held only in the apply process: never printed, logged, stored in the plan file, or in the envelope.

### 7.4 Waiting and the identity check

- The CLI listens through the Relay's existing bounded `collect_events` (`subscribe_events` `mobile_app_notification_action`, `max_events: 1`, 10 s cap) in two overlapping lanes 5 s apart, so one re-subscribes while the other listens. No Relay change (no App release, works with installed Relays). First a 1 ms probe window must succeed (the Relay starts a window only after Home Assistant acknowledged the subscription), so a Relay that can't subscribe gets no notification at all; the push then goes out 1 s after the first lane starts. The phones are sent to in parallel, bounded by the wait, so a stalled notify service never holds apply past the deadline.
- An event decides only if `data.action` is one of our two ids. It then counts only if `origin` is `REMOTE`, `context.user_id` is the owner, and that id is not the Relay's own user; otherwise the wait ends as `CONFIRM_REJECTED` (exit 4, nothing sent; worded as "a device that isn't signed in to Home Assistant as the owner", since a family device on the owner's person gets the buttons too). Ending, not ignoring: a matching id from someone else means the id leaked, and the human can simply apply again for a fresh one. Other actions (other notifications' buttons) are ignored.
- Timeouts as the window: 90 s in the CLI, 40 s in `ha-nova mcp`, counted from before discovery, never past the plan's expiry, at least 15 s left or `PLAN_EXPIRED`.
- Outcomes: Confirm → the code is derived in process (`humanAnswer`) and apply claims, re-plans, and checks as always; Cancel → `CONFIRM_DECLINED`; no answer → `CONFIRM_CANCELLED` (apply again sends a new notification); could not listen or send → `CONFIRM_UNAVAILABLE`. All keep the plan and send nothing. A phone plan never falls back to another channel. A code from argv never replaces the phone (`CONFIRM_ON_PHONE` when apply is reached without the answer).

### 7.5 No downgrade: the phone pin

Detection is automatic, so a model could break it in Home Assistant (remove the phone from the person, disable the Companion app's entry) to move later plans to the form or chat. Therefore the first time a phone is found, `config.json` gets `"confirm_phone": {"<owner user id>": "on"}` — keyed by the owner's user id, unique per Home Assistant, so removing or renaming a server profile can't carry it to another home (if that write fails, the plan is refused — an unpinned phone plan would lose this protection — unless a concurrent planner already wrote "on"); from then on a typed plan whose phone can't be found (or whose Relay now acts as the owner) is refused (`CONFIRM_UNAVAILABLE`, exit 4), never moved. When the owner can't be read but a pin is "on", or `config.json` can't be read at that moment, the plan is refused as well. A typed plan's apply re-plans on its stored (sealed) channel, so a phone lookup that fails after the tap never refuses a confirmed plan. `ha-nova setup confirm-phone off` is confirmed on the phone itself and writes `"off"`; where no phone can be reached it changes nothing and names the key to edit by hand. `on` removes `"off"`. Editing `config.json` is T3, as for the window.

### 7.6 Forgery paths closed (raw guard, `cli/relay_raw_guard.go`)

Already refused before this change: `fire_event`, `POST /api/events/*`, `/api/webhook/*` (any method), WS `webhook/*` (`webhook/list`, `webhook/handle`), `subscribe_events` without a type, `*`, or `call_service` (the notify call carries the ids). Added: `subscribe_events` and `subscribe_trigger` on `mobile_app_notification_*` and iOS `ios.notification_action_fired` (Android echoes every action id and its webhook id in them), and any raw `subscribe_trigger` holding a template (an event trigger's type can be templated). WS `cloud/status` (lists cloudhook webhook ids) and `mobile_app/*` (the local push channel) are not on #718's raw WS allowlist, so they stay refused (pinned by tests). The CLI's own discovery reads and its listener never go through the raw routes: they use their own fixed allowlist (`checkPhoneRead`) and a fixed subscription body, so nothing here widens what a model can send raw. Phone plans carry #718's plan seal like every typed plan; an edited phone plan is never asked on the phone.

### 7.7 Honest limits

- **T3 as before:** a model that reads the Relay credential or edits `config.json` (pin off, window off) is not stopped.
- **Leaked ids are not enough on their own:** a forged tap also needs the owner's identity. Paths that carry it: an Android (unencrypted) registration's webhook id plus `POST /api/webhook/<id>` from outside the Relay. The raw guard closes the known ways to learn it through the Relay; a model that builds its own reader (for example a SQL sensor over the recorder's event table through a config flow) or reads HA's debug log after raising its level (`logger.set_level` is preview) could learn ids — T3-adjacent, stated. iOS registrations are encrypted: `webhook/handle` and a plain webhook post are refused there.
- **Notification overlay (closed):** an automation that watches `call_service` or a phone notification event other than the tap could copy our buttons into a notification with its own text; the owner's genuine tap would then confirm text they never read. Writing such an automation or script (event trigger or `wait_for_trigger` on `call_service`, `mobile_app_notification_received|cleared`, or a templated event type; blueprints after substitution) is therefore typed (`CONFIRMATION_WATCH`). Listeners on the tap itself stay ordinary: they see the ids only once they are dead. Existing automations made before this change are not re-checked.
- **Late delivery:** a send still in flight when the wait ends may reach a phone after the clear; its buttons are dead (the ids lived only in that wait).
- **The owner's own context:** a script the model wrote and the owner then runs from a dashboard fires with the owner's user; with a leaked id it could confirm. Narrow; stated.
- **Listening gap:** an unrelated notification action ends both listening windows at once; a tap in the milliseconds before they re-subscribe is lost (no answer, nothing sent).
- **Delivery:** push quota and push-relay errors are silent in Home Assistant (the call still succeeds): the wait then ends as no answer. A phone with no route to HA (no cloud, remote URL, or LAN) loses the tap. A swipe-away is no answer (Android's cleared event is not read).
- **Who:** only the owner confirms; a household member who is not the owner can't confirm on their phone, and a phone added to the owner's person but registered by another HA user is rejected.
- **The pin** is set on the first typed plan that finds a phone; before that, a broken phone setup is not noticed. A Relay that later runs on the owner's token refuses typed plans until the pin is turned off. A lost phone that is still registered keeps getting the notification, and turning the pin off asks that phone: the way out is removing the old device in Home Assistant and signing in to the Companion app on the new phone, or editing `config.json`. The pin only matters where no window opens (headless, SSH); a desktop that always gets the window never sets it, so a phone setup broken there before the first headless plan is not noticed either.
- **Lock screen:** the plan's summary (names, the action) shows in the notification preview.
- **MCP:** the server answers no `ping` during the up to 40 s wait, as with the window.
- **Untested:** the real Companion apps (simulated registrations only; the push payload is per the apps' sources), App mode itself (a separate admin HA user stood in for the Supervisor user), and remote connectors.
- **Language:** the notification text is English, like the window.

### 7.8 Tests

- `cli/confirm_phone_test.go` (fake phones): event judging (wrong user, Relay user, `LOCAL` origin, no user, wrong id, other events); confirm on one of two phones sends once and clears both; the ids never appear in output, stderr, the plan file, or `config.json`; wrong user, Relay user, local fire, wrong id, cancel, and no answer send nothing and keep the plan; no phone → chat, pinned and missing → refused at plan and apply; off → chat; discovery needs an owner other than the Relay; a window that can't open falls back to the phone.
- `cli/relay_raw_guard_test.go`: the added refusals.
- Throwaway HA `2026.10.1` (project `ha-nova-phonepush`, 127.0.0.1:28323/28391, not the test bed): owner with two simulated Android registrations, a second user with one, the Relay on a separate admin user's token, the CLI built from this branch with windows off. The plan picked `phone` and pinned it; both owner phones got the push (with "Owner’s Pixel" slugged by HA), the other user's phone none. A real `mobile_app_notification_action` from the other user's phone webhook → `CONFIRM_REJECTED`, lock stayed locked; the Relay user's own `POST /api/events` with the right id → `CONFIRM_REJECTED`; the owner's Cancel → `CONFIRM_DECLINED`; the owner's Confirm → `verified`, unlocked, and both owner phones got `clear_notification`. A raw `subscribe_events` on the event was refused. With the Relay restarted on the owner's token: the pinned home refused (`CONFIRM_UNAVAILABLE`), a fresh home got `chat`. Containers, network, and image removed afterwards.
