# Opt-in Census Spec

Status: Variant B implementation decision (no identifier), archived with the
v0.21 release prep. This is the decision record; `PRIVACY.md` and
`docs/reference/census.md` are the current behavior contract.
Date: 2026-07-22
Trigger: download-count analysis (scripts/dev/release-download-stats.sh
--estimate) hits its structural ceiling — wide uncertainty bands, and
installed-but-not-updating users are invisible by design. The maintainer wants
honest, voluntary counting with a clear explanation of why.

## Principles (the Home Assistant model — accepted in this exact community)

1. **Off by default, forever.** Nothing is ever sent without an explicit yes.
2. **Asked exactly once while local state remains intact, never nagged.** One interactive question per install;
   any non-yes answer is final until the user changes it themselves.
3. **Tiny, documented payload — and NO identifier.** Exactly: HA NOVA
   version, relay version (only when recently observed, else omitted), OS.
   No UUID, no ID of any kind — the server stores only aggregate counters,
   so recognizing an install twice is impossible by construction. No IP field
   or IP retention in HA NOVA application storage, no HA data, no usage events,
   no client timestamps.
4. **Public numbers.** Everyone can inspect the same current public aggregate
   view. No per-install view exists; retained aggregate rows are not presented
   as unique installs.
5. **Readable code.** The client ping and the receiving endpoint live in the
   repo; the payload is contract-tested so it cannot grow silently.
6. **Real opt-out.** After `off` returns successfully, no new ping can begin;
   it waits for one bounded request that already started. A non-empty
   `HA_NOVA_NO_CENSUS` suppresses asks and pings in every process launched with
   it. There is nothing to delete server-side because no per-install record
   exists — only counters.

## Why-text (shown at the ask, localized at runtime; EN source)

> **One-time question: may HA NOVA include this install in its public census?**
> HA NOVA sends no behavioral or feature-use analytics. The census exists because we otherwise
> cannot tell which operating systems and versions need attention first.
> If you opt in, HA NOVA records one tiny anonymous ping attempt per ISO week
> before sending; with local census state intact, it does not attempt again that week:
> your HA NOVA version, your relay version when recently observed, and your
> operating system. There is no ID, no IP in HA NOVA storage, and nothing about your home.
> Public totals are directional accepted-ping counts, not verified unique
> installs. The full payload and receiving code are public.
> You can change your mind anytime: `ha-nova census on|off|status`.
> Include this install? [y/N]

## Mechanics

- **Client:** new `ha-nova census on|off|status` command; state (`asked_at`,
  `asked_via`, answer, enabled, `last_ping_week`, skill-notice count, and a
  recently observed Relay version plus local observation time — NO uuid) in a
  small device-local file (`LOCALAPPDATA` on Windows; next to config.json
  elsewhere; per-install, not per-server-profile). Ping on opt-in, then
  piggybacked after one recorded attempt per ISO week (UTC label gate, stamped before the
  send) on ALL `check-update` paths including `--quiet --json` — never on
  relay hot paths; synchronous only after output with a dedicated 1.5-second
  timeout and no automatic retry; never changes a single output byte (`--json`
  stays byte-clean, test-pinned).
  Restoring, deleting, or rolling back local census state can permit another
  attempt; the receiver cannot deduplicate because it has no client identifier.
- **The ask:** first interactive occasion after the feature ships — end of
  `ha-nova setup`, end of `ha-nova update`, or `doctor` — TTY only, once
  (asked-flag stamped before the prompt), skippable with Enter (default No).
  Skill-only sessions get a `CENSUS ASK PENDING` callout on the check-update
  human output, hard-capped at three emissions while local census state
  remains intact (the third closes the question with answer=none).
- **Server:** smallest possible receiver — a Cloudflare Worker with one
  SQLite Durable Object of aggregate counter rows
  (iso_week, version, os, relay) → count. No uuid or IP fields; aggregate
  rows have no automatic expiry. Public `GET /stats` returns aggregates
  only (a sparse series within a 26-week horizon, 4-week
  by_os/by_version/by_relay with an `unknown`
  relay bucket, peak weekly pings). The receiver intentionally has no client
  authentication, so accepted pings are directional and may be duplicated or
  fabricated; they are not a verified install count. Endpoint code is in the
  repo.
- **Claims/CI updates:** census/reference/privacy/safety claims landed with the
  implementation; the stable README disclosure lands in the release-prep PR;
  `docs/reference/safety.md` gets a guarantee row (payload contract-tested);
  `scripts/check-docs.sh` check [11] rejects configured analytics-vendor
  patterns; check [12] confines known census symbols to the census module and
  pins its explicit opt-in guard. `docs/reference/comparison.md` is updated if
  it cites no-telemetry.
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
4. Why-text revised after adversarial accuracy review to state that public
   totals are directional accepted-ping counts, not verified unique installs.
5. **Rollout order:** (a) worker + client + claims/tests land in one reviewed
   release-prep commit; (b) the required RC runs against the pre-launch Worker;
   (c) that reviewed Worker deploys to the clean `public-v0.21` namespace and
   `/stats` is verified empty with the new schema; (d) the final release ships
   the client and announces the why-text.

## Non-goals

- No usage/feature analytics, no error reporting, no per-request pings, no
  third-party analytics SDKs, no cookies/fingerprinting, no linkage to
  client_install_id or pairing identities.

## Verification

- Contract tests: payload byte-shape; opt-out preserves the one-time answer and
  disables sends; no ping without enabled=true; opted-in `--json` remains
  byte-identical while carrying the ping; no relay-hot-path ping; ask happens
  at most once while local census state remains intact.
- check-docs rejects configured analytics-vendor patterns, confines known
  census symbols to `cli/census*.go`, and pins the explicit opt-in guard.
- Before final publish: deploy the reviewed Worker after the RC, then verify
  the clean stable namespace is empty and the public response exposes
  `peak_weekly_pings`, never `monthly_lower_bound`.
