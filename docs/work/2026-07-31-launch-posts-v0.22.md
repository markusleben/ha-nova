# HA NOVA v0.22 launch rollout

Status: active — live publication URLs are not yet recorded

> Publication-ready source copy for `u/w0nk1` and `markusleben`. Verified
> against stable v0.22.0. Apply each channel's moderation gate before posting.
> Home Assistant Cloud access is an optional desktop Beta for HA OS/Supervised
> installs and requires the user's own paid Nabu Casa subscription with
> Remote UI enabled; headless, SSH, WSL, service, gateway, Container, and Core
> setups stay local-only.
>
> For Reddit, attach `assets/cloud-fallback.png`. It is an illustration, not a
> live screenshot. If a real screenshot is used instead, crop hostnames,
> device names, private URLs, and any other personal Home Assistant data.

## 1. r/homeassistant

**Publication gate**

Re-verify the current subreddit rules immediately before posting. As of the
v0.21 launch: personal Home Assistant projects are explicitly allowed, but
AI-generated content is prohibited — the maintainer must write the final post
in their own words and use this section only as a fact-checked source, never
submitting it verbatim or automatically.

**Title**

HA NOVA update: your AI assistant now reaches Home Assistant away from home — through your own Nabu Casa, not my servers

**Body**

I build and maintain HA NOVA, a free MIT-licensed project that lets AI clients
such as Claude Code, Claude Desktop, Codex CLI, OpenCode, Google Antigravity,
and Hermes Agent work on Home Assistant through readable markdown skills —
with a safety workflow that previews, confirms, and verifies every change.

The new part: optional remote access as a desktop Beta. Until now everything
was strictly local. The setup wizard can now keep HA NOVA local, add Home
Assistant Cloud as a secure fallback, or go Cloud-only. Local stays the
recommended default and always stays preferred at runtime — the Cloud route is
an automatic fallback for when you are away, with no manual URL switching.

The part I care most about: HA NOVA still runs no tunnel, broker, or cloud of
its own. The remote route is your existing paid Home Assistant Cloud (Nabu
Casa) subscription with Remote UI. The wizard validates your Cloud URL, opens
Home Assistant's own OAuth, and stores the authorization in your operating
system's native credential store (macOS Keychain, Windows Credential Manager,
Linux Secret Service). Neither that authorization nor the Relay credential is
ever exposed to the AI, and each computer keeps its own separately revocable
device pairing.

Scope, honestly: the Beta targets desktop sessions on macOS, Windows, and
Linux with Home Assistant OS/Supervised. Headless, SSH, WSL, service, gateway,
Container, and Core setups stay local-only for now. Existing installs can add
it with `ha-nova cloud add` or by rerunning `ha-nova setup`.

Everything else stays as before: one installer command, one six-digit one-time
pairing code, previews before every write, typed confirmation codes for
deletes, a 40+ rule automation audit, and skills that are plain markdown files
you can open and edit. And as always: this is young software — back up your
config before letting any AI change it.

Repo: https://github.com/markusleben/ha-nova

Latest release: https://github.com/markusleben/ha-nova/releases/latest

How the fallback works (README section): https://github.com/markusleben/ha-nova#optional-remote-access-with-home-assistant-cloud-beta

Feedback is very welcome — especially from Nabu Casa users on setups I could
not cover. The Beta label comes off when real-world reports say it should.

**Attachment**

`assets/cloud-fallback.png`

## 2. Short version (X/Mastodon/Discord)

HA NOVA update: AI assistants (Claude Code, Codex, and friends) can now work
on your Home Assistant away from home — as an optional desktop Beta routed
through your own Nabu Casa Remote UI. No HA NOVA-operated tunnel or broker,
OAuth stays in your OS's native credential store, local access always stays
preferred. One installer, one six-digit code, every change previewed and
verified. https://github.com/markusleben/ha-nova
