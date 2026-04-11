# README Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite README.md from a technical document into a compelling, Apple-like product page that converts visitors into users.

**Architecture:** Single-file rewrite of `README.md` plus moving "Project Structure" into `CONTRIBUTING.md`. No code changes, no new files beyond these two edits.

**Tech Stack:** Markdown, GitHub-flavored Markdown (HTML center tags, details/summary, blockquotes, tables)

**Spec:** `docs/work/2026-04-10-readme-rewrite-spec.md`

---

### Task 1: Add Project Structure to CONTRIBUTING.md

**Files:**
- Modify: `CONTRIBUTING.md` (add section before "Security" at bottom)

- [ ] **Step 1: Add Project Structure section to CONTRIBUTING.md**

Insert this section before the existing `## Security` section (line 125) in `CONTRIBUTING.md`:

```markdown
## Project Structure

```
cli/         Local command-line app (setup, doctor, update, uninstall, relay)
nova/        Relay app (runs on your HA server)
skills/      AI skills (markdown files)
scripts/     Setup, deploy, diagnostics
tests/       Test suite
```
```

- [ ] **Step 2: Verify CONTRIBUTING.md renders correctly**

Run: `head -5 CONTRIBUTING.md && echo "..." && tail -15 CONTRIBUTING.md`
Expected: Project Structure section visible near the bottom, followed by Security.

- [ ] **Step 3: Commit**

```bash
git add CONTRIBUTING.md
git commit -m "docs: move project structure from README to CONTRIBUTING

Part of README rewrite — project structure is contributor info,
not user-facing product info.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Write the new README.md

**Files:**
- Modify: `README.md` (full rewrite)

**Reference:** Read the spec at `docs/work/2026-04-10-readme-rewrite-spec.md` for design decisions and rationale.

- [ ] **Step 1: Write the complete new README.md**

Replace the entire contents of `README.md` with the following:

```markdown
<p align="center">
  <a href="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml"><img src="https://github.com/markusleben/ha-nova/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/markusleben/ha-nova/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux*-lightgrey" alt="macOS | Windows | Linux*">
</p>

<h2 align="center">AI that checks its work before touching your Home Assistant</h2>

<p align="center">
  Preview every change. Catch mistakes automatically.<br>
  Your token never leaves the server.
</p>

<p align="center">
  Works with <b>Claude Code</b> · <b>Claude Desktop</b> · <b>Codex CLI</b> · <b>OpenCode</b> · <b>Gemini CLI</b>
</p>

<p align="center">
  <img src="assets/demo.webp" alt="HA NOVA demo: creating a smart automation from plain English">
</p>

> *"When I get home, set my main lights to a warm welcome scene"* — one sentence in, a fully reviewed automation out, including suggestions you might not have thought of.

<p align="center">
  <b><a href="#-what-you-can-do">What it does</a></b> · <b><a href="#%EF%B8%8F-safe-by-design">Is it safe?</a></b> · <b><a href="#-get-started">Get started</a></b> · <b><a href="#%EF%B8%8F-how-it-works">How it works</a></b>
</p>

