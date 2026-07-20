# README relaunch draft — v0.19.0

> **DRAFT — ships only inside the v0.19.0 release-prep PR**, together with the
> `version.json` bump (`readme-release-gate`). Facts are checked against
> `origin/main` (the release-bound state), not against the stable 0.18.1 README.
> Do not copy into `README.md` outside that PR.
>
> Status 2026-07-20: **Fully approved by the maintainer** — all sections, the
> release-notes draft, the launch spine, and all four images (see
> `docs/work/2026-07-20-asset-specs.md`). Image files stay unreferenced by the
> published README until the release-prep PR.

> **Release-prep PR checklist (collected during drafting):**
> `version.json` bump (readme-release-gate) · `nova/config.yaml` 0.6.0 → 0.7.0
> (release notes already say 0.7.0) · replace `assets/how-it-works.png` with
> `how-it-works-v2.png` content (and retire the stale `how-it-works.svg` and
> `banner.svg` — superseded by the vector sources in `docs/work/`; keep
> `logo.svg` [the star geometry source], `icon.svg`, `logo-dark-text.svg`) ·
> update `docs/reference/safety.md` (still describes pre-v1 pairing/Home Base
> in places) · swap `assets/social-preview.png` for `social-preview-v2.png`
> content and upload it in GitHub repo settings **at release** · re-check
> README nav anchors after any heading change · `assets/demo.webp`:
> **REMOVE in the release-prep PR** (maintainer decision 2026-07-20 — it shows
> a very old build and the draft no longer references it; `git rm` on the PR
> branch, not in older worktrees) ·
> **rewrite `docs/reference/comparison.md` against ha-mcp's CURRENT README**
> (verified 2026-07-20): its context-cost claim is outdated — ha-mcp documents
> that deferred-loading Claude clients pull tools in only when needed — and
> its token-storage row misses that ha-mcp's recommended install is in-process
> ("no access token to manage") with webhook-URL/`ha_auth`/OAuth options.
> Update our side too (per-device pairing, no LLAT in App mode) and the
> "as of" dates.
>
> **Linked-docs audit (2026-07-20, all README links checked):**
> `CONTRIBUTING.md`, `.claude/INSTALL.md`, `.antigravity/INSTALL.md` — clean.
> Still carrying pre-v0.19 language, update in the release-prep PR:
> `docs/reference/relay-container.md` line 10 ("there is no Home Base" → NOVA
> page) and line 62 (the "relay emits one pairing code at startup" paragraph
> describes pre-v1 pairing — re-verify the standalone image's actual v0.19
> behavior and rewrite); `docs/reference/safety.md` rows at lines 26/36/38
> (Home Base naming, old `home.ts`/`pairing.ts` references, shared relay-token
> model) on top of the general v0.19 update already listed;
> `.hermes/INSTALL.md` line 51 ("Relay token" → device credential wording).

<!-- PUBLISHED FILE STARTS BELOW THIS LINE — everything above stays in docs/work -->

---

<p align="center">
  <img src="assets/hero-banner.png" alt="HA NOVA — a four-point star, and the line: one code to connect, every change checked" width="100%">
</p>

<p align="center">
  <a href="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml"><img src="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/markusleben/ha-nova/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey" alt="macOS | Windows | Linux">
</p>

<h2 align="center">One code to connect. Every change checked.</h2>

<p align="center">
  Pair your AI with a one-time six-digit code — no tokens to create, no secrets to keep.<br>
  Every change is previewed, approved, and verified before it touches your Home Assistant.
</p>

<p align="center">
  Works with <b>Claude Code</b> · <b>Claude Desktop</b> · <b>Codex CLI</b> · <b>OpenCode</b> · <b>Google Antigravity</b> · <b>Hermes Agent</b> (preview)
</p>

> *"When I get home, set my main lights to a warm welcome scene"* — one sentence in, a fully reviewed automation out, including suggestions you might not have thought of.

## ✦ Three steps, one code

1. **Run the installer** — one command; the wizard finds your Home Assistant and sets up the NOVA Relay App.
2. **Click "Connect a device"** — the wizard opens your new NOVA page in the Home Assistant sidebar for you.
3. **Type the six-digit code** — done. Your AI is paired.

