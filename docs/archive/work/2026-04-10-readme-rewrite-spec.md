# README Rewrite Spec

**Date:** 2026-04-10
**Goal:** Rewrite README.md to be more marketing-oriented, Apple-like, visually appealing, and honest about the project's current state — while making the Relay+Skills architecture advantage tangible for non-technical Home Assistant users.

## Context

The current README (~230 lines) is technically accurate but reads like documentation, not a product page. At 2 GitHub stars, the README is the primary conversion tool. Every line must earn its place.

### Research Performed

- 8 specialized agents analyzed: marketing psychology, conversion copywriting, UX writing, competitor positioning, README best practices, skill capabilities, unreleased features, undocumented features
- Key competitor: ha-mcp (2,177 stars) — has adopted skills concept, but requires token on client side
- Community sentiment: token security is fear #1, HA community rewards authenticity and punishes marketing-speak

### Key Insights

1. **Token isolation** is the #1 differentiator — no other project keeps the token on the HA server
2. **40+ quality checks** only apply to automations/scripts today — be honest about this
3. **3 new skills** (dashboard, organize, history) are on main but unreleased
4. **Several features** exist but aren't in the README (auto-discovery, self-update with rollback, doctor diagnostics)
5. **"Early" is a feature** — frame it as an opportunity, not an apology

## Design Decisions

### Approach: "Honest Builder"

Authentic, vision-forward, no corporate tone. The README sells AND shows the vision. Early adopters should feel they're at the start of something bigger.

### No MCP Comparison

At 2 stars vs 2,177, a comparison table would advertise the competitor. Instead, differentiate through own strengths (token isolation, safety flow, text-file updates). No mention of MCP in the README.

### Safety Before Installation

Mental flow: "What can it do?" → "Is it safe?" → "How do I get it?" — Safety section moves before Quick Start.

### Honest About Scope

The full 4-step safety flow (Research → Preview → Apply → Review with 40+ checks) currently covers automations and scripts. Helpers, dashboards, and device control get preview-and-verify. This is stated clearly, framed as "expanding with every release."

### Apple-Like Visual Style

- Short sentences, breathing room, visual hierarchy
- Horizontal rules between major sections
- Emoji headings (one per section, consistent)
- Tables over prose wherever possible
- `<details>` for platform notes and CLI commands
- Navigation line under the hero for quick jumps
- Blockquotes for pull-quotes and important callouts

## Section Specification

### 1. Hero

**Badges:** CI, License, Platform (drop Node badge — false prerequisite impression)

**Tagline (h2, centered):**
```
AI that checks its work before touching your Home Assistant
```

**Sub-tagline (centered):**
```
Preview every change. Catch mistakes automatically.
Your token never leaves the server.
```

**Client line (centered, bold names):**
```
Works with Claude Code · Claude Desktop · Codex CLI · OpenCode · Gemini CLI
```

Rationale: At 2 stars, the client line in the hero borrows credibility from 5 recognized AI platforms. This stays.

**Demo GIF:** `assets/demo.webp` with existing caption. No heading before it (saves 2 lines of vertical space).

**Navigation line (centered, after demo):**
```
What it does · Safe by design · Get started · How it works
```
Linked to section anchors for quick jumps.

**Early access note:** Moves BELOW the demo GIF. Show value first, then set expectations.

### 2. What You Can Do

**Heading:** `## 💬 What You Can Do`

**Intro (1 sentence):**
```
Say what you want. HA NOVA figures out the rest.
```

**Table (8 rows, differentiators first):**

| You say | What happens |
|---------|-------------|
| "Check my automations for problems" | Audits against 40+ rules — catches conflicts, dead triggers, and mistakes you'd never spot manually |
| "Why didn't my motion light trigger last night?" | Replays the actual trace and tells you exactly where it failed and why |
| "Turn on the porch light at sunset and off at 11 PM" | Builds, previews, and reviews the automation before it touches your config |
| "What happened to my living room temperature overnight?" | Pulls history and summarizes the timeline in plain language |
| "Add a weather card to my main dashboard" | Reads the dashboard, shows the change, writes only after you confirm |
| "Move all kitchen devices to the Kitchen area" | Reassigns entities and devices — shows what will change first |
| "Turn off the upstairs lights" | Turns them off, confirms the new state |
| "Show me all temperature sensors" | Finds entities by name, room, area, or label |

Row order rationale: Review/debug first (unique differentiators), create/manage middle (breadth), quick control last.

**Honesty blockquote (below table):**
```
> Automations and scripts get the full treatment today: preview, review,
> and a 40+ rule audit. Helpers, dashboards, and device control get
> preview-and-verify — deeper audits are rolling out with every release.
```

### 3. Safe by Design

**Heading:** `## 🛡️ Safe by Design`

**Emotional hook (1 line):**
```
We built this because we didn't trust AI with our own config either.
```

**Scenario walkthrough (numbered, bold leads):**

1. **Researches first.** Finds your devices, checks your setup, resolves entity names. No guessing.
2. **Shows you the change.** Full diff, before anything is written.
3. **You approve it.** Deletes require a specific confirmation code — not just "yes."
4. **Writes and verifies.** Reads the config back to confirm the change stuck.
5. **Audits itself.** Checks for mistakes, conflicts, and reliability issues.

**Ground rules (emoji bullets):**

- 🔒 Your HA token stays on your server. The AI never sees it.
- 🔑 Credentials live in your OS keychain, not config files.
- 📖 Every rule the AI follows is a markdown file you can read.
- 🏠 Nothing leaves your network. No cloud relay, no telemetry.

### 4. Get Started

**Heading:** `## 🚀 Get Started`

**Intro:**
```
One installer. One wizard. Done.
It even finds your Home Assistant automatically.
```

**Prerequisite:** Home Assistant OS or Supervised (blockquote).

**3 steps:**
1. Open the latest release (linked)
2. Copy the one-liner for your OS
3. Run it — the wizard discovers your HA, sets up the connection, and configures your AI client

**First command suggestion:**
```
Once it finishes, try: "Show me all my automations."
```

**Collapsed sections:**

`<details>` **Platform notes:**
- macOS: fully tested on real hardware
- Linux: same installer, passes automated checks, real-machine testing ongoing
- Windows: x64 builds (ARM64 via x64 emulation), Claude most tested, Gemini basic validation, Codex/OpenCode early
- Windows prereqs: Claude needs Git for Windows / Git Bash, Gemini needs Node.js
- Link to .claude/INSTALL.md and .gemini/INSTALL.md
- "Do not download tar.gz manually" warning

`<details>` **CLI commands & legacy uninstall:**
- Table: check-update, update, setup, doctor, uninstall, uninstall --purge
- Legacy uninstall one-liners (macOS/Linux curl, Windows irm)

### 5. How It Works

**Heading:** `## ⚙️ How It Works`

**Diagram:** `assets/how-it-works.png` (centered, width-constrained)

**3 short paragraphs:**

1. **Skills** = plain markdown on your machine. AI's playbook — what to check, preview, verify. You can open, read, edit them.

2. **Relay** = small server on your HA box. Only thing that talks to HA directly. Keeps your token there — never on your laptop, never in a prompt.

3. **The payoff:** New workflows = text file update. Relay doesn't change. No rebuild, no restart, no risk. And because the Relay sits next to HA, it can do things a remote client can't — like automatic backups of every automation change (coming soon).

### 6. What's Next

**Heading:** `## 🔮 What's Next`

**Opener:**
```
HA NOVA is early — and that's the point.
```

**Bullet list (4 items, exciting first):**
- **Automation backups** — snapshot every automation before a change, so you can always roll back
- **Deeper reviews everywhere** — the full 40+ rule audit, expanding beyond automations
- **Community skills** — write a workflow as a markdown file, share it with everyone
- **Broader platform testing** — real-machine coverage for Linux and Windows

