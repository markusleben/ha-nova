# The HA NOVA Census

HA NOVA has no telemetry — that stays. The census is the one, strictly opt-in
exception: an anonymous, ID-free count of installs. This page is the detail
reference behind the one-time question; the plain-language privacy summary is
[PRIVACY.md](../../PRIVACY.md).

## The one-time question

This is the verbatim question the CLI shows (the closing prompt line is
rendered by the terminal prompt):

```
  One-time question

  May HA NOVA count your install? HA NOVA has no telemetry — that stays.
  A yes sends one anonymous ping, at most once a week:
      HA NOVA version  ·  relay version  ·  operating system
  No ID, no IP stored, nothing about your home — and the resulting
  numbers are public for everyone.

  Details: docs/reference/census.md   Change anytime: ha-nova census on|off
  Count this install? [y/N]:
```

The interactive question is asked at most once per install — on an
interactive terminal at the end of `ha-nova setup`, after `ha-nova update`,
after a clean `ha-nova doctor`, or on a plain `ha-nova check-update`. For
skill-only sessions the same open question surfaces as an AI-client callout at
most three times; after the third it closes itself permanently. Enter means
No. Any non-yes answer is final until you change it yourself. If you opt in,
the first ping is sent immediately.

## The exact payload

```json
{"schema":1,"version":"0.21.0","relay":"0.7.0","os":"macos"}
```

- `schema` — payload format version, currently `1`.
- `version` — the installed HA NOVA version.
- `relay` — the relay version, included only when it was observed during your
  normal relay traffic within the last 7 days; omitted otherwise (the public
  stats show those pings in an `unknown` bucket). The census never makes its
  own relay call.
- `os` — `macos`, `linux`, or `windows`.

No ID, no IP stored, no timestamps, nothing else. `ha-nova census status`
prints the literal bytes that would be sent right now. Contract tests pin the
payload on both ends (`cli/census_test.go`,
`tests/census-worker/worker.test.ts`), and `scripts/check-docs.sh` checks
[11]/[12] fail the build if telemetry patterns appear anywhere else or the
send path leaves `cli/census*.go`.

## When it sends

At most once per ISO week (UTC), piggybacked on commands that already touch
the network for the update check (`ha-nova check-update` and the background
refresh it spawns). Update checks run automatically as part of normal use —
AI clients trigger one at session start, and a background refresh keeps the
release cache warm — so an opted-in install is counted weekly without you
doing anything. Never on relay hot paths, never blocking (3-second dedicated
timeout, fire-and-forget, no retries), and never changing a single byte of
command output. The week is stamped locally before the send — a failed send
loses that week's count rather than ever double-counting.

Because there is no ID, a reinstall in the same week can count twice and a
version upgrade shows up in the numbers only from the next week on — the
counts are an honest lower bound, not an exact figure. Two smaller honesty
notes: a ping sent seconds before Monday 00:00 UTC can land in the next
week's server bucket (the client's once-per-week invariant holds either way,
only the label may differ by one at the boundary); and platforms outside the
three documented buckets (macOS, Linux, Windows) are simply not counted at
all rather than being guessed.

## How it is counted

The receiving end is a small Cloudflare Worker in this repository
(`census-worker/`) backed by one SQLite Durable Object holding aggregate
counter rows `(iso_week, version, os, relay) -> count`. Request and
invocation logging are disabled; IPs are not stored.

`GET /stats` (public: <https://ha-nova-census.markusleben.workers.dev/stats>)
shows everyone the same aggregates: the weekly series, breakdowns by
OS/version/relay over the last 4 weeks, and `monthly_lower_bound` (the busiest
of the last 4 weeks — a floor, since weekly counts cannot be de-duplicated
without identifiers).

## Controls

| Action | Command |
|---|---|
| Opt in (sends the first ping immediately) | `ha-nova census on` |
| Opt out (nothing is ever sent) | `ha-nova census off` |
| Inspect state and the exact bytes | `ha-nova census status` |
| Environment kill switch (suppresses ask and ping; ANY non-empty value counts as set) | `HA_NOVA_NO_CENSUS=1` |

`ha-nova uninstall` removes the local census state (`census.json`) with the
rest of the managed files. There is nothing to delete server-side because no
per-install record exists — only counters.
