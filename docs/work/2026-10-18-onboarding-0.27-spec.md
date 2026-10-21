# 0.27: setup in the browser, HA NOVA in chat apps

Status: active

Direction and prototype approved by the maintainer (2026-10-18). Implementation runs as the PR train in §13 (A1 #740, A2 #739, A3 #741 merged).
**UX baseline:** the approved clickable prototype `docs/work/2026-10-18-onboarding-0.27-prototype.html`. Its screens and copy are the reference; §16 lists where this spec still deviates.
Inputs:
- `cli/setup_*.go`, `cli/ha_signin_session.go`, `cli/setup_signin_pairing.go`, `cli/cloud_setup_remote.go`, `cli/cloud_owner_pairing.go`, `cli/confirm_phone*.go`;
- `nova/src/http/handlers/pair-v2.ts`, `nova/src/security/pairing-v1.ts`, `nova/config.yaml`;
- `docs/work/2026-10-16-mcp-skill-bridge-spec.md`, PR #738;
- `../ha-nova-site/DESIGN.md`;
- the rc2/rc3 real-device lanes of 2026-10-18.

Maintainer decisions (2026-10-18):
1. The page follows the hanova.app look (§4.1).
2. The Claude Code auto-mode rule is preselected.
3. English only at first, but i18n-ready from day one (§4.2).
4. The window and the phone ask in parallel, and the first answer wins (§7.4).
5. The wow page combines insights and a tailored automation, each previewed and saved only on click (§7.5), as in the prototype. There is no light blink.
6. The chat-app bridge over Home Assistant Link ships in 0.27 (§11).

## 1. Problem

What a first computer on HA OS goes through today:
- about 6 terminal prompts;
- 3 manual clicks in Home Assistant (Add repository, Install, Start);
- a sign-in tab, then back to the terminal;
- the phone and Claude Code rule prompts;
- a text first look.

Every hop between terminal and browser is a point where a non-technical user stalls. The rc3 run named:
- mixed numbered and arrow prompts;
- cluttered text: the intro alone has six bullets (`cli/setup_intro.go`);
- an owner code on Link even when the OAuth user is the owner;
- a Windows firewall prompt with no context.

There is also no way to use HA NOVA from a chat app or a phone without a computer running a coding agent.

What stays unchanged: HA sign-in pairing (OPAQUE over owner-armed ingress), the confirmation window and phone, the first-look read, the prebuilt App images, the typed verbs and tier engine, and the skills.

## 2. Goals and non-goals

Goals:
1. **One terminal step.** The installer line is the only thing typed. After it, the terminal shows one progress line and asks nothing on a desktop.
2. **Four browser steps, one tab:**
   - found → sign in and install → "You stay in control" → your home;
   - the HA sign-in redirect returns into the same tab.
3. **Detect, don't ask.** The page asks only when detection is ambiguous or when consent is required (§5).
4. **No manual App install.** After the owner signs in, the Supervisor API adds the repository and installs, starts, or updates the App. Consent is on the button.
5. **A finish worth showing.** The home at a glance, insights, and an automation made for this home: previewed, saved only on click. Nothing changes without a click.
6. **Simpler than any MCP-server setup** a non-technical HA user would face:
   - no JSON or YAML editing;
   - no tokens to copy;
   - no Docker, Python, or `uvx`;
   - no HA restart on the computer path.
7. **Works from the phone and in chat apps:**
   - Claude (web, desktop, mobile) and ChatGPT reach the home through Home Assistant Link;
   - they get the same skills, typed verbs, and human confirmation;
   - no computer is involved (§11).
8. **Plain-language skills** (§10).
9. **Uninstall is one command and one answer** (§9).

Non-goals for 0.27:
- No new pairing crypto, no new tiers, no new confirmation channel. The safety-relevant changes are two: window and phone ask in parallel (§7.4), and the bridge's channel policy (§11.4).
- Container/Core keeps the token guide; the page detects it and links there.
- No native GUI, no code signing, no hosted setup page.
- No terminal-free entry for *desktop* clients (Claude Desktop extension). Chat-app users get a terminal-free path through §11.
- Assist/voice is not part of 0.27: the bridge registers no LLM API (§11.7).
- Read-first Matter/Thread/Z-Wave skills move to 0.28: the bridge took that slot.
- Recovery edge states (Cloud checkpoints, credential retirement, secure-storage recovery) stay in the text wizard. The page shows "This needs one step in the terminal" and hands over.

## 3. Architecture of the setup page

| Option | Verdict |
|---|---|
| **A. Local page served by the CLI on `127.0.0.1:<random>`** | **Chosen.** The CLI already runs a loopback server for the OAuth callback (`cli/cloud_oauth_loopback.go`); the same server serves the page, so the HA redirect lands in the same tab. Only the computer can detect clients, write client config, open the confirmation window, and store the device credential. |
| B. NOVA ingress page in HA | Rejected for first run: no App means no page, and HA can't configure the computer. It stays the owner console: devices, phone, "Connect a device", chat apps (§11), plus a "Set up another computer" row with the one-liner. |
| C. Hosted page driving the CLI via `fetch` | Rejected: Chrome's local-network-access prompt, mixed content, our domain in the trust chain. |
| D. Native GUI | Rejected: signing and packaging cost; HA sign-in needs the browser anyway. |
| E. TUI only | Not enough as the main path; kept as `--no-browser` with arrow menus (§9). |

The CLI stays as the runtime: skills call `ha-nova` for plan, apply, the window, and the keychain. It stops being the interface. An agent that runs `ha-nova setup` for a repair now opens the page, so no skill ever has to tell the user to run a command (§10).

**Engine/renderer split.** `interactiveSetup` (`cli/setup_interactive.go`, 1318 LOC) mixes flow, prompts, and rendering. 0.27 extracts a `setupFlow` state machine:
- steps: `detect` → `signIn` (+ `installOrUpdateApp`) → `pair` → `configureClients` → `control` (window, phone, Claude rule, Link) → `home` (insights, suggestions) → `finish`;
- two renderers: web (JSON state plus POST actions) and text;
- pairing, Cloud, keychain, and client-install internals are called unchanged.

## 4. Screens (desktop: macOS, Windows, Linux with a display)

The copy follows the prototype. `[ ]` marks a button; "Step n of 4" shows as step dots.

**T: terminal** (the only step before the page; the installer already auto-starts setup):
```
HA NOVA 0.27 installed · Opening setup in your browser…
  If the page doesn't show, open this address in your browser: http://127.0.0.1:53817/s/<token>
```
The line updates in place and ends with `✓ Done: this computer is connected to Home Assistant.` (exit 0). Ctrl+C stops setup, and the page shows "Setup was stopped in the terminal."

**Step 1: "We found your home."** (eyebrow "Welcome")
- Lead: "HA NOVA looked around. Here's what it found. No questions so far."
- Rows:
  - ✓ Home Assistant "homeassistant.local · version 2026.10" [Change];
  - ✓ Your AI assistant "Claude Code · also found Codex CLI", tagged "Both set up";
  - · NOVA App "Not installed in Home Assistant yet", tagged "Next step". When the App runs: ✓ "Running · version 0.11". When it is old: "Needs an update".
- Buttons: [Sign in and install NOVA] (or [Sign in with Home Assistant] / [Sign in and update NOVA]) and ghost [Use a six-digit code].
- Fine print: "You sign in on your own Home Assistant page as the owner. HA NOVA then installs only its own App." Plus one line: "Home Assistant will say 'You're about to give http://127.0.0.1:… access'. That is this page."
- Sub-states:
  - nothing found or several found: the HA row becomes "Where is your Home Assistant?" with a field (or a list) and [Connect]. A link reads "No Home Assistant yet? Look around the demo house" (§8);
  - code path: "In Home Assistant, open NOVA in the sidebar and click Connect a device." Six boxes, [Connect].

**Step 2: "Setting up NOVA in your Home Assistant."** (eyebrow "Connecting")
- Lead: "This takes about a minute. You can watch, or get a coffee."
- Live rows:
  1. Signed in as Markus (owner);
  2. Adding the NOVA App;
  3. Starting the NOVA App;
  4. Connecting this computer;
  5. Teaching Claude Code and Codex CLI.
- When every row is ✓, the page moves on to step 3 by itself (§16 D9). Soft failures stay on this step with one line and one button:
  - not owner: "Signed in as Anna, who isn't the owner. Only the owner can connect a computer." [Sign in as the owner] · "Use a code";
  - not admin: "Anna can't install Apps. Ask the owner to sign in here." [Sign in as the owner];
  - Container/Core: "This Home Assistant has no Apps. Follow the container guide." [Open the guide];
  - no AI client: "No AI app found on this computer." [Get Claude Desktop] · [Get Claude Code] · [Check again];
  - install failed: "Home Assistant couldn't install the NOVA App: <one plain line from the Supervisor job>." [Try again] · "Open the App page" (today's my-link).

**Step 3: "You stay in control."** (eyebrow "Your safety net")
- Lead: "Unlocks and deletes always wait for you. Your AI assistant never sees a code."
- Rows:
  - "Confirm on this computer": "A small window asks before anything risky runs." Tag: On.
  - "Confirm on your phone": "iPhone Markus, and its Apple Watch. Both ask; the first answer counts." Switch, off by default. Switching it on sends one link notification ("Tap 'Link' there."). Several eligible phones: the row first lists them to pick one, as `confirm-phone` does today (§16 D10). None: "No phone with the Home Assistant app found", and no switch.
  - "Let Claude Code show the window": "Adds one rule to Claude Code so its auto mode doesn't block the confirmation window." Switch, **preselected**; written on [Finish setup] with #738's writer; uninstall removes it.
  - *Addition* (§16 D3), only when HA Link remote access is active: "Use HA NOVA in Claude and ChatGPT, on any phone." Switch, off by default; turning it on runs §11.6.
- Button: [Finish setup].
- Fine print: "Turning on your phone sends one notification to that phone. Tap 'Link' there."

**Step 4: "Here's your home, at a glance."** (eyebrow "All set")
- Stats row: rooms · devices · lights · locks · automations.
- **Worth a look**: up to 3 insights, each with one action that previews first. Prototype examples:
  - "Outside Temperature battery is at 12%" [Remind me];
  - "60 devices aren't in a room" [Suggest rooms]. The preview shows the room ← device list and "Nothing is saved until you choose 'Save rooms'."
- **Made for your home**: one tailored automation ("Light on when the front door is unlocked after dark", "Uses Front Door and Bed Light. Preview only."). [Preview] shows "When / Only / Do", then [Save automation] → "Saved ✓".
- **Now ask your assistant**: 3 copyable phrases ("What's on right now?", "Turn off everything in the kitchen", "Unlock the front door"). A tap copies: "Copied. Paste it into Claude Code."
- Primary: [Open Claude Code] (§16 D5).
- Fine print: "Tap a phrase to copy it." The prototype's "Nothing in your home changed during setup." becomes "Nothing in your home changed unless you saved it." (§16 D6).

**Steps 3–4 as built (A6b):**
- Step 3's phone row links the first phone with one notification: the click on the page is the consent at this computer (the window's part in `ha-nova setup confirm-phone`), the tap on that phone links it. A phone linked before shows as on; a link that can't be asked now stays with `confirm-phone`.
- Step 3 runs once setup is complete (as the terminal's offers do), so its writes never race setup's own config checks. Ctrl+C or idle there shows "connected, choices not applied".
- The Link row is the Home Assistant Link fallback (D2), shown only while Link remote access answers; its sign-in opens in a tab of its own. A Link failure keeps the terminal's exit code, but step 4 still shows with one line. The chat-app row (B4) is in the contract doc, "Setup and CLI (B4)".
- Step 4 runs the §7.6 verbs as child processes for the server setup connected (`--server`), in their own signal group (Ctrl+C never stops a save halfway). A save posts the plan id the page shows; the CLI saves only a plan this run stored for that preview, once. An unclear outcome is never offered again; a read-back mismatch shows no ✓. Rooms undo is a preview of its own, then a click, and stays offered until an undo is verified. Done, Ctrl+C, or 30 min idle closes setup.
- No Home Assistant found: step 1 offers "Look around the demo house" (`ha-nova try`; `setup --demo` is the alias).

**Re-run on a working install** ("HA NOVA is connected"):
- ✓ rows from doctor's checks;
- [Done] · "Connect a different Home Assistant" · "Fix something" (only when a check fails).

**Re-run as built (G1):** a plain `ha-nova setup` on a desktop with a saved address opens the page too (`cli/setup_page_rerun.go`).
- Working: one screen "HA NOVA is connected." with rows Home Assistant · NOVA App · This computer · Your AI assistant, then step 3's rows (window, phone, Claude Code rule preselected as in the terminal, Link offer, or On while it is the automatic fallback, chat apps as a switch; while on it shows Add to Claude, the address, and "Show me how"). A re-run has no owner sign-in, so a switch that needs one says so under it when flipped (Link: "You sign in to Home Assistant when you click Done.", also on step 3 with "Finish setup"; chat apps: what Done does). Done turns chat apps off as `ha-nova chat-apps off` does; turning them on signs the owner in first, then the row asks the step-3 consent, and a second Done applies the other switches and ends setup. Otherwise [Done] applies the switches and ends setup. "Add another Home Assistant server" continues in the terminal.
- AI assistants: the ones set up before are judged; when only their skills are out of date (after an update) they are refreshed without a question. One found since gets a "Set up … too" button, never set up without that click. None of them here (none set up yet, or all removed): every one found is set up, as on a first run.
- Anything else: "Something needs a fix." with [Fix it], which runs the usual steps on the page; a computer removed on the NOVA page lands on step 1 to sign in again. While the NOVA App doesn't answer: [Check again], the App page, and [Fix it] (step 1: another address, or the sign-in that installs or starts the App).
- The shared-token install (also a former service token file), first: "One quick change first." It says 0.27 is the last release that accepts the shared access; [Switch to your own access] runs the usual sign-in pairing on the page, for the AI assistants set up before; [Not now] keeps the terminal. Only while its App offers the sign-in; else the terminal asks the same (sign-in or the six-digit code).
- Keep the terminal: a custom Relay address, Link that isn't ready or Link only, flags, `--service`, named profiles, SSH, `--no-browser`. Not built: "Connect a different Home Assistant".

### 4.1 Look and feel: the hanova.app system

The page follows `../ha-nova-site/DESIGN.md` ("The Legible Cosmos"), so setup feels like the site the user came from. The prototype already does.
- **Tokens:**
  - copied verbatim from the site's `styles.css :root`: `--bg-0/1/2`, `--ink`, `--ink-soft`, `--cyan`, `--amber`, `--hairline`, `--panel`, `--ease-rise`;
  - kept in one embedded `tokens.css` whose header names the site commit;
  - the release site-sync step checks drift.
- **Theme:** the dark night gradient only, like the site. Star dots stay sparse and static under reduced motion.
- **Two Lights Rule:**
  - cyan = this computer and the user's actions: primary buttons, client rows, copy, the "Both set up" tag;
  - amber = Home Assistant, the App, and safety: all of step 3, the guard icons, the App install;
  - never swapped.
- **Type:** system stack only (No-Webfont Rule). Headline for screen titles, Title for row keys, Body and Lead for copy, Label for the uppercase eyebrows.
- **Structure:** hairline-topped rows, not cards (One Hairline and No-Shadow Rules). The step 2 progress rows may use the Safety Timeline nodes.
- **Components:**
  - pill buttons: primary is cyan with Abyss Ink text; ghost has a hairline; the focus outline is 2px cyan;
  - pill switches and tags;
  - chips for the phrases;
  - a code bar for the terminal line;
  - the preview block is a recessed `--panel` surface, like the site's code bars.
- **Imagery:**
  - the star only as the vendored `assets/logo.svg`;
  - on step 4, the site's pairing neon scene draws the cyan→amber link once as the "connected" moment.
- **Motion:** `--ease-rise` staggered rise per screen, all off under `prefers-reduced-motion`.
- **CSP:** the site's inline `html.js` script moves into the external script (§6). The page needs JS; `<noscript>` names `ha-nova setup --no-browser`.
- **Layout:** one column, max 760px, 1.5rem gutters, no horizontal scroll, full-width buttons under 720px.

### 4.2 Strings: i18n-ready, English only

- Every user-visible string is a key in `setupweb/strings.en.json`:
  - named placeholders (`{name}`, `{count}`);
  - CLDR plural forms;
  - no concatenated sentences: word order lives in the catalog.
- The server sends data only (counts, numbers, timestamps, HA names). The page formats with `Intl.NumberFormat`/`DateTimeFormat`/`PluralRules` in the browser locale.
- The text path uses the same catalog.
- Tests:
  - every key used exists, and placeholders match;
  - the renderers contain no user-facing literals;
  - a pseudo-locale render (expanded, accented) does not overflow.
- A new language is one file.

### 4.3 Platform specifics

| Platform | Difference |
|---|---|
| macOS | `open`; login keychain as today. Test lane: separate macOS user or VM (hard rule). |
| Windows | Browser launch as today. The page server binds `127.0.0.1`, which does not trigger the firewall. Discovery uses the system resolver for `homeassistant.local` (Windows resolves `.local` via mDNS itself) plus saved and probed addresses, and **skips the dnssd browse**, so the "publisher unknown" firewall prompt (D4) never appears. |
| Linux desktop | `xdg-open`; Secret Service. On WSL, `wslview` opens the browser and WSL2 forwards loopback (test lane). |
| Linux headless / SSH | text path automatically (§9) |

## 5. Detected vs asked

| Fact | Detected by | Asked only when |
|---|---|---|
| HA address | saved config; system resolver/mDNS; candidate probes (`cli/setup_discovery.go`) | none or several found |
| App present, version, sign-in capable | `GET /pair/v1/info`; after sign-in, Supervisor `/addons/<slug>/info` | never |
| OS/Supervised vs Container/Core | after sign-in: Supervisor reachable over WS `supervisor/api` | never |
| Owner / admin / user | `auth/current_user` | never; the role picks step 2's next action |
| AI clients | client registry runtime detection | none found → download links. All found are configured (`--client` stays for scripts) |
| Local vs Link | local address reachable → local; a Link address not reachable locally → Link only | step 3 Link and chat-app rows, only when Link remote is active |
| Desktop vs headless | TTY, `SSH_*`, `DISPLAY`/`WAYLAND_DISPLAY`, Windows session 0 | never; `--no-browser` overrides |
| Phone and Watch | admins' Companion apps (`discoverPhone`) | several eligible phones (pick list); the link is a switch |
| Formatting | browser locale | never; text is English in 0.27 |

## 6. Security model of the local page

Threats: other sites in the browser (CSRF), DNS rebinding, other OS users, clickjacking, hostile HA names (XSS), leaked launch URLs, a page outliving setup.

| Control | Rule |
|---|---|
| Bind | `tcp4 127.0.0.1:0` only; never `0.0.0.0` or `localhost` |
| Host check | `Host` must equal `127.0.0.1:<port>` exactly, else 421 |
| Launch token | 256-bit, `/s/<token>`, **single use**: the first GET sets the session cookie and moves on to `/`. Reuse shows "This setup page is already open in another tab." On Linux, where other users can read command lines, the browser opens a private 0600 launcher file instead of the URL |
| Session cookie | `HttpOnly`, `SameSite=Strict`, host-only, `Path=/`. HA's cross-site redirect to `/oauth/callback` carries no cookie and needs none: the OAuth `state` checks it, and it answers with a page that moves on to `/` itself, so that hop is same-site and carries the cookie |
| Mutations | `POST /api/action` only; JSON content type; `Origin` exactly `http://127.0.0.1:<port>`; `Sec-Fetch-Site` absent or `same-origin`; session cookie; allowlisted action names with typed arguments |
| OAuth callback | existing `validateOAuthCallback` (exact state, single use); HA tokens never reach the page |
| Headers | CSP `default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'`; `X-Frame-Options: DENY`; `no-referrer`; `no-store`; `nosniff`; embedded assets only, no inline script |
| HA-sourced text | `textContent` only; control and format characters stripped (`terminalSafeName`) |
| Secrets | none on the page; a typed six-digit code goes to the CLI once and is never echoed |
| Writes from the page (step 4) | only by plan ID: the page posts `save {plan_id}`, never a body. The CLI built the plan, showed exactly that preview, and applies it through the normal `apply` path (§7.5) |
| Lifetime | exits on finish, Ctrl+C, or 30 min idle |
| Logs | never logs the token, cookie, OAuth code, or state |

Sign-in session scope:
- HA tokens stay in the CLI process, and the refresh token is revoked at the end, as today.
- The flow now spans the App install, so budgets are per phase: install ≤ 10 min, arming ≤ 2 min and the rest of the pairing exchange within the Relay's armed slot (2 min from its answer), the sign-in steps after the browser returns ≤ 2 min each (a busy Home Assistant answers each request in 15–30 s). A timeout after Home Assistant answered says it answered too slowly and keeps the address. A pairing that failed before its finish request keeps the owner's ingress session in memory for 10 minutes after that failure, and step 2's [Try again] reuses it while the App offers sign-in (Try again reads the App again first; while it doesn't, step 2 stays as it is). After a finish request (it may have left a pending device, which expires after 15 minutes), a not-owner answer, or a session Home Assistant ended, [Try again] asks for the owner's sign-in again.
- A typed client with an allowlist, never a passthrough:
  - `auth/current_user`;
  - `supervisor/api`:
    - `GET /addons/<slug>/info`, `GET /store/addons/<slug>`;
    - `GET|POST /store/repositories`, `POST /store/reload`;
    - `POST /store/addons/<slug>/install|update` (background);
    - `POST /addons/<slug>/start`;
    - `GET /jobs/<id>`;
    - `POST /ingress/session`;
  - through that ingress session, only the App's owner actions: arm, chat apps on and off, and the owner's chat-app phone link (§11.4, §11.6);
  - `cloud/status`;
  - the phone lookup reads.
- Repository trust:
  - the repository URL is a compiled constant;
  - HA NOVA continues only when the normalized `repositories[].source` equals it and the slug resolves from that repository;
  - otherwise: "Home Assistant lists a different NOVA source. Nothing was installed."

Known limits:
- Same-user malware is out of scope.
- Another OS user could race the launch URL from process arguments; single use bounds it.
- An AI with computer use can click page buttons, as with the window. The owner's HA password stays the consent for install and pairing.

## 7. Feature decisions

**7.1 App install via Supervisor.**
- This is the HA frontend's own path: an admin's WS `supervisor/api`, with `background: true` and `/jobs` polling.
- Sequence: add repo (skip if present) → install → start → poll `/pair/v1/info` until `sign_in: true` (≤ 2 min) → ingress session → arm → pair.
- Auto-update stays at HA's default (§16 Q3).
- An old App updates with the same consent wording.
- The text path gets the same automation whenever a local browser sign-in is possible.

**7.2 Link owner pairing without a code.**
- When the Link OAuth user `is_owner` (HA's `auth/current_user` over Link), the CLI arms through the Link ingress session and pairs without a code.
- **Relay (built, A4):** `POST /pair/v2/arm/link` runs the same owner check as `/pair/v2/arm` (fresh `config/auth/list`, never client-claimed), needs Cloud remote instead of the secure port, and arms a 256-bit, two-minute, single-use slot for that ingress user only. Its finish yields the same user-bound Cloud device as the owner code (no pin), activated over ingress by that user only. The owner's "Cancel" on the NOVA page withdraws it; the device is revoked like any other. A finished sign-in pairing logs one line (`via`, `user_id`, `device_id`; never the secret).
- **Feature detection, no version floor:** an own path instead of a body flag, because older Apps ignore the body and would arm a local slot. An older App answers 404 and the CLI asks for the owner code as before; `min_relay_version` stays. Needs the App release that ships this route.
- Fallback to the owner code on any failure before the finish request (404, `OWNER_REQUIRED`, `PAIRING_UNAVAILABLE`, owner check unavailable, rate limit, network) and on the Relay's own finish refusal; an unknown finish outcome, a different Relay, or a user stop ends setup instead.
- Non-owners keep the owner code.
- Built on the #733 per-profile `client_install_id`.

**7.3 Clients.** All detected clients are configured and named on step 1. No client → download links and [Check again]. Claude Desktop-only users are accepted.

**7.4 Window and phone in parallel (decided; built in A7, `cli/confirm_both.go`).** With a window available and a phone linked and reachable, a typed window plan asks on both at once.
1. **One plan, first answer wins.** Both show the same plan. The first decisive answer (Confirm *or* Cancel; on the phone also a tap from another user, which stops it) wins inside the apply process; apply then takes the plan's atomic claim as always (`cli/verb_apply.go`), so one plan applies at most once.
2. **Withdraw the other at once.**
   - The window closes: macOS kills `osascript` and Linux kills `zenity` (each owns its dialog); Windows posts `WM_CLOSE` to the message box (the `#32770` dialog on its own locked thread, found with `EnumThreadWindows`), retried for up to 2 s while the box may not exist yet.
   - The phone stops listening (its action ids die with the ask), and the result replaces its notification by the same tag, without buttons (also on the Watch). The replacement comes once apply has finished, so the buttons show for those seconds; a tap then counts nowhere.
   - A late answer changes nothing: no one reads it. A window that can't be closed stays until its own timeout; its text says "The first answer counts; a later one changes nothing."
3. **One deadline** for both: the window's wait (90 s in the CLI, 40 s in `ha-nova mcp`). On expiry both are withdrawn and nothing is sent; the plan stays for a new try.
4. **Reachability.**
   - Notify fails → the window alone continues.
   - Linked phone unreachable at apply → the window alone, and the terminal says why the phone stays quiet.
   - Window can't open → the phone alone decides, in the same apply.
   - Neither → refused (`CONFIRM_UNAVAILABLE`), never a chat code.
   - No window (or windows off) → the phone alone, as before; no phone → the window alone. `ha-nova serve` never shows a window (§11.4).
5. **Result notification** (whenever the phone was asked): "Done: Unlock “Front Door”" with "Approved on the computer." before the result line when the window answered (never a device name: a watch shows it too); "Cancelled on the computer. Nothing changed."; on expiry "Not confirmed … Nothing changed." Apply's JSON reports where it was confirmed as `plan.channel` (`window` or `phone`), and the terminal says it too.
6. **Settings changes never race.** `confirm-window off` is confirmed in the window alone; `confirm-phone` link, move, and off keep their own rules (`allowPhoneChange`): the linked phone alone while it can answer, else the window. A race would let the weaker channel approve the change that removes the stronger one (on Windows and X11 a same-user process can click the window), so those stay single-channel.
7. **Known limits.** A model that can fire Home Assistant events with the plan's action ids could cause a rejected tap, which withdraws the window (refusal only; it never confirms). Where `config.json` links a phone for another Home Assistant, the plan's `say` names the phone, but apply asks only the window and the terminal says why. Windows and Linux withdrawal is untested on a real desktop.
8. **Tests** (`cli/confirm_both_test.go`): each side first with the other answering late (Confirm/Cancel both ways, and a window that can't be closed), simultaneous answers (at most one apply, outcome matches the winner), window can't open, the OS closes the window, notify failure, both out, phone unreachable, expiry.

**7.5 Step 4 insights and the tailored automation** (decided: the prototype's insights plus the made-for-your-home automation).
- **Built by the CLI, deterministic, no AI:**
  - insights come from the `map` first-look index (low batteries, unavailable devices, automations that can't run, devices without a room);
  - the automation comes from a small fixed template set over real device pairs in one area or with matching names: motion + light, door/lock + light after dark, contact + notify. Each template is a vetted body with golden tests.
- **Preview first, save only on click.**
  - Every action builds a plan with today's verbs: `plan write` for automations; for rooms, a grouped change set of area creates and entity/device area updates.
  - The page shows exactly that plan's preview.
  - [Save] posts the plan ID; `apply` re-checks drift, writes once, and verifies by read-back.
  - A rooms plan is several writes. `apply` records each item's previous area before writing it. If a write fails, it rolls the earlier writes back from that record (deleting areas it created) and reports "Nothing was saved". If a rollback write fails too, the result names exactly what stayed changed.
  - Only preview-tier plans qualify. A plan that would need a typed confirmation is never offered on the page.
- **Undo.**
  - Automations get the normal snapshot and revert.
  - Rooms: the plan records each item's previous area so "undo that" restores it. Registry edits have no config snapshot today, so this record is new (§15 R7).
- **Honesty.** A page-saved automation skips the AI's 40+ rule review: templates are pre-reviewed instead. The row says "Saved and checked" only after the read-back.
- **No actuation from setup:** no blink, no scene test, no service calls.

**7.6 Step 4 data contract (A8, built).** The page calls these in-process (or as commands) and renders HA-sourced text with `textContent` only.
- **Read:** `ha-nova map --insights` (`(*homeIndex).insights` in `cli/home_insights.go`). `result`:
  ```json
  {"stats":{"rooms":9,"devices":42,"lights":18,"locks":2,"automations":40},
   "insights":[
    {"kind":"battery_low","entity_id":"sensor.x","name":"Outside Temperature battery","area":"Garden","value":"12%","count":3,"action":"battery_reminder"},
    {"kind":"no_room","count":60,"action":"rooms","suggestions":[{"device_id":"d1","name":"Kitchen Lights","area_id":"kitchen","area":"Kitchen","confidence":"high"}]},
    {"kind":"automation_unavailable","entity_id":"automation.x","name":"X","count":1},
    {"kind":"device_offline","device_id":"d2","name":"Garden Sensor","area":"Garden","value":"3d","count":2},
    {"kind":"automation_never_ran","entity_id":"automation.y","name":"Y","count":4}],
   "automation":{"template":"door_light_after_dark","uses":[{"role":"trigger","entity_id":"lock.front_door","name":"Front Door"},{"role":"light","entity_id":"light.hall","name":"Hall Light"}]}}
  ```
  - Insights in this fixed order, each only when present; the page shows the first 3:
    1. `battery_low`: device batteries (`entity_category: diagnostic`; never phones or private devices) below 20 %, lowest first;
    2. `no_room`: devices without any area (`count` includes all, `suggestions` up to 25);
    3. `automation_unavailable`: automations that can't load;
    4. `device_offline`: devices whose every entity has been `unavailable` ≥ 24 h (`value` = the youngest such age; `last_changed` restarts with Home Assistant, so a restart in the last day hides them);
    5. `automation_never_ran`: enabled automations with no recorded run.
  - Room suggestions use existing areas only: `high` = the area's own name is in the device name (a longer matching name wins: "Guest Bedroom" over "Bedroom"); `medium` = one room word (kitchen/Küche, bedroom/Schlafzimmer, living/Wohnzimmer, bath/Bad, …; German compounds by stem) that exactly one area has. Two rooms named, or a room word of another kind beside the match, suggests nothing.
  - Never named: persons, trackers, zones, phones (`mobile_app` devices), and any device carrying a presence entity (a car, a tag): not in batteries, suggestions, offline rows, or as a template trigger or light. The away template counts persons and reads `zone.home`; `uses` lists neither. The one exception is the battery reminder's own target: `uses` names the phone linked for confirmations (`role: phone`).
  - `action` and `automation.template` name the plan command; absent = no preview action. The battery row's `action` is set only when the reminder covers that battery and it is still at or above 10 % (the trigger fires on crossing). `automation` is the first match of `door_light_after_dark`, `lights_off_when_away`, `battery_reminder` (the last only when the battery row doesn't carry it as its action) whose plan computes as preview tier; a template whose plan would need a code (or can't be classified) is never offered, in either place.
- **Preview:** `ha-nova plan rooms` or `ha-nova plan template <id>`. Both store a preview plan (exit 3) and return the normal plan envelope: `plan.id`, `tier: preview`, `say`.
  - Rooms: `result.changes[]` = `{device_id, name, from_area_id, from_area, area_id, area}` (area fields omitted for "no room"; `area_id` is where the device ends up, so an undo that clears its own area shows its parent's room), at most 25, recomputed by the CLI (the page never sends pairs). `say` is neutral ("… Save these rooms?"); the page shows its own button.
  - Template: `result` = `{template, uses}`, plus `config.body` (the exact draft saved) and `diff`.
  - Refusals (exit 4, no plan): `NO_ROOM_SUGGESTIONS`, `TEMPLATE_NO_MATCH`, `TEMPLATE_NOT_OFFERED` (its plan would be typed).
- **Save:** `ha-nova apply --plan <id>`, the normal path.
  - Template: `status: verified` after the read-back.
  - Rooms: `verified` with `result.changes` and `undo` (`undo.target_id` is the rooms record; `undo.evicted` names older checkpoints it pushed off the 5-slot stack, which the page shows). After a failed write the CLI puts the earlier ones back and reads the rooms again: `ROOMS_NOT_SAVED` (exit 6) only when that read (or, if it fails, each write's known outcome) shows nothing changed; `ROOMS_PARTIAL` (exit 6, or 7 when the read fails: "possibly changed") names each device changed now, and the undo record holds what is still to undo.
  - Drift (rename, own or inherited room change, area gone) sends nothing. Its envelope carries no `result.changes`, so the page runs `plan rooms` again and never saves the drift's plan id unseen.
- **Undo:** `ha-nova plan revert --target <undo.target_id>` plans the way back, then apply. A plain `plan revert` never picks a rooms record ("undo the last update" stays with automations); it answers `REVERT_UNSUPPORTED` whose `next` names the record and says to ask the user first. Devices already back are left out of an undo (`NOTHING_TO_UNDO` when none is left).
- **Templates** (golden bodies in `cli/home_insights_test.go`):
  - `door_light_after_dark`: a lock `from: [locked, unlocking]` `to: unlocked`, or a door/garage-door contact `from: off` `to: on`; only between sunset and sunrise; turns on a light in the same area (names never pair: brand words would);
  - `lights_off_when_away`: `zone.home` below 1 for 10 min turns off every light (1–30 lights; needs persons whose locations are all known);
  - `battery_reminder`: any device battery level below 10 % notifies the phone linked for confirmations.
- **Deviations from §7.5:** templates pair by area only, never by name; rooms go into existing areas only (no area creates, so the undo record is one area per device); the reminder threshold is 10 %, not D4's 5 %; Q1 ships A plus "never ran" without a 30-day window; motion + light is not a template yet.

## 8. Demo path (`ha-nova try`)

- `ha-nova try` (A10) and step 1's "Look around the demo house" connect to `demo.hanova.app`:
  - the relay token comes from `connect.json` (public, shared, no pairing); its `relay_url` must share the origin of `connect.json`, and the relay must answer `/health` before anything is saved;
  - the profile `demo` lives in `demo.json` beside `config.json`, never in it: it cannot become the default, and only `HA_NOVA_SERVER=demo` or `--server demo` selects it;
  - `setup`, `pair`, and `cloud` refuse a demo selection; new server profiles can't be named `demo` (an existing one keeps working, and `try` then refuses);
  - `try --stop` and `uninstall` remove `demo.json`; `try` adds the skills only when no AI app has them yet;
  - undo checkpoints saved on the demo are tagged `demo`, and the demo never reverts to an untagged one.
- On the demo, typed verbs carry a `demo` notice and no Relay update notices; an unreachable or resetting demo is one plain line. With only the demo connected, an unselected verb says how to select it. One session-bootstrap line names `--server demo`, and the Claude Code session hook says when the demo is connected.
- Step 4 reads "A shared demo house. It resets every hour; never type real names or passwords."
- The demo profile gets no phone link and no Claude rule offer.
- Not done: the `setup --demo` alias (one command is enough) and step 1's link (page work, A6).

## 9. Migration from the current wizard

- **Choosing the path:**
  - a desktop `ha-nova setup` opens the page;
  - `--no-browser` (or `HA_NOVA_NO_BROWSER=1`) uses the text path;
  - SSH, headless Linux, Windows session 0, or no TTY and no display select the text path automatically.
- **Text path** = the same engine, steps, and catalog, with arrow menus everywhere and a typed fallback (prototype "no-browser" screen):
  - "✓ Home Assistant found · ✓ Claude Code found", then "› Use a six-digit code from NOVA / Skip for now";
  - over SSH on Windows and macOS: "The saved login needs the desktop." One line, then it stops before changing anything.
- **Flags keep working:** `--non-interactive`, `--client`, `--service`, `--server`, `--ha-url`, `--relay-url`, `--relay-token`.
- **Stored state:** configs, profiles, and credentials from 0.26 are read unchanged. No stored state changes, so no backwards-compatibility decision is needed. The connection-mode prompt leaves the desktop path, and `ha-nova cloud add` stays.
- **Uninstall:**
  - `ha-nova uninstall` asks one arrow-menu question: "Remove HA NOVA from this computer? Your Home Assistant stays as it is. › Remove / Cancel".
  - Removing revokes this device and removes the skills, client config, and Claude rule.
  - Only for the last paired device (and with chat apps off): "Also remove the NOVA App from Home Assistant?" → Keep it (default) / Remove it.
  - "Remove it" opens the page in sign-in mode and removes the App and its repository via Supervisor (owner only). Chat apps are off by then, so the integration's entry and files are already gone (§11.6).
  - Over SSH it prints today's manual steps.

## 10. Skills language pass (Sonnet run on rc3, 2026-10-18)

The rules go in one block in `skills/ha-nova/output-rules.md`, pinned by `tests/skills/plain-language-contract.test.ts`. Budgets are ratchets: trim, never raise.
1. **No jargon** unless the user used it or asked: no entity/Entität, domain, YAML, service call, payload, registry, entity_id. Say device, light, sensor, room, automation, setting.
2. **Never tell the user to run a command.**
   - The agent runs `ha-nova` itself; user-only actions are described in words.
   - This replaces #738's "the user runs `ha-nova setup`": setup started from chat opens the page.
   - In chat apps (§11) there is no shell: point to NOVA in the Home Assistant sidebar.
3. **HA names verbatim** ("Bed Light", never "Bettlicht").
4. **No option tokens.**
   - "apply / show yaml / cancel" becomes a natural question ("Save it, show the details, or leave it?").
   - The explicit-approval rule is unchanged.
5. **"Where is X used" includes dashboards.** The `map` where-used scans storage-mode dashboards, and the answer names the views.
6. **One-line coverage disclaimers**, no "list truncated" noise.
7. **Each fact once per answer.**

Verification: an E2E agent run on the test bed (Sonnet and Haiku, German and English) with the nine prompts of the 2026-10-18 run plus "what can you do", reviewed against rules 1–7. The same set runs through the bridge (§11).

## 11. Chat-app bridge over Home Assistant Link

### 11.1 Shape

Anti-tool-catalog: the bridge only carries the skills and the existing typed verbs to chat apps. It adds no tools of its own.

```
Claude (web, desktop, iOS/Android) · ChatGPT
  → https://<id>.ui.nabu.casa/api/ha_nova/mcp         the integration's own HTTP view (Streamable HTTP) + HA OAuth
  → integration ha_nova (custom_components)           pure pass-through of the HTTP exchange (§11.3), adds secret + HA user ID
  → NOVA App: `ha-nova serve` (same Go binary as CLI)  skills, typed verbs, tier engine, plan store, phone channel
  → NOVA Relay over loopback (internal device credential) → Home Assistant
```

Why this shape:
- **Only an integration can add a route through Link.** Home Assistant Link forwards only HA's own HTTP, and only an integration can add a view there.
- **HA does the OAuth.** Every authenticated HA view gets HA's sign-in, consent, and token revocation (the 401 with `resource_metadata` and the well-known documents are global); we don't build an OAuth server.
- **Own view, not HA's MCP server endpoint** (B0). Behind `/api/mcp/<api_id>` the server instructions are dropped, results are wrapped in a JSON string, every call is capped at 60 s, only admins get in, and a separate MCP server entry is needed. The own view passes all of this through unchanged and needs no other integration.
- **No second implementation.** The tier engine, plan store, verbs, and skills are the CLI's Go code, run as a server process inside the App. Nothing is ported to Python or TypeScript.
- **The Relay stays a dumb pipe.** The bridge is a separate process that talks to the Relay like any paired device.
- **The integration is a pure proxy.** A contract test fails it on any logic beyond discovery, the proxy, and the user ID hand-off (like `check-docs.sh` [3]–[5] for the Relay).

### 11.2 Tools and skills

- Exactly the 10 tools of today's `ha-nova mcp`:
  - `ha_skill`;
  - `ha_find`, `ha_get`, `ha_history`, `ha_map`, `ha_trace`, `ha_read`;
  - `ha_plan`, `ha_call`, `ha_apply`.
- The same server instructions ("load the skill first").
- The skills bundle ships inside the App image, version-locked to the binary.
- **Limit:** without a shell, writes beyond device control and automation/script changes (helpers, dashboards, registry, files) are refused with "needs HA NOVA on a computer". Skills say so in one line (§10 rule 2).

### 11.3 Auth and identity

- **Connector sign-in** is HA's own OAuth on the Link address: PKCE plus a client-ID metadata document, which needs HA 2026.10 or newer (verified in the spike, §13 B0).
- **Off by default.** The owner turns on "Chat apps" on the NOVA page or in step 3, and picks the allowed HA users (default: the owner only). The bridge re-checks the user on every call, as the owner check does today.
- **Everything the bridge stores belongs to one HA user:** plans (`ha_plan`, `ha_call`), pending phone asks, and phone links carry the ID of the user who created them. `ha_apply` and every `waiting_for_phone` re-call act only on a plan of the calling user; another user's plan ID answers as unknown.
- **Revocation:** NOVA page → Chat apps → turn off, or remove a user; the effect is immediate. The token itself is revoked in HA → Profile → Security, and the NOVA page links there.
- **Honest limits:**
  - access is per HA user, not per device;
  - the chat-app vendor stores an HA token without scopes, as with any HA-based connector.
- **Integration ↔ App:**
  - the App publishes a Supervisor discovery message (`POST /discovery`, service `ha_nova`, config: host, port, secret). `discovery: [ha_nova]` in `config.yaml` only permits it. The App keeps the returned UUID in `/data`;
  - host is the App's stable Supervisor hostname, never an IP. The 256-bit secret is generated once and kept in `/data`. App restarts and updates reuse both; on start the App re-posts its message (Supervisor keeps one per App and service, so an unchanged one is a no-op; an App can't list messages). The secret changes only by turning chat apps off and on;
  - the integration's `hassio` config flow stores them with the message's uuid as unique ID (Home Assistant removes a `hassio` entry only by that uuid when the message is deleted). A changed message updates that entry; a new message from the same App replaces it; there is never a second;
  - the bridge port is internal to the Supervisor network and never mapped to the host;
  - the user ID passed by the integration is trusted only with that secret.
- **Proxy contract.** The integration forwards the whole Streamable HTTP exchange, not just the body:
  - the method, and the body streamed both ways (JSON or SSE);
  - request headers `Content-Type`, `Accept`, `Last-Event-ID`, and every `Mcp-*` header (protocol version, session, routing). It drops `Authorization`, cookies, and any client-sent user header, then adds the secret and the HA user it authenticated;
  - back: the status, `Content-Type`, and every `Mcp-*` header.

  The bridge, not the integration, validates them. It is stateless in 0.27: POST only, 405 for GET and DELETE.

### 11.4 Confirmations (same tiers as the CLI)

- **No window:** the bridge runs on a server.
- **Phone.** The linked phone of *that* HA user asks, using the same Companion flow, result notification, and Watch support as §7.4.
  - Two stores, no sync. The bridge reads only the chat-app links in the App's `/data`, keyed by HA user. A computer reads only its own link in its `config.json`, as in 0.26.
  - Users link a phone for chat apps on the NOVA page (same pick list and link tap as step 3). When step 3 turns on chat apps right after linking a phone, the CLI also stores that link as the owner's chat-app link (an owner action over the ingress session), so the owner taps once. Later changes affect only their own side; the NOVA page shows which phone chat apps use.
  - `ha_apply` waits up to 45 s inside the call. If no answer has come, it returns `status: waiting_for_phone`, and the model calls `ha_apply` again. That call is idempotent and sends no new notification.
- **No phone linked → chat code, explained as low protection:**
  - The code appears in the result with fixed text (0.27 has no form path for chat apps): "The AI can see this code too, so it protects less than a tap on your phone. Type it yourself to confirm. Link your phone in NOVA for real protection."
  - The owner can turn chat codes off (NOVA page). Typed plans are then refused until a phone is linked.
- **Owner switch "Chat apps can only read"** (default off): for owners who want chat apps as a read-only view.

### 11.5 Phone use case

The user is in the Claude app on an iPhone, away from home, with no computer on:
1. "Is the garage open?" → `ha_get` → "Yes, since 18:02."
2. "Close it." → ordinary tier → closed and verified.
3. "Unlock the front door for the neighbor." → a typed plan → a Companion notification on the same iPhone (and its Watch) → tap Unlock → the result notification appears, and the chat says "Front Door unlocked ✓ (confirmed on this phone)".

ChatGPT works the same way on the web only; its mobile app is not claimed, since writes there are reported as disabled (§15 R2). Claude Code Remote Control and Codex mobile stay a second, documented phone path for computer users. They use §7.4, so an unattended window never blocks them.

### 11.6 Turning it on (step 3 row or NOVA page)

1. The owner turns on "Use HA NOVA in Claude and ChatGPT". This requires an active Link remote and HA 2026.10+.
2. Both entry points call the same owner-only App action: the NOVA page directly, the CLI through its owner ingress session (§6). The switch state lives in `/data`, not in a Supervisor App option, so the running App acts at once, without an App restart, and never rewrites the App's other options.
3. The App writes `custom_components/ha_nova` into `/config`. It needs no new permission: `homeassistant_config` is already mapped for `/files`. It writes only that directory, and a test pins this.
4. The App publishes its discovery message (§11.3).
5. Home Assistant needs one restart to load a new custom integration (B0: it caches the custom integration list for the process lifetime). The App restarts it through the HA API access it already has (`homeassistant.restart`, which checks the configuration first). The App gets no new permission, and the CLI makes no restart call. The consent line on the switch reads: "Home Assistant restarts once (about a minute)." **This is the only restart in 0.27, and only on this opt-in path.** The setup page waits for HA to return before step 4.
6. HA reads pending discoveries at startup, so the integration config flow completes by itself after the restart, and `/api/ha_nova/mcp` goes live. No MCP server entry is created or changed.
7. The page shows the connector address in a copy bar plus [Show me how]: 3 steps for Claude ("Settings → Connectors → Add custom connector → paste → sign in to Home Assistant") and 3 for ChatGPT.

Every point is idempotent. A failure shows one plain line and [Try again], which runs the whole action again.

Lifecycle rules:
- **Turning it off:** the App deletes its discovery message (stored UUID), HA then removes the config entry, and the App deletes `custom_components/ha_nova`. The loaded code stays inert until HA's next restart; no restart is asked for. Deleting only the entry is not enough: HA re-discovers a removed `hassio` entry.
- **Removing the App in HA while chat apps are on:** Supervisor drops the discovery message, so the entry goes, but the directory stays. Known limit; the NOVA page and docs say to turn chat apps off first.
- **Integration code changes also need a restart.** The integration is therefore a version-stable proxy: tools, instructions, and schemas come from the bridge at runtime, so App updates almost never touch the integration, and the bridge tolerates an older integration.

### 11.7 Assist/voice

0.27 registers no LLM API, so HA's conversation agents (Assist, Companion chat, voice) don't see HA NOVA. Registering one is a 0.28 candidate, with the identical tiers and the phone channel; it must use per-property schemas and keep a regression test on HA's `/api/mcp` (§15 R10).

## 12. Test plan

Unit/contract (CI):
- **Page server:**
  - Host mismatch → 421;
  - cross-origin, form-encoded, cookie-less, and `cross-site` POSTs are refused;
  - token reuse;
  - headers present;
  - hostile HA names render inert;
  - a hostile-origin server tries fetch, a form, and an iframe, and all fail.
- **Engine:** table tests over App missing/old/running × owner/admin/user × 0/1/several HAs, clients, and phones.
- **Supervisor client:** allowlist, repository-source mismatch, job failure, timeout.
- **§7.4 race suite.**
- **§7.5:**
  - template golden tests;
  - save-by-plan-ID only;
  - drift refusal;
  - the rooms undo record, and rollback after an injected failure at each write.
- **Relay:** armed Link slot → user-bound device; local unchanged; non-owner arm → 403.
- **Bridge:**
  - tool parity with `ha-nova mcp`;
  - user allowlist re-check;
  - chat-code policy and its switch;
  - the `waiting_for_phone` re-call is idempotent;
  - another user's plan ID is refused on `ha_apply` and on the re-call;
  - integration proxy-only contract; method, status, `Mcp-*` headers, and an SSE body pass both ways; client auth and user headers are dropped;
  - discovery secret required; the message is published and its UUID kept; App restart and App update keep the connector working without re-enabling;
  - enable from the NOVA page and from step 3, each without an App restart, other App options untouched; a failure at each point, then [Try again];
  - turning off removes the message, the entry, and the directory;
  - the App writes only `custom_components/ha_nova`.
- **i18n catalog checks (§4.2); skills contracts (§10).**

Real-device lanes (the ones that found real bugs on 2026-10-18):

| Lane | Covers | Who |
|---|---|---|
| Throwaway HAOS VM, App absent / old / present | Supervisor install and update, pull time, repository check (rc2 item 3 still pending) | agent |
| HAOS users owner, admin, `tester2` | step 2 branches, code fallback | agent |
| Win11 ARM64 VM, admin desktop + standard user over SSH | page flow (Edge), no firewall prompt; text path; uninstall (N1) | agent |
| Separate macOS user/VM; Linux desktop container lane + SSH; WSL2 | page flow (Safari, Chrome, Firefox), keychains, `--no-browser` | agent |
| Throwaway HAOS VM, dev App with chat apps on (required before the 0.27 tag) | real Supervisor discovery: turn on → `ha_nova` entry appears after the restart; turn off (deletes the message) → the entry goes, matched by the message's uuid, not the App slug (B2's open question); off and on again → one entry with the new secret; App update and restart → same entry, connector still works | agent |
| HA Link test account on the HAOS VM | owner without code; **bridge**: enable flow, restart, claude.ai web + iOS/Android app, ChatGPT web/mobile, revoke | maintainer (Cloud checklist) |
| iPhone + Watch, Android emulator | step 3 link, §7.4 race both ways, result notifications, §11.5 script | maintainer |
| Claude Code auto mode, Sonnet + Haiku | rule preselected → unlock via window, no block; §10 scenarios | agent on the test bed |

Timing protocol: wall clock from Enter on the installer line to the first verified action, plus the action count (one click, Enter, or completed form = 1; an HA login = 1).

## 13. PR train

Production LOC estimates exclude tests and assets. HR = high-risk, RB = release-bound.

| # | PR | Size | Class |
|---|---|---|---|
| **B0** | Bridge spike, no PR. **Done 2026-10-19: go with changes** (own `/api/ha_nova/mcp` view, restart required, discovery flow works, HA 2026.10 floor confirmed). Remaining: the maintainer's claude.ai/ChatGPT connector test on the throwaway test HA "HA NOVA HAOS Test" with the Link trial, never the home HA (R9, R5 over Link) | done | gate |
| A1 | `fix(skills)`: plain-language pass | ~150 skill lines, net ≤ 0 words | behavior |
| A2 | `feat(map)`: where-used includes dashboards | ~200 | behavior |
| A3 | `feat(setup)`: App install/update via Supervisor after sign-in (text path first) | ~400 | HR |
| A4 | `feat(link)`: owner pairs over Link without a code (Relay + CLI) | ~250 | HR, RB |
| A5 | `refactor(setup)`: `setupFlow` engine, text renderer, arrow menus, string catalog | ~500 moved, ~200 new | behavior |
| A6 | `feat(setup)`: local page: server, security, steps 1–2, hanova.app tokens | ~450 Go + ~40 KB assets | HR |
| A7 | `feat(confirm)`: window and phone in parallel, result notification; step 3 | ~350 | HR |
| A8 | `feat(setup)`: step 4 data (§7.6): stats, insights, rooms and automation templates, save by plan ID, rooms undo record; the page's step-4 rendering joins A6 | ~450 | HR |
| A9 | `feat(uninstall)`: one question; last-device App removal | ~250 | HR |
| A10 | `feat(setup)`: `ha-nova try` | ~150 | behavior |
| B1 | `feat(mcp)`: `ha-nova serve` (Streamable HTTP on an internal port, secret auth, server-side plan store bound to the HA user, phone-only/chat-code policy, `waiting_for_phone`) | ~450 | HR |
| B2 | `feat(app)`: the App runs the bridge (binary + skills in the image, internal device credential, `/data` state: switch, secret, discovery UUID, phone links; owner action on/off with discovery and the HA restart; NOVA page "Chat apps") | ~400 | HR, RB. Lands dormant (`HA_NOVA_CHAT_APPS` off, no `config.yaml` change); apply in the App release bump: `discovery: [ha_nova]` in `config.yaml`, `ARG HA_NOVA_CHAT_APPS=1` in `nova/Dockerfile` (contract doc, "App side") |
| B3 | `feat(integration)`: `custom_components/ha_nova` (discovery flow, `/api/ha_nova/mcp` pass-through view per §11.3) + App delivery + Python CI | ~250 | HR, RB |
| B4 | `feat(setup)`: step 3 chat-app row + enable flow; docs: connect Claude/ChatGPT, use from the phone (also Remote Control/Codex) | ~150 + docs | behavior |
| R | Release prep: README "Get started", site sync (incl. tokens), flow video | docs | release-prep |

Order:
- **Parallel starts:** A1, A2, A3, and B0. A4 waits for the `client_install_id` fix.
- **Setup chain:** A5 → A6 → A7/A8 → A9.
- **Bridge:** B0 said go with changes, so B1–B3 may start. B2 needs B1, and B4 needs A7 and B3. B3 drives Home Assistant's own `hassio` discovery code in pytest (Supervisor mocked); the real Supervisor run is a release lane (§12).
- **If the remaining maintainer test fails on R9 and the own-view metadata fix doesn't help:** the bridge moves to 0.28, and 0.27 ships phone use through Remote Control/Codex (B4 docs only).
- **Review:** every HR PR gets the security, UX, and sibling-sweep reviewers before opening.

Sum: about 3.4k new production lines in A and about 1.25k in B. B roughly adds 3–5 weeks to the critical path.

## 14. Launch criteria (0.27 RC, §12 lanes)

| Bar | Target |
|---|---|
| Terminal questions on a desktop | **0** |
| Typed or pasted besides the installer line and the HA login (computer path) | **0** |
| User actions, installer → step 4, App absent / present | **≤ 5 / ≤ 4** |
| Browser tabs opened by setup | **1** |
| Time to first verified action, App absent (prebuilt image, LAN) | median **≤ 4 min**, worst of 5 ≤ 6 min |
| …App present | median **≤ 90 s** |
| Fresh-install success without fallback, per desktop OS | **10/10**; text path **5/5** |
| Link owner path without a code | **3/3** |
| E2E agent scenarios on the test bed (§10 set, Sonnet + Haiku, DE + EN), CLI and bridge | **100 %** task success, **0** jargon/command-to-user findings, **0** translated HA names |
| Claude Code auto mode: unlock after setup | works, no manual `/permissions` |
| Window + phone race (§7.4) | exactly one apply in **20/20** concurrent-answer runs |
| Bridge enable → first verified action in claude.ai (phone) | **≤ 6 actions** (one paste of the connector address), **≤ 5 min** incl. the restart |
| Unlock from the phone with no computer on (§11.5) | **3/3** claude.ai iOS + Android; ChatGPT **3/3** where the spike says it works |
| Setup changes to the home | **0** without a click; every saved item verified by read-back |
| Uninstall | one command, ≤ 2 actions, opaque markers only, **10/10** |
| Security | all §6/§11 controls tested; 0 open P1/P2 |

## 15. Biggest risks

| # | Risk | Mitigation |
|---|---|---|
| R1 | **Integration delivery:** a new custom integration needs an HA restart (confirmed in B0), integration code updates need one too, and App-written code under `custom_components` is a new trust surface | opt-in only, explicit restart consent, write scope pinned by a test, turning off removes the directory; version-stable proxy so App updates rarely touch it; fallback is a HACS/marketplace listing |
| R2 | **Connector reliability** (OAuth against HA via Link is a known flaky class); ChatGPT custom-connector writes are documented for the web only and reported as disabled on mobile | maintainer test on the throwaway test HA with the Link trial; claim only what the lanes pass; copy says "ChatGPT on the web" |
| R3 | **Weaker human gate in chat apps:** no window, and Claude shows no forms, so the chat code is visible to the model | phone first; chat code explained as low protection, owner can switch it off; the read-only switch |
| R4 | **Token scope:** the vendor holds an HA token without scopes; revocation is per user, not per device | off by default, per-user allowlist, NOVA page revoke + HA token link; stated plainly in docs and `safety.md` |
| R5 | **Phone wait vs tool-call timeouts.** Claude allows 240 s per tool call; a 45 s call passed through HA in B0. The Home Assistant Link proxy timeout is undocumented (R-Link) | 45 s wait + idempotent `waiting_for_phone` re-call; integration timeout above the bridge wait with a readable error; shorten the wait (e.g. 25 s) if Link cuts earlier; first Link test on the throwaway test HA |
| R6 | **Scope and schedule:** the bridge adds ~1.25k HR lines plus a Python component and a release-bound App change | the B0 gate and the no-go fallback in §13; Matter/Thread/Z-Wave moved to 0.28 |
| R7 | **Page-side writes** (step 4) bypass the AI review; room changes have no snapshot today | fixed pre-reviewed templates, preview-tier only, save by plan ID, the new rooms undo record |
| R8 | **Prompt injection** in chat apps with web or file access steering ordinary actions | same tiers as the CLI; typed actions always reach the human; read-only switch |
| R9 | **OAuth resource path (R-OAuth):** HA's protected-resource metadata names only the origin, while Claude's docs require the `resource` to match the MCP URL including its path | open; the maintainer test against `/api/mcp/assist` (same path shape) decides; fallback: the own view answers its 401 with its own metadata document whose `resource` is the full `/api/ha_nova/mcp` URL |
| R10 | **HA merged-API bug** (`helpers/llm.py`, `MergedAPI` serializer merge checks `is not None`, while serializers return `UNSUPPORTED`): an LLM API with a custom schema serializer empties or degrades HA's default `/api/mcp` for every MCP client | 0.27 registers no LLM API; any later registration uses per-property schemas plus a regression test on `/api/mcp`; report upstream |

## 16. Open points

Deviations between spec and prototype (D) and open questions (Q). The recommended default comes first.
- **D1** Prototype installer line `irm https://hanova.app/install.ps1 | iex` needs install scripts served from the site.
  - **A** keep the GitHub raw URLs at launch;
  - B serve them from hanova.app with a pinned checksum check.
- **D2** The prototype has no Link "away from home" row. **A** add it to step 3, only when Link remote is active; B leave Link to `ha-nova cloud add`.
- **D3** The prototype has no chat-app row. **A** add it to step 3 (§11.6); B NOVA page only.
- **D4** "Remind me" (battery) is undefined.
  - **A** a one-shot reminder automation on the linked phone when the level drops below 5 %, previewed and saved on click;
  - B drop it for a copyable prompt.
- **D5** "Open Claude Code" can't open a terminal from a browser.
  - **A** the CLI opens the platform terminal running `claude` in a folder `HA NOVA` in the user's home (macOS Terminal, Windows Terminal/PowerShell, Linux `x-terminal-emulator`) and falls back to the copy hint. Not the home folder itself: Claude Code asks its trust question there on every start, and trusting it covers every file. The page says to answer “Yes, I trust this folder”; uninstall removes the folder only if setup created it and it is still empty; a link or junction in its place is refused;
  - B "Open Claude Desktop" when present, otherwise the copy hint only.
- **D6** The prototype's fine print "Nothing in your home changed during setup." **A** "Nothing in your home changed unless you saved it."
- **D7** Prototype step 3 says "Your AI assistant never sees a code". This is true on the computer path; in chat apps without a phone it is not (§11.4). **A** keep it on the page; the bridge rows say it plainly.
- **D8** Prototype stars are 38 random twinkling dots; the site uses fixed layers. **A** use the site's layers.
- **D9** The prototype's step 2 ends with [Continue]. **A** move on by itself when every row is ✓, which keeps the App-present path at 4 actions (§14).
- **D10** The prototype names one phone. **A** a pick list when several are eligible (§4 step 3).
- **Q1** Insight scope at launch.
  - **A** battery, unavailable, can't run, no room (all deterministic);
  - B also "never ran in 30 days".
- **Q2** Bridge users.
  - **A** owner only by default, others added on the NOVA page;
  - B every admin.
- **Q3** App auto-update on install. **A** HA default (off); B on.
- **Q4** Chat code in chat apps.
  - **A** on by default, explained;
  - B off by default (phone required for typed plans).
- **Q5** Restart consent (B0 confirmed the restart is required).
  - **A** in the enable flow, one line plus the button;
  - B ask the user to restart later and finish on the next start.
