# HA NOVA v0.21 launch rollout

Status: superseded — replaced by the v0.22 launch rollout draft

> Publication-ready source copy for `u/w0nk1` and `markusleben`. Verified
> against stable v0.21.0. Apply each channel's moderation gate before posting.
> One-code pairing, per-device credentials, and one-click revocation apply to
> HA OS/Supervised App installs. Container/Core uses the standalone Relay
> container: its Home Assistant token stays server-side, while clients share a
> Relay authentication token.
>
> For Reddit, attach `assets/pairing-flow.png`. It is an illustration with an
> example code, not a live screenshot. If a real NOVA-page screenshot is used
> instead, generate a fresh code immediately before capture and crop hostnames,
> device names, and other private Home Assistant data.

## 1. r/homeassistant

**Publication gate**

Current rule 4 explicitly allows Home Assistant-related personal projects, so
no moderator exception is required. Current rule 1 prohibits AI-generated
content. The maintainer must therefore write the final post in their own words;
use this section only as a fact-checked source and do not submit it verbatim or
automatically. No required project/self-promotion flair is documented.

**Title**

HA NOVA for HA OS/Supervised: pair your AI using one six-digit code — every change checked

**Body**

I build and maintain HA NOVA, a free MIT-licensed project that lets AI clients such as Claude Code, Claude Desktop, Codex CLI, OpenCode, Google Antigravity, and Hermes Agent work on Home Assistant through readable markdown skills.

The biggest problem was not the AI. It was setup: create a long-lived access token, copy a large secret into a terminal, and hope it never leaks. I watched a first-time user stop at exactly that step.

On HA OS or Supervised, that flow is gone. Run the installer, click **Connect a device** on the NOVA page in the Home Assistant sidebar, and type the six-digit code. The code is single-use and expires after 10 minutes. The App uses Home Assistant's Supervisor access, so there is no Home Assistant token to create or copy. OPAQUE (RFC 9807) protects the pairing handshake; subsequent device calls use SPKI-pinned TLS 1.3. Each computer gets its own credential and can be revoked individually from the NOVA page.

Once connected, HA NOVA can build and audit automations against 40+ rules, diagnose why an automation failed, edit dashboards and scenes, manage helpers, inspect history and energy data, work with MQTT and voice, and more. The 29 task skills are plain markdown files you can open and edit.

The safety flow is preview → approval → write → read back and verify. Deletes require a typed confirmation code; “yes” is not accepted. Supported non-destructive multi-step tasks can show every operation first and take one confirmation for the full set. They then run sequentially with an explicit result for each operation — not as an atomic transaction. Requirements you state during the conversation remain active; a later plan that conflicts with one is blocked and explained.

It also supports named profiles for multiple Home Assistant servers on one machine, with isolated credentials per server. Update notices show the important release changes and any required action instead of only two version numbers.

v0.21 adds an optional public census because, without behavioral analytics, I otherwise cannot tell which operating systems and versions need attention first. It is off until you explicitly opt in. Its application JSON body contains only the payload schema, HA NOVA version, operating system, and a recently observed Relay version when available — no installation/device/user ID and no Home Assistant data. Cloudflare is the hosting provider for the census endpoint and processes source-IP and connection metadata for HTTPS; HA NOVA Worker code does not read the IP, and HA NOVA application storage/public statistics do not store it. The published totals are directional aggregate ping counts. You can inspect the exact payload and change the choice anytime with `ha-nova census status|on|off`.

The honest bits: this is still a young project, so back up your config before letting any AI change it. Container/Core installs use the same Relay code in a standalone container. Its Home Assistant token stays server-side, but clients share one Relay authentication token instead of using App pairing. The repo also includes a named, dated comparison with MCP-server approaches, including where the MCP side is ahead.

Repo: https://github.com/markusleben/ha-nova

Latest release: https://github.com/markusleben/ha-nova/releases/latest

Public census details: https://github.com/markusleben/ha-nova/blob/main/docs/reference/census.md

Feedback is very welcome. Multi-server support exists because one user asked for it; real launch feedback will decide what gets built next.

**Attachment**

`assets/pairing-flow.png`

## 2. Home Assistant Community Forum

Reply to the existing **Share your Projects!** topic; do not create a duplicate:

https://community.home-assistant.io/t/why-i-replaced-my-home-assistant-mcp-server-with-a-dumb-relay-and-smart-ai-skills-ha-nova/994607

**Reply**

Quick project update: HA NOVA has changed substantially since the first post.

