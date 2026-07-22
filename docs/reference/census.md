# The HA NOVA Census

HA NOVA sends no behavioral or feature-use analytics. The census is one strictly opt-in measurement:
an anonymous, ID-free stream of small pings intended as a directional signal
about participating versions and operating systems. It cannot establish a
verified count of unique installs. This page is the detail reference behind
the one-time question; the plain-language privacy summary is
[PRIVACY.md](../../PRIVACY.md).

## The one-time question

This is the verbatim question the CLI shows (the closing prompt line is
rendered by the terminal prompt):

```
  One-time question

  May HA NOVA include this install in its public census?
  HA NOVA sends no behavioral or feature-use analytics. The census exists because we otherwise
  cannot tell which operating systems and versions need attention first.
  A yes permits one anonymous ping attempt per week while local census state remains intact:
      HA NOVA version  ·  relay version  ·  operating system
  No ID, no IP in HA NOVA storage, nothing about your home. Public totals are
  directional accepted-ping counts, not verified unique installs.

  Details: docs/reference/census.md   Change anytime: ha-nova census on|off
  Include this install? [y/N]:
```

While local census state remains intact, the interactive question is asked at
most once per install — on an
interactive terminal at the end of `ha-nova setup`, after `ha-nova update`,
after a clean `ha-nova doctor`, or on a plain `ha-nova check-update`. For
skill-only sessions the same open question surfaces as an AI-client callout at
most three times; after the third it closes itself permanently. Enter means
No. Any non-yes answer is final until you change it yourself. If you opt in,
the first eligible ping attempt starts immediately.

## The exact payload

```json
{"schema":1,"version":"0.21.0","relay":"0.7.1","os":"macos"}
```

- `schema` — payload format version, currently `1`.
- `version` — the installed HA NOVA version.
- `relay` — the relay version, included only when it was observed during your
  normal relay traffic within the last 7 days; omitted otherwise (the public
  stats show those pings in an `unknown` bucket). The census never makes its
  own relay call.
- `os` — `macos`, `linux`, or `windows`.

No ID, no IP field, no client timestamp, nothing else. `ha-nova census status`
prints the literal bytes that would be sent right now. Contract tests pin the
payload on both ends (`cli/census_test.go`,
`tests/census-worker/worker.test.ts`). `scripts/check-docs.sh` checks [11]/[12]
keep known census symbols inside `cli/census*.go`, pin the explicit opt-in
guard, and reject configured analytics-vendor patterns elsewhere.

## When it sends

The client records at most one attempt per ISO week (UTC) in its local state
before sending, piggybacked on commands that already touch
the network for the update check (`ha-nova check-update` and the background
refresh it spawns). Update checks run automatically as part of normal use —
AI clients trigger one at session start or before the first Home Assistant
task, depending on the client, and a background refresh keeps the release
cache warm — so an opted-in install normally attempts one ping each
week without you doing anything. Never on relay hot paths, output-safe by
construction: the synchronous send runs after all command output, bounded by
a dedicated 1.5-second timeout, with redirects rejected and no automatic retry
— and never changing a single byte of command output. The week is stamped locally before the send — a
failed carrier send loses that week's ping rather than risking a duplicate.

This is a local-state guarantee, not a server-side uniqueness guarantee.
Restoring, deleting, or rolling back `census.json` can permit another attempt in
the same week; the receiver has no identifier with which to deduplicate it.

Because there is no ID, a reinstall in the same week can ping twice. The public
receiver also has no client authentication: any schema-valid unauthenticated payload is
accepted, so duplicate or fabricated pings cannot be distinguished from the
released client. The aggregates are therefore a directional activity signal,
not a lower bound, exact total, or verified unique-install count. Two smaller
honesty notes: a ping sent seconds before Monday 00:00 UTC can land in the next
week's server bucket (the client's once-per-week invariant holds either way,
only the label may differ by one at the boundary); and platforms outside the
three documented buckets (macOS, Linux, Windows) are not counted rather than
being guessed.

## How it is counted

The receiving end is a small Cloudflare Worker in this repository
(`census-worker/`) backed by one SQLite Durable Object holding aggregate
counter rows `(iso_week, version, os, relay) -> count`. Request and invocation
logging are disabled; the application stores no IPs. Cloudflare still exposes
built-in aggregate Worker metrics (request counts/status/runtime) with up to
three months of metrics retention.

`GET /stats` (public: <https://ha-nova-census.markusleben.workers.dev/stats>)
shows the accepted-ping aggregates: a sparse weekly series within a 26-week
horizon, breakdowns by OS/version/relay over the last 4 weeks, and
`peak_weekly_pings` (the busiest week in that 4-week window). Counter rows are
aggregate-only, have no automatic expiry, and remain until operator deletion;
older rows age out of the public horizon. There is no per-install view for the
operator or the public. To bound storage abuse, new dimension combinations
beyond 256 rows in one week fold into one `(other, other, other)` row. That can
surface as an `other` key in each breakdown; it does not represent another
client OS.

## Controls

| Action | Command |
|---|---|
| Opt in (starts the first eligible ping attempt immediately) | `ha-nova census on` |
| Opt out (after success, no new ping can begin; waits for an in-flight request) | `ha-nova census off` |
| Inspect state and the exact bytes | `ha-nova census status` |
| Environment kill switch (suppresses ask and ping; ANY non-empty value counts as set) | `HA_NOVA_NO_CENSUS=1` |

`ha-nova uninstall` removes the local census state (`census.json`). It retains
one opaque random lifecycle marker outside the managed directories so stale
processes cannot restart census activity after uninstall. The marker carries
no consent, timestamp, process data, or stable device ID, is never sent, and a
later successful setup removes it. There is nothing to delete server-side
because no per-install record exists — only counters.

The local state records the one-time question/answer, enabled flag, last
attempted ISO week, AI-client notice count, and any recently observed Relay
version with its local observation time. None of those local timestamps or
control fields are sent.