**CTA (blockquote):**
```
> If you've ever wanted to shape how AI works with Home Assistant,
> this is the stage where your input actually changes the product.
```

### 7. Skills

**Heading:** `## 🧩 Skills`

**Intro (1 line):**
```
11 skills, each a markdown file you can read and edit.
```

**Table (11 rows, emoji + bold name + benefit-oriented description max 12 words):**

| Skill | What it does |
|-------|-------------|
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

**Link:** → CONTRIBUTING.md for adding new skills

### 8. Supported Clients

**Heading:** `## 🖥️ Supported Clients`

**Table (5 rows):**

| Client | Type |
|--------|------|
| Claude Code | Terminal |
| Claude Desktop (Code tab) | Desktop app |
| Codex CLI | Terminal |
| OpenCode | Terminal |
| Gemini CLI | Terminal |

Client names linked to their repos.

**Callout:**
```
> Not a terminal person? Claude Desktop gives you the same skills
> in a regular app window.
```

**Platform support notes:** NOT here (already in Quick Start `<details>`).

### 9. Contributing

**Heading:** `## 🤝 Contributing`

**Opener:** "HA NOVA is early. This is the best time to help shape it."

**4 bullets:**
- Write a skill — a new workflow in markdown, no server code needed
- Test on your setup — edge cases from real installs make everything better
- Improve docs — make HA NOVA clearer for the next person
- Tackle an open issue (linked)

**Link:** → CONTRIBUTING.md

### 10. The Story

**Heading:** `## 📖 The Story`

**2 short paragraphs:**
```
I spent over a year building an 88K-line MCP server for Home Assistant.
Kept adding features, never releasing. Others shipped theirs while mine
sat on my machine.

Then I realized the architecture was the problem. Instead of burying
HA knowledge in a huge server, I could keep the server lean and write
the workflows as plain text. HA NOVA is what came out of that.
```

**YouTube link:** "Here's an early demo from the MCP era." (linked)

### 11. License + Acknowledgments

**License:** MIT (linked)

**Acknowledgments (1 flowing sentence):**
Safety-rule ideas inspired by HALMark by Nathan Curtis. Automation patterns, helper guidance, and device-control patterns adapted from homeassistant-ai/skills by Sergey Kadentsev and Julien Lapointe.

## What Gets Removed

| Current section | Action |
|----------------|--------|
| "What is HA NOVA?" (8 lines) | Replaced by tagline + sub-tagline |
| "Why People Use It" (4 bullets) | Duplicates capability + safety sections |
| "Why Not Just Use a Generic MCP Server" (table + prose) | No MCP comparison at 2 stars |
| "Why The Relay + Skills Split Matters" (separate section) | Merged into "How It Works" |
| "Project Structure" (code block) | Moved to CONTRIBUTING.md |
| Platform notes inline (30+ lines) | Collapsed into `<details>` |
| Node.js badge | Removed (false prerequisite impression) |

## What Gets Added

| New element | Rationale |
|-------------|-----------|
| Navigation line under hero | Quick jumps for all personas |
| Auto-discovery mention in Quick Start | Existing feature, undocumented, compelling |
| "What's Next" roadmap section | Frames "early" as opportunity, teases backup feature |
| Horizontal rules between sections | Breathing room, Apple-like visual flow |
| Emoji ground rules in Safety | Scannability for the most important trust section |
| Early access note moved below GIF | Show value before setting expectations |
| Dashboard, organize, history in capability table | 3 new skills on main, unreleased but working |

## Constraints

- All text in English (international audience)
- No claims that aren't backed by current project state
- "40+ checks" only claimed for automations/scripts
- Future features (backups) clearly marked as "coming soon"
- No competitor names mentioned
- URLs must be verified against actual repo paths before implementation
- Existing asset files (demo.webp, how-it-works.png) referenced as-is

## Target Metrics

- Visible lines: ~140-150 (down from ~230)
- Sections: 12 (down from 18)
- Time to Quick Start: Screen 3 (down from Screen 7)
- Time to Safety: Screen 2 (down from Screen 13)