The old token-copy setup is gone on HA OS/Supervised. The installer now opens the NOVA page in the Home Assistant sidebar; click **Connect a device**, type the one-time six-digit code, and the computer gets its own individually revocable credential. The App uses Supervisor access upstream, so there is no Home Assistant token to create or store on the client.

HA NOVA now works end-to-end on macOS, Linux, and Windows and includes 29 readable task skills. It can build and review automations, diagnose failed runs, edit dashboards and scenes, manage helpers, inspect history and energy, work with MQTT and Assist, and more. Every configuration change is previewed before approval and verified by reading it back afterward. Deletes require a typed confirmation code.

Recent additions include named profiles for multiple Home Assistant servers, one confirmation for a fully previewed non-destructive multi-step task, and conversation-scoped decision memory that blocks later plans which contradict requirements you already stated.

v0.21 also starts an optional public census. It is off until explicit opt-in and exists because, without behavioral analytics, I cannot tell which operating systems and versions need attention first. Its application JSON body contains only the payload schema, HA NOVA version, operating system, and a recently observed Relay version when available — no installation/device/user ID and nothing about your home. Cloudflare is the hosting provider for the census endpoint and processes source-IP and connection metadata for HTTPS; HA NOVA Worker code does not read the IP, and HA NOVA application storage/public statistics do not store it. The public totals are directional accepted-ping counts, not verified unique installs.

If you already use the current Go-based HA NOVA: run `ha-nova update`, update **NOVA Relay** under **Settings → Apps**, then start a new AI session. Container/Core users need to pull and recreate the Relay container instead. Pre-Go installs must use the legacy cleanup path in the README before running the current installer.

Repo: https://github.com/markusleben/ha-nova

Latest release: https://github.com/markusleben/ha-nova/releases/latest

Census details: https://github.com/markusleben/ha-nova/blob/main/docs/reference/census.md

The feedback I would value most:

- Is the multi-server CLI comfortable enough, or should a guided wizard be next?
- Are update highlights useful or too chatty?
- Which configuration family should receive the deeper 40+ rule audit after automations?

For problems, GitHub issues are easiest to track. `ha-nova doctor` produces the diagnostics that usually make a report actionable.

## 3. r/ClaudeCode

**Flair:** `Showcase`

**Title**

I made readable Home Assistant skills for Claude Code — every change previewed, approved, and checked

**Body**

I build and maintain HA NOVA, a free MIT-licensed project that connects Claude Code and Claude Desktop to Home Assistant through 29 plain markdown task skills and a deliberately thin Relay. There is no Home Assistant MCP server in the middle. If you want to read and diff the rules your agent follows, every workflow is a `SKILL.md`.

The safety model is the part Claude users may find interesting: configuration writes follow preview → approval → write → read back and verify. Deletes require an exact typed confirmation code. Supported non-destructive multi-step changes show the full set and ask once, then execute sequentially with a per-operation ledger. Requirements stated earlier in the conversation stay active and block contradictory later plans.

On HA OS/Supervised, setup is one installer command followed by a six-digit code from the Home Assistant sidebar. The code is one-time with a 10-minute TTL. OPAQUE protects the pairing handshake; subsequent device calls use pinned TLS. Each computer receives its own revocable credential. Container/Core uses the same Relay code as a standalone container: its Home Assistant token stays server-side, while clients share one Relay authentication token.

It also supports several Home Assistant instances on one machine through named profiles and `HA_NOVA_SERVER=<name>`.

v0.21 adds a strictly opt-in public census with an identifier-free application JSON body so platform and version priorities can be based on a directional signal instead of guesswork. It is off by default and includes no Home Assistant data. Cloudflare is the hosting provider for the census endpoint and processes source-IP and connection metadata for HTTPS; HA NOVA Worker code does not read the IP, and HA NOVA application storage/public statistics do not store it.

Disclosure: I am the creator and maintainer. HA NOVA has no paid tier or referral links; the AI client you use may have its own subscription costs.

Repo: https://github.com/markusleben/ha-nova

Latest release: https://github.com/markusleben/ha-nova/releases/latest

## 4. r/selfhosted

**Flair:** `Release (AI)`

Before posting, verify that the account's recent participation is not dominated
by HA NOVA promotion. The subreddit allows released, documented self-hostable
projects but applies its anti-spam/self-promotion rule discretionarily.

**Title**

HA NOVA — a local Relay and readable AI skills for Home Assistant, with per-device revocation on App installs

**Body**