<p align="center">
  <img src="assets/pairing-flow.png" alt="Pairing flow: run the installer, click Connect a device on the NOVA page, type the six-digit code">
</p>

*No tokens to create, no secrets to keep — the code is one-time and expires in minutes. Each device gets its own connection you can revoke anytime from NOVA. Simpler and more secure at the same time.*

<p align="center">
  <b><a href="#-what-you-can-do">What it does</a></b> · <b><a href="#%EF%B8%8F-safe-by-design">Is it safe?</a></b> · <b><a href="#-get-started">Get started</a></b> · <b><a href="#%EF%B8%8F-how-it-works">How it works</a></b>
</p>

> **Actively developed.** Core workflows are proven end-to-end on macOS, Linux, and Windows — and the product is young enough that your feedback still shapes it. Back up your configs before letting AI touch anything. Hit a problem? [Open an issue](https://github.com/markusleben/ha-nova/issues).

---

## 💬 What You Can Do

Say what you want. HA NOVA figures out the rest.

| You say | What happens |
|:--------|:-------------|
| *"Check my automations for problems"* | Audits against 40+ rules — catches conflicts, dead triggers, and mistakes you'd never spot manually |
| *"Why didn't my motion light trigger last night?"* | Replays the actual trace and tells you exactly where it failed and why |
| *"Turn on the porch light at sunset and off at 11 PM"* | Builds, previews, and reviews the automation before it touches your config |
| *"What happened to my living room temperature overnight?"* | Pulls history and summarizes the timeline in plain language |
| *"Add a weather card to my main dashboard"* | Reads the dashboard, shows the change, writes only after you confirm |
| *"Move all kitchen devices to the Kitchen area"* | Reassigns entities and devices — shows what will change first |
| *"Turn off the upstairs lights"* | Turns them off, confirms the new state |
| *"Show me all temperature sensors"* | Finds entities by name, room, area, or label |

> Automations and scripts get the full workflow today: preview, review, and a 40+ rule audit. Helpers, dashboards, and device control get preview-and-verify — deeper audits are rolling out with every release.

---

## 🛡️ Safe by Design

We built this because we didn't trust AI with our own config either.

**What happens when AI wants to change something:**

1. **Researches first.** Finds your devices, checks your setup, resolves entity names. No guessing.
2. **Shows you the change.** A clear before/after table of exactly what will change, before anything is written.
3. **You approve it.** Deletes require a specific confirmation code — not just "yes." A reviewed batch delete takes one code bound to the exact list of items.
4. **Writes and verifies.** Reads the config back to confirm the change stuck.
5. **Audits itself.** Checks for mistakes, conflicts, and reliability issues.
6. **Offers a safe test.** After saving a new or changed automation or script, you get a test plan matched to the risk — from a zero-impact logic check to a real run that names exactly which devices will switch before you say go.
7. **Lets you take it back.** Reply `revert` to undo the latest verified update. New items are removed through the same preview-and-confirm delete flow.

**The ground rules — always:**

- 🔒 **Your Home Assistant credentials never leave the server.** The App connects through Home Assistant's own built-in access — there is no token for you to create, and the AI never sees any Home Assistant credential. (Standalone Container/Core keeps its token server-side the same way.)
- 🔑 **Every device is paired on its own.** The six-digit code is one-time and short-lived; what it creates is a per-device credential over an encrypted, pinned connection (OPAQUE, RFC 9807 + SPKI-pinned TLS 1.3 under the hood). (On App installs; the standalone container uses one shared token instead.)
- 🗑️ **Revoke with one click.** Lost a laptop? Retired a machine? Cut off any paired device from the NOVA page — everything else keeps working.
- 📖 **Every rule the AI follows is a markdown file you can read.**
- 🏠 **No cloud relay, no telemetry.** Your data stays between your machine and your Home Assistant.

---

## 🚀 Get Started

One installer. One wizard. One code. It even finds your Home Assistant automatically.

> **You need:** Home Assistant — any install type. HA OS and Supervised get the NOVA Relay App, and the wizard below walks you through it. Container and Core run the same relay as a [standalone container](docs/reference/relay-container.md) — same skills, same safety guarantees, with a token-based setup: skip the steps below and follow that guide end to end instead.

**macOS / Linux:**

```sh
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex
```

The installer selects the latest stable release. Run the command for your OS; the wizard discovers your Home Assistant, walks you through installing the NOVA Relay App, and configures your AI clients. At the pairing step it opens your new NOVA page — click **"Connect a device"**, type the six-digit code, done.

Once it finishes, try: *"Show me all my automations."*

> **Upgrading from an earlier HA NOVA?** Everything keeps working through the update. To move to per-device pairing, re-pair each computer once (one code each), then revoke the old shared access from the NOVA page whenever you're ready.

<details>
<summary><strong>Platform notes</strong></summary>

<br>

**macOS** — proven end-to-end on real hardware.

**Linux** — proven end-to-end, including headless setups (servers, containers, LXC, SSH): with no desktop keyring around, the device credential falls back to a private file automatically.

**Windows** — proven end-to-end on a live Windows 11 system. Ships x64 builds (Windows ARM64 can use the x64 build through emulation; no native ARM64 bundle is shipped).
- Claude Code is the most tested client on Windows today
- Google Antigravity has basic validation
- Codex and OpenCode are still early
- Native prerequisites: Claude Code needs Git for Windows / Git Bash; Google Antigravity Desktop or CLI must be installed
- See [.claude/INSTALL.md](.claude/INSTALL.md) and [.antigravity/INSTALL.md](.antigravity/INSTALL.md)

> Do not download the `ha-nova-installer-bundle-*.tar.gz` / `.zip` assets manually. Those archives are used by the installer behind the scenes. Always use the one-liner or `ha-nova update`.

</details>

<details>
<summary><strong>CLI commands & legacy uninstall</strong></summary>

<br>

| Command | What it does |
|:--------|:-------------|
| `ha-nova check-update` | Check for newer versions |
| `ha-nova update` | Update to the latest release |
| `ha-nova setup` | Re-run the setup wizard |
| `ha-nova pair` | Pair this computer again with a fresh six-digit code |
| `ha-nova doctor` | Diagnose connection and config problems |
| `ha-nova uninstall` | Remove HA NOVA (keeps settings for easy reinstall) |
| `ha-nova uninstall --purge` | Full cleanup including saved settings |

**Coming from a pre-Go install?**
- macOS / Linux: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh | bash`
- Windows: `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1 | iex`

</details>

> **Not a terminal person?** Setup is one copy-pasted command; after that, Claude Desktop and Google Antigravity Desktop give you the same skills in an app window — and pairing another computer is just one more six-digit code.

---

## ⚙️ How It Works

<p align="center">
  <img src="assets/how-it-works-v2.png" alt="How HA NOVA works: your AI client and markdown skills on your machine talk to the NOVA Relay on your Home Assistant server over a pinned, per-device connection. The relay is the only part that talks to Home Assistant.">
</p>

**Skills** live on your machine as plain markdown. They're the AI's playbook — what to check, what to show you first, what to verify after a change. You can open them, read them, even edit them.

**The Relay** lives on your Home Assistant server. It's the only part that talks to Home Assistant directly, and whatever credential that takes stays inside it — never on your laptop, never in a prompt. (In the App there's nothing to create at all: it uses Home Assistant's own built-in access.) Each device you pair reaches the relay over its own encrypted, pinned connection, and the NOVA page is where you pair new devices or cut old ones off.

Most new HA NOVA workflows are text-file skill updates on your machine. The Relay only changes when a workflow needs something genuinely new from Home Assistant — and even then it stays the dumb pipe: all the know-how stays in the skills.

And because the Relay sits right next to Home Assistant, it can do things a remote client can't — like snapshotting an automation before it updates it, so you can revert the latest verified update with a single word. Deletes capture a config snapshot first, so a deleted item can be restored through the same preview-and-confirm flow; for point-in-time recovery beyond that, use a suitable Home Assistant Backup.

Every safety guarantee above is backed by a file and a test — the **[safety page](docs/reference/safety.md)** maps each claim to what enforces and verifies it.

---

## ⚖️ Why not an MCP server?

Fair question — we built one first. 88,000 lines of it, never shipped. What it taught us is exactly what HA NOVA does differently:

<p align="center">
  <img src="assets/skills-vs-tools.png" alt="Left: a tool server lists its whole catalog by default. Right: HA NOVA sends one markdown skill on demand, and the credential stays locked on the server.">
</p>

| | Tool-based MCP server | ✦ HA NOVA |
|:--|:--|:--|
| 📖 **Where the know-how lives** | In server code — changing the behavior means changing the server | In markdown files you can open, read, and edit yourself |
| 🛡️ **Safety model** | Configurable: read-only modes, per-tool toggles, approval policies you set up | Enforced for every change: preview first, deletes take a typed code, every write is verified — each guarantee backed by a test |
| 🏠 **Where it runs** | The leading project's recommended setup runs inside Home Assistant's own process | A deliberately dumb relay in its own process — a crash can't touch Home Assistant, and it holds no HA business logic |
| 🔑 **Connecting a device** | A shared secret (URL or token) — or OAuth where configured | A one-time six-digit code per device, each individually revocable from the NOVA page *(App installs; the standalone container keeps a server-side token)* |

> **The honest bit:** the MCP side is ahead on breadth today — more tools (add-on and HACS management, dashboard screenshots, ZHA inspection), more contributors, more stars. It's the older project. But the asymmetry matters: **our gaps close one markdown file at a time — turning a tool server's Python into plain text you can read would be a rewrite.**

Full detail, named, dated, honest in both directions: **[comparison page](docs/reference/comparison.md)**.

---

## 🧩 Skills

29 task skills plus the HA NOVA context skill — each one a markdown file you can read and edit.

- ✏️ **Build & change** — automations & scripts, helpers, scenes, dashboards, areas & labels, device control, YAML-only config
- 🔍 **Understand & debug** — config reading, 40+ rule audits, root-cause tracing, history & stats, entity discovery, system health
- 🧰 **Run & maintain** — backups, updates, recorder & statistics repair, energy, MQTT, InfluxDB history, integration setup
- 🏠 **Everyday** — media & speakers, notifications, cameras, voice assistants, to-dos, calendars
- 🛟 **Admin & safety net** — persons, zones & users (owner-guarded), blueprint fallback, onboarding & troubleshooting

<details>
<summary><strong>The full list — all 29 skills</strong></summary>

<br>

| Skill | What it does |
|:------|:-------------|
| ✏️ **write** | Build and change automations with full preview and safety checks |
| 📖 **read** | Inspect your automation and script configs as they really are |
| 🔍 **review** | Audit your setup for mistakes, conflicts, and reliability gaps |
| 🧱 **dashboard** | Edit dashboards, cards, and Lovelace resources safely |
| 🎬 **scene** | Create and manage scenes, including "save this room as a scene" |
| 🗂️ **organize** | Manage areas, floors, labels, and entity metadata |
| 🕒 **history** | Query history, logbook timelines, and long-term stats |
| 🏠 **health** | Summarize Home Assistant status, repairs, integrations, system health, and noisy entities |
| 📅 **calendar** | Read calendars and bounded event windows |
| ✅ **todo** | Manage to-do and shopping-list items, create Local To-do lists |
| 💾 **backup** | Check backup status, create backups — also as a safety net before risky changes |
| ⬆️ **updates** | See pending updates, read release notes, and install them with safety gates |
| ⚡ **energy** | Analyze consumption, solar, battery, and per-device costs; manage Energy dashboard sources |
| 🧹 **maintenance** | Repair statistics, purge recorder history, and clean up dead entities — with strict safety gates |
| 🎛️ **service-call** | Control lights, climate, covers, switches, and media players |
| 🔎 **entity-discovery** | Find entities by name, room, area, or label |
| 🧩 **helper** | Create and manage helpers: counters, timers, template sensors, and more |
| 🩺 **diagnose** | Root-cause a concrete failure: "why didn't my automation run?" |
| 🎵 **media** | Control media players, browse libraries, group speakers, send TTS announcements |
| 📣 **notify** | Send notifications to your phone, manage persistent notifications |
| 📷 **camera** | Look at the current camera frame, get stream URLs, record |
| 📡 **mqtt** | Listen to MQTT topics, inspect discovery, publish with guards |
| 🗣️ **assist** | Test what your voice assistant understands, manage pipelines and voice exposure |
| 👥 **admin** | Manage persons, zones, tags, and user accounts — with hard owner guards |
| 🔌 **integration-setup** | Add new integrations and finish reauthentication flows |
| 📝 **yaml-config** | Edit YAML-only configuration (template/REST/command-line sensors, packages, themes) via opt-in file access |
| 📊 **external-sources** | Query InfluxDB directly for history the recorder purged long ago |
| 🛡️ **fallback** | Safety net for blueprints and advanced ops |
| 🚀 **onboarding** | Diagnose setup issues and troubleshoot connections |

</details>

Want to add a new capability? → [CONTRIBUTING.md](CONTRIBUTING.md)

The skills declare the NOVA Relay version they need (see `version.json`) and warn at runtime when the installed relay app is older — update it in Home Assistant under **Settings > Apps > NOVA Relay**.

---

## 🖥️ Supported Clients

| Client | Type |
|:-------|:-----|
| [Claude Code](https://github.com/anthropics/claude-code) | Terminal |
| [Claude Desktop](https://claude.com/download) (Code tab) | Desktop app |
| [Codex CLI](https://github.com/openai/codex) | Terminal |
| [OpenCode](https://github.com/opencode-ai/opencode) | Terminal |
| [Google Antigravity](https://antigravity.google/) | Desktop app / Terminal |
| Hermes Agent (preview) | Terminal |

> Google Antigravity is the current Google client path. `ha-nova setup gemini` remains a legacy alias for existing Gemini-era installs.

> **Hermes is in preview.** The Linux desktop route (GNOME Keyring) is maintainer-validated; macOS and Windows-via-WSL2 are experimental, and native Windows isn't supported. Details: [.hermes/INSTALL.md](.hermes/INSTALL.md).

---

## 🔮 What's Next

HA NOVA is young — and that's the point. Here's where it's going:

- **Deeper reviews everywhere** — the full 40+ rule audit, expanding beyond automations
- **Community skills** — write a new workflow as a markdown file, share it with everyone
- **A guided Docker path in the wizard** — Container and Core setup without the manual steps

> If you've ever wanted to shape how AI works with Home Assistant, this is the stage where your input actually changes the product.

---

## 🤝 Contributing

This is the best time to get involved.

- **Write a skill** — add a new workflow in markdown, no server code needed
- **Test on your setup** — edge cases from real installs make everything better
- **Improve docs** — make HA NOVA clearer for the next person
- **Tackle an [open issue](https://github.com/markusleben/ha-nova/issues)** — especially if you want a workflow to become first-class

→ [CONTRIBUTING.md](CONTRIBUTING.md)

---

## 📖 The Story

I spent over a year building an 88K-line MCP server for Home Assistant. Kept adding features, never releasing. Others shipped theirs while mine sat on my machine.

Then I realized the architecture was the problem. Instead of burying HA knowledge in a huge server, I could keep the server lean and write the workflows as plain text — readable, editable, and much easier to grow with other people.

HA NOVA is what came out of that. **[Here's an early demo](https://youtu.be/ylak867RkzM)** from the MCP era.

---

## 📄 License

[MIT](LICENSE)

## 🙏 Acknowledgments

Safety-rule ideas inspired by [HALMark](https://github.com/nathan-curtis/HALMark) by Nathan Curtis. Automation patterns, helper guidance, and device-control patterns adapted from [homeassistant-ai/skills](https://github.com/homeassistant-ai/skills) by Sergey Kadentsev ([@sergeykad](https://github.com/sergeykad)) and Julien Lapointe ([@julienld](https://github.com/julienld)).