> **Early access.** The core works, and this is a good time to help shape the product. Back up your configs before letting AI touch anything. Hit a problem? [Open an issue](https://github.com/markusleben/ha-nova/issues).

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

> Automations and scripts get the full treatment today: preview, review, and a 40+ rule audit. Helpers, dashboards, and device control get preview-and-verify — deeper audits are rolling out with every release.

---

## 🛡️ Safe by Design

We built this because we didn't trust AI with our own config either.

**What happens when AI wants to change something:**

1. **Researches first.** Finds your devices, checks your setup, resolves entity names. No guessing.
2. **Shows you the change.** Full diff, before anything is written.
3. **You approve it.** Deletes require a specific confirmation code — not just "yes."
4. **Writes and verifies.** Reads the config back to confirm the change stuck.
5. **Audits itself.** Checks for mistakes, conflicts, and reliability issues.

**The ground rules — always:**

- 🔒 Your HA token stays on your server. The AI never sees it.
- 🔑 Credentials live in your OS keychain, not config files.
- 📖 Every rule the AI follows is a markdown file you can read.
- 🏠 Nothing leaves your network. No cloud relay, no telemetry.

---

## 🚀 Get Started

One installer. One wizard. Done. It even finds your Home Assistant automatically.

> **You need:** Home Assistant OS or Supervised.

1. Open the [latest release](https://github.com/markusleben/ha-nova/releases/latest)
2. Copy the one-liner for your OS
3. Run it — the wizard discovers your HA, sets up the connection, and configures your AI client

Once it finishes, try: *"Show me all my automations."*

<details>
<summary><strong>Platform notes</strong></summary>

<br>

**macOS** — fully tested on real hardware.

**Linux** — same installer, passes automated checks. Real-machine testing is ongoing.

**Windows** — ships x64 builds (ARM64 works via x64 emulation). Claude Code is the most tested client on Windows today; Gemini CLI has basic validation; Codex and OpenCode are still early. Native client prerequisites apply: Claude Code needs Git for Windows / Git Bash, Gemini CLI needs Node.js. See [.claude/INSTALL.md](.claude/INSTALL.md) and [.gemini/INSTALL.md](.gemini/INSTALL.md).

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
| `ha-nova doctor` | Diagnose connection and config problems |
| `ha-nova uninstall` | Remove HA NOVA (keeps settings for easy reinstall) |
| `ha-nova uninstall --purge` | Full cleanup including saved settings |

**Coming from a pre-Go install?**
- macOS / Linux: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh | bash`
- Windows: `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1 | iex`

</details>

> **Not a terminal person?** Claude Desktop gives you the same skills in a regular app window — no command line needed.

---

## ⚙️ How It Works

<p align="center">
  <img src="assets/how-it-works.png" alt="How HA NOVA works: Your AI Client talks to the NOVA Relay on your HA server, which connects to Home Assistant. Skills teach the AI what to do.">
</p>

**Skills** live on your machine as plain markdown. They're the AI's playbook — what to check, what to show you first, what to verify after a change. You can open them, read them, even edit them.

**The Relay** lives on your Home Assistant server. It's the only part that talks to Home Assistant directly, and it keeps your access token there — never on your laptop, never in a prompt.

When HA NOVA learns a new workflow, that's a text file update on your machine. The Relay doesn't change. No rebuild, no restart, no risk to a running system.

And because the Relay sits right next to Home Assistant, it can do things a remote client can't — like automatic backups of every automation change before it happens. That's coming soon: a safety net Home Assistant doesn't offer natively.

---

## 🔮 What's Next

HA NOVA is early — and that's the point. Here's where it's going:

- **Automation backups** — snapshot every automation before a change, so you can always roll back
- **Deeper reviews everywhere** — the full 40+ rule audit, expanding beyond automations
- **Community skills** — write a new workflow as a markdown file, share it with everyone
- **Broader platform testing** — real-machine coverage for Linux and Windows ARM

> If you've ever wanted to shape how AI works with Home Assistant, this is the stage where your input actually changes the product.

---

## 🧩 Skills

11 skills, each a markdown file you can read and edit.

| Skill | What it does |
|:------|:-------------|
| ✏️ **write** | Build and change automations with full preview and safety checks |
| 📖 **read** | Inspect configs and debug why an automation didn't fire |
| 🔍 **review** | Audit your setup for mistakes, conflicts, and reliability gaps |
| 🧱 **dashboard** | Edit dashboards, cards, and Lovelace resources safely |
| 🗂️ **organize** | Manage areas, floors, labels, and entity metadata |
| 🕒 **history** | Query history, logbook timelines, and long-term stats |
| 🎛️ **service-call** | Control lights, climate, covers, switches, and media players |
| 🔎 **entity-discovery** | Find entities by name, room, area, or label |
| 🧩 **helper** | Create and manage helpers: counters, timers, input booleans, more |
| 🛡️ **fallback** | Safety net for blueprints, zones, energy, and advanced ops |
| 🚀 **onboarding** | Diagnose setup issues and troubleshoot connections |

Want to add a new capability? → [CONTRIBUTING.md](CONTRIBUTING.md)

---

## 🖥️ Supported Clients

| Client | Type |
|:-------|:-----|
| [Claude Code](https://github.com/anthropics/claude-code) | Terminal |
| [Claude Desktop](https://claude.com/download) (Code tab) | Desktop app |
| [Codex CLI](https://github.com/openai/codex) | Terminal |
| [OpenCode](https://github.com/nicepkg/OpenCode) | Terminal |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | Terminal |

---

## 🤝 Contributing

HA NOVA is early. This is the best time to help shape it.

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
```

- [ ] **Step 2: Verify line count is within target**

Run: `wc -l README.md`
Expected: 140–170 lines (down from 229).

- [ ] **Step 3: Verify all internal links resolve**

Run: `for f in CONTRIBUTING.md LICENSE .claude/INSTALL.md .gemini/INSTALL.md; do test -f "$f" && echo "OK: $f" || echo "MISSING: $f"; done`
Expected: All OK.

- [ ] **Step 4: Verify asset files exist**

Run: `ls -la assets/demo.webp assets/how-it-works.png`
Expected: Both files exist.

---

### Task 3: Run CI verification

**Files:** None (read-only verification)

- [ ] **Step 1: Run docs verification**

Run: `npm run verify:docs`
Expected: All documentation claims verified. Key checks:
- Skill directory count = 12 (we list 11 in table, ha-nova context skill is the 12th dir)
- No MCP patterns in src/
- No domain-handler patterns in src/
- No telemetry patterns in src/
- Internal links (CONTRIBUTING.md, LICENSE) resolve
- install.sh exists
- Supported client install scripts exist

- [ ] **Step 2: If verify:docs fails, review the failure**

The check-docs.sh script validates README claims against the codebase. If any check fails:
- Check [1] (LOC): We no longer mention LOC — check still validates the range 1000–2000. Should pass.
- Check [2] (Skill count): Expects 12 directories. We have 12. Should pass.
- Check [6] (Internal links): We link CONTRIBUTING.md and LICENSE. Both exist. Should pass.

If a check fails unexpectedly, read the error and fix the README claim, not the check script.

- [ ] **Step 3: Verify GitHub anchor links**

The navigation line uses anchor links. Verify they match the actual section headings:
- `#-what-you-can-do` → `## 💬 What You Can Do`
- `#️-safe-by-design` → `## 🛡️ Safe by Design`
- `#-get-started` → `## 🚀 Get Started`
- `#️-how-it-works` → `## ⚙️ How It Works`

GitHub generates anchors by lowercasing, replacing spaces with hyphens, and stripping most special characters. Emoji handling varies. Test by pushing and checking the rendered page, or use the URL-encoded anchors in the hero section.

---

### Task 4: Commit the README

**Files:**
- Modified: `README.md`

- [ ] **Step 1: Review the diff**

Run: `git diff README.md | head -200`
Expected: Full rewrite — old sections removed, new sections in place. Verify:
- No "What is HA NOVA?" section
- No "Why People Use It" section
- No "Why Not Just Use a Generic MCP Server" section
- No "Why The Relay + Skills Split Matters" section
- No "Project Structure" section
- No Node.js badge
- New sections present: "Safe by Design", "What's Next", navigation line

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: rewrite README as product page with Apple-like flow

Restructure from technical documentation to conversion-focused
product page. Safety before install, capabilities table first,
architecture brief, roadmap for early adopters.

Key changes:
- New tagline: 'AI that checks its work before touching your HA'
- Safety section moved before Quick Start
- MCP comparison removed (own strengths, not competitor comparison)
- Platform notes collapsed into <details>
- 3 new skills (dashboard, organize, history) in capability table
- What's Next roadmap section with backup feature teaser
- Project Structure moved to CONTRIBUTING.md

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Final visual verification

**Files:** None (review only)

- [ ] **Step 1: Check rendered README on GitHub**

After push (if requested), verify on GitHub:
1. Demo GIF loads and plays
2. Architecture diagram loads
3. Navigation anchor links work (click each one)
4. `<details>` sections expand/collapse correctly
5. Tables render with correct alignment
6. Emoji headings display properly
7. Blockquotes render as callout boxes
8. Horizontal rules create visual separation
9. All external links work (client repos, YouTube, release page)

- [ ] **Step 2: Mobile check**

GitHub README on mobile is narrower. Verify:
1. Tables don't overflow horizontally
2. Demo GIF is visible (not clipped)
3. Navigation line wraps gracefully