I build and maintain HA NOVA, a free MIT-licensed project. It keeps Home Assistant traffic local: markdown skills run with the AI client on your machine, and a thin Relay runs beside Home Assistant. There is no cloud relay and no behavioral or feature-use analytics. Normal update checks contact GitHub.

For HA OS/Supervised App installs, pairing uses OPAQUE (RFC 9807): the six-digit code is not transmitted in plaintext, is single-use, and has a 10-minute TTL. Calls after pairing use TLS 1.3 pinned to the Relay's exact SPKI. Every computer receives its own credential and can be revoked individually from the Home Assistant sidebar. Keyring-less headless Unix clients use a private `0600` credential file. Container/Core runs the same Relay code as a standalone container and keeps the Home Assistant token server-side; clients authenticate to that Relay with one shared Relay token.

The client contains 29 readable markdown task skills. Configuration writes are previewed, explicitly approved, and verified by reading them back. Deletes require a typed confirmation code. The Relay contains transport and host capabilities, not Home Assistant business logic.

v0.21 adds an optional public census to reveal which operating systems and versions need attention. It is off until explicit opt-in. The application JSON body is limited to the payload schema, HA NOVA version, operating system, and a recently observed Relay version when available — no installation/device/user ID and nothing about the home. Cloudflare is the hosting provider for the census endpoint and processes source-IP and connection metadata for HTTPS; HA NOVA Worker code does not read the IP, and HA NOVA application storage/public statistics do not store it. The receiver stores aggregate counters, and the public totals are directional rather than verified unique installs.

Repo and install docs: https://github.com/markusleben/ha-nova

Latest release: https://github.com/markusleben/ha-nova/releases/latest

Privacy/census contract: https://github.com/markusleben/ha-nova/blob/main/docs/reference/census.md

## 5. Show HN

Current HN policy temporarily restricts Show HN submissions to established
participants. Verify that the posting account is eligible before submitting.
Do not create a new account solely for this launch.

Policy: https://news.ycombinator.com/showlim

HN also prohibits generated or AI-edited comments. The maintainer must write
the submission title and first comment in their own words. Use the material
below only as a fact-checked source; do not submit it verbatim or
automatically.

Generated-content policy: https://news.ycombinator.com/newsguidelines.html#generated

Submit the repository URL. Title candidate for the maintainer's rewrite:

**Title**

Show HN: HA NOVA – AI for Home Assistant that checks its work

**URL**

https://github.com/markusleben/ha-nova

Fact-checked source for the maintainer's first comment:

**First-comment facts**

I built HA NOVA to let AI clients operate Home Assistant through plain markdown skills. The architecture rule is “the Relay stays dumb, the skills stay smart”: the server-side component is a thin proxy with no Home Assistant domain logic, while the workflows and safety rules remain readable on the client.

The obvious question is why this is not an MCP server. I built one first — about 88,000 lines — and never shipped it. That experience pushed me toward a smaller server boundary, workflows users can read and edit without recompiling anything, and safety contracts that map to tests.

Configuration writes follow preview → explicit approval → write → read back and verify. Deletes require an exact typed confirmation code. Supported non-destructive multi-step changes bind one confirmation to the fully previewed set, execute sequentially, and report partial completion instead of claiming atomicity. User requirements stated earlier in the conversation remain active and block contradictory plans.

On HA OS/Supervised, pairing uses a one-time six-digit code with a 10-minute TTL and OPAQUE (RFC 9807); subsequent calls use SPKI-pinned TLS 1.3. Each computer gets an individually revocable credential. Container/Core uses the same Relay code as a standalone container: its Home Assistant token stays server-side, while clients share one Relay authentication token.

I build and maintain the project. It is free, MIT-licensed, and installable now on macOS, Linux, and Windows. It is still young, so I recommend a backup before allowing any AI to change Home Assistant. Hard questions about the trust model are welcome.

## Posting order and completion record

1. The maintainer writes the r/homeassistant post in their own words from the verified facts above.
2. Post it first, with `assets/pairing-flow.png`.
3. Reply to the existing Home Assistant Community topic within one day of the Reddit post.
4. Fold concrete feedback into the r/ClaudeCode and r/selfhosted variants, then post them.
5. Submit Show HN last, using a maintainer-written title and first comment, only from an eligible established account and when there is time to answer questions.

The `/releases/latest` links are intentional and remain correct across patch
releases.

Record each live URL here after posting:

- r/homeassistant:
- Home Assistant Community:
- r/ClaudeCode:
- r/selfhosted:
- Show HN:
