# Launch-post spine — "v0.19 — the simplicity release"

Status: superseded — v0.19.0 released; the v0.21 launch plan replaced this
spine

> Channel-neutral story skeleton for the v0.19.0 launch. One spine, adapted per
> channel (notes at the end). Written in the project's warm maker voice; facts
> match the v0.19 release state (`origin/main` + release-prep bump). Publish
> only after the release is out.

## Title candidates

- "HA NOVA v0.19 — the simplicity release: pair your AI with one six-digit code"
- "We replaced token setup with one six-digit code — and made it *more* secure"
- "Your AI + Home Assistant: one code to connect, every change checked"

## The hook

Setting up AI for your Home Assistant used to mean the token dance: create a
Long-Lived Access Token, copy a 180-character secret, paste it into a terminal,
hope you never leak it. As of v0.19, setup is: install, click **"Connect a
device"** on your new NOVA sidebar page, type the six-digit code it shows you.
Done.

## The pain (why we killed the token flow)

We watched a real first-time user hit the old flow and bounce. Tokens are
scary, fiddly, and wrong for this job: one long-lived secret with full access,
shared by every machine, revocable only all-at-once. The thing everyone
copies around is exactly the thing that should never leave the server.

## The shift (what setup is now)

1. Run one installer command — the wizard finds your Home Assistant and sets
   up the NOVA Relay App.
2. Click **"Connect a device"** — the wizard opens your NOVA page for you.
3. Type the six-digit code. Your AI is paired.

No token to create. No secret to keep. If you're not a terminal person: that
one command is the whole terminal part.

## The twist (simpler AND more secure — at the same time)

The six-digit code isn't a password you keep — it's one-time, expires in
minutes, and burns up after a single use. What it creates is stronger than
what tokens gave you:

- **Per-device credentials** — every computer gets its own, none of them ever
  sees a Home Assistant token (the App uses Home Assistant's own Supervisor
  access upstream).
- **Individually revocable** — lost laptop? Revoke that one device from the
  NOVA page; everything else keeps working.
- **For the skeptics:** OPAQUE (RFC 9807) for the pairing handshake,
  SPKI-pinned TLS 1.3 for every call after it — no CA chain, no
  "trust anyway". The safety page maps every claim to the code and test that
  enforce it.

Simpler and more secure at the same time — that trade-off turned out to be no
trade-off at all.

## The obvious question: why not an MCP server?

We built one first — 88,000 lines of it, never shipped. The lesson: a tool
server keeps its know-how in server code, and the leading one's recommended
setup runs inside Home Assistant's own process. HA NOVA is the opposite bet:
the know-how is markdown you can read and edit, the relay is deliberately
dumb and runs in its own process, safety is enforced (preview, typed delete
codes, verify-after-write — each guarantee backed by a test), and every
device pairs with its own one-time six-digit code, individually revocable.
The full comparison — named, dated, honest in both directions, including
where the MCP side is ahead — lives in the repo
(`docs/reference/comparison.md`).

## What HA NOVA is (for first-time readers)

Home Assistant + your AI client (Claude Code, Claude Desktop, Codex CLI,
OpenCode, Google Antigravity, Hermes preview), connected through readable
markdown skills on your machine and a thin relay on your HA server. Every
change is previewed, approved, and verified before it touches your config;
deletes take a typed confirmation code; one word reverts the latest verified
update. No cloud relay, no telemetry. Every rule the AI follows is a markdown
file you can read.

## Also in v0.19

- Works headless: servers, containers, LXCs — no desktop keyring needed.
- Upgrades migrate automatically; legacy access keeps working until you
  revoke it from the NOVA page.

## Honest maturity note

Proven end-to-end on macOS, Linux, and Windows on real machines. Still young —
back up your configs before letting AI touch anything, and tell us what
breaks: the product is at the stage where your feedback actually shapes it.

## Links

- Repo: https://github.com/markusleben/ha-nova
- Safety guarantees, claim by claim: `docs/reference/safety.md`
- HA NOVA vs. MCP servers, honest in both directions: `docs/reference/comparison.md`

## Per-channel adaptation notes

- **r/homeassistant** — lead with the token pain (every HA user knows LLATs);
  keep crypto in one short "for the skeptics" line; screenshots/GIF of the
  NOVA page carry the post.
- **r/ClaudeCode** — lead with "your Claude can run your home now"; the
  six-digit pairing is the supporting act; mention skills-as-markdown (Claude
  users get it instantly).
- **r/selfhosted** — lead with no-cloud/no-telemetry + per-device revoke; the
  OPAQUE/pinned-TLS detail earns its full paragraph here.
- **HA community forum** — longer, support-friendly version of the spine;
  include the upgrade/migration paragraph prominently.
- **Show HN** — title format: "Show HN: HA NOVA – AI for Home Assistant that
  checks its work (pair with one code)". Lead technical and honest: the
  architecture law (relay stays dumb, skills stay smart), then make the
  "why not an MCP server?" beat the SECOND paragraph (HN asks it within
  minutes — answering it unprompted, with the 88K-line confession, sets the
  tone), then the pairing crypto and the safety-page link. Expect and welcome
  the hard questions.
- **YouTube (CanWeFixThat)** — the pairing flow is the demo moment: film
  install → code → first command live; the "simpler AND more secure" line is
  the thumbnail/hook.
