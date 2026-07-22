# Opt-in Census Spec

Status: approved — AMENDED to Variant B (no identifier) after legal/UX/
feasibility review (maintainer decisions 2026-07-22: Cloudflare Worker
hosting via a SQLite Durable Object counter, name `census`, public stats via
Worker GET /stats + README link; ping rides ALL check-update paths
output-clean; skill-mediated ask allowed with a 3-emission cap)
Date: 2026-07-22
Trigger: download-count analysis (scripts/dev/release-download-stats.sh
--estimate) hits its structural ceiling — wide uncertainty bands, and
installed-but-not-updating users are invisible by design. The maintainer wants
honest, voluntary counting with a clear explanation of why.

## Principles (the Home Assistant model — accepted in this exact community)

1. **Off by default, forever.** Nothing is ever sent without an explicit yes.
2. **Asked exactly once, never nagged.** One interactive question per install;
   any non-yes answer is final until the user changes it themselves.
3. **Tiny, documented payload — and NO identifier.** Exactly: HA NOVA
   version, relay version (only when recently observed, else omitted), OS.
   No UUID, no ID of any kind — the server stores only aggregate counters,
   so recognizing an install twice is impossible by construction. No IP
   retention, no HA data, no usage events, no client timestamps.
4. **Public numbers.** Everyone sees the same aggregates the maintainer sees.
5. **Readable code.** The client ping and the receiving endpoint live in the
   repo; the payload is contract-tested so it cannot grow silently.
6. **Real opt-out.** `off` (or `HA_NOVA_NO_CENSUS=1`) stops pings
   immediately. There is nothing to delete server-side because no per-install
   record exists — only counters.

## Why-text (shown at the ask, localized at runtime; EN source)

> **One-time question: may HA NOVA count your install?**
> HA NOVA has no telemetry — by design, and that stays. The flip side: we have
> no idea how many people use it or on which OS, which makes it hard to decide
> what to build and test first.
> If you opt in, HA NOVA sends a tiny anonymous ping, at most once a week:
> your HA NOVA version, your relay version, and your operating system.
> There is no ID — we could not recognize you twice even if we wanted to.
> Nothing about your home, your entities, or your usage — ever. The full
> payload is documented, the code is readable, and the resulting numbers are
> public for everyone at the same URL.
> You can change your mind anytime: `ha-nova census on|off|status`.
> Count this install? [y/N]

## Mechanics

- **Client:** new `ha-nova census on|off|status` command; state (asked,
  answer, enabled, last_ping_week — NO uuid) in a small local file next to
  config.json (per-install, not per-server-profile). Ping on opt-in, then
  piggybacked at most once per ISO week (UTC label gate, stamped before the
  send) on ALL `check-update` paths including `--quiet --json` — never on
  relay hot paths, never blocking (fire-and-forget, 3s dedicated timeout),
  never changing a single output byte (`--json` stays byte-clean, testpinned).
- **The ask:** first interactive occasion after the feature ships — end of
  `ha-nova setup`, end of `ha-nova update`, or `doctor` — TTY only, once
  (asked-flag stamped before the prompt), skippable with Enter (default No).
  Skill-only sessions get a `CENSUS ASK PENDING` callout on the check-update
  human output, hard-capped at three emissions ever (the third closes the
  question with answer=none).
- **Server:** smallest possible receiver — a Cloudflare Worker with one
  SQLite Durable Object of aggregate counter rows
  (iso_week, version, os, relay) → count. No uuid, no IPs, no TTL semantics
  needed (nothing per-install exists). Public `GET /stats` returns aggregates
  only (weekly series, by_os/by_version/by_relay with an `unknown` relay
  bucket, monthly lower bound). Endpoint code in the repo.
- **Claims/CI updates (same PR):** README "no telemetry" section gains the
  census sentence (off by default, payload documented, numbers public);
  `docs/reference/safety.md` gets a guarantee row (payload contract-tested);
  `scripts/check-docs.sh` check [11] is adjusted to allow exactly the census
  module and nothing else. `docs/reference/comparison.md` if it cites
  no-telemetry.
- **Existing users:** the ask reaches them via the same first-interactive-
  occasion rule after their next update. Release notes carry the announcement
  under `New Features` with the full why-text linked.

## Decisions (2026-07-22)

1. **Hosting:** Cloudflare Worker (free tier; code in repo under
   `census-worker/`; one SQLite Durable Object of aggregate counters — KV
   rejected as non-atomic, Analytics Engine rejected for retention/token
   reasons).
2. **Naming:** `census` — `ha-nova census on|off|status`.
3. **Public numbers:** Worker `GET /stats` (aggregate JSON, public) linked
   from the README census sentence.
4. Why-text approved as drafted.
5. **Rollout order:** (a) worker + client + claims/tests land in the repo with
   the endpoint URL as a single constant; (b) worker deploys to the
   maintainer's Cloudflare account; (c) the release that ships the client
   feature announces it in the notes with the why-text.

## Non-goals

- No usage/feature analytics, no error reporting, no per-request pings, no
  third-party analytics SDKs, no cookies/fingerprinting, no linkage to
  client_install_id or pairing identities.

## Verification (when implemented)

- Contract tests: payload byte-shape, opt-out deletes local state, no ping
  without enabled=true, no ping in --json/non-TTY/relay paths, ask happens
  at most once.
- check-docs still fails on any OTHER network/analytics addition.
- Live: opt in on a test install, see the aggregate move; opt out, see TTL
  expiry semantics documented.
