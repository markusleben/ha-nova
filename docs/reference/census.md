# The HA NOVA Census

HA NOVA sends no behavioral or feature-use analytics. The census is one
strictly opt-in measurement with an identifier-free application JSON body,
intended as a directional signal about participating versions and operating
systems. It cannot establish a verified count of unique installs. Cloudflare
is the hosting provider for the census endpoint, so the HTTPS request exposes
source-IP and connection metadata to Cloudflare; the distinction is documented
below. This page is the detail reference behind the one-time choice; the
plain-language privacy summary is
[PRIVACY.md](../../PRIVACY.md).

## The one-time privacy choice

This is the verbatim disclosure and choice the CLI shows:

```
  One-time privacy choice

  May this installation contribute to HA NOVA's public version statistics?
  HA NOVA sends no behavioral or feature-use analytics.
  If you agree, HA NOVA sends this version information now and then at most
  once per week.
  The message content (JSON) contains only:
      payload schema  ·  HA NOVA version  ·  operating system
      recently observed relay version (when available)
  No installation, device, or user ID; no usage or Home Assistant data.
  Cloudflare is the hosting provider for the census endpoint. It processes the
  source IP and connection metadata for HTTPS under its privacy policy.
  HA NOVA does not read the source IP or store it in application data or public
  statistics.
  The public numbers show general trends, not a verified installation count.

  Inspect exact JSON: ha-nova census status   Change: ha-nova census on|off
  Details: docs/reference/census.md
  Choose one:
    1. Yes — contribute
    2. No — do not contribute
    3. Show exact data
  Select 1, 2, or 3:
```

While local census state remains intact, the interactive question is asked at
most once per install — on an
interactive terminal at the end of `ha-nova setup`, after `ha-nova update`,
after a clean `ha-nova doctor`, or on a plain `ha-nova check-update`. For
skill-only sessions the same open question may be delivered as an AI-client
callout until it has actually been presented three times; deferral behind
another active choice does not consume a presentation. Only explicit Yes or No
changes consent. Blank, free-form, dismissed, or ambiguous input changes
nothing. Show exact data prints the literal JSON, leaves consent unchanged, and
shows the same three choices again. If you opt in, the first eligible ping
attempt starts immediately and the result is reported separately from the
saved choice.

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

The JSON body has no installation, device, or user ID, IP field, client
timestamp, usage data, or Home Assistant data. `ha-nova census status` prints
the literal application JSON bytes that would be sent right now. Contract
tests pin the payload on both ends (`cli/census_test.go`,
`tests/census-worker/worker.test.ts`). `scripts/check-docs.sh` checks [11]/[12]
keep known census symbols inside `cli/census*.go`, pin the explicit opt-in
guard, and reject configured analytics-vendor patterns elsewhere. Check [12b]
positively limits incoming Worker Request reads to method, URL, body,
`content-type`, and `content-length`.

The JSON body is not the whole HTTPS exchange. Cloudflare is the hosting
provider for the census endpoint and processes the source IP, routing data, and
other connection metadata for the request under its
[Privacy Policy](https://www.cloudflare.com/privacypolicy/). The Worker runtime
makes request metadata available to application code, but HA NOVA's Worker code
does not read source-IP headers or `request.cf`; the aggregate application
storage schema has no place to store them.

## When it sends

The client records at most one attempt per ISO week (UTC) in its local state
before sending, piggybacked on commands that already touch
the network for the update check (`ha-nova check-update` and the background
refresh it spawns). Update checks run automatically as part of normal use —
whichever independently loadable skill is used first triggers one human-output
check per selected profile before its first Home Assistant task, even when
background context already refreshed the cache. A background refresh keeps
the release cache warm, so an opted-in install normally attempts one ping each
week without you doing anything. Never on relay hot paths, output-safe by
construction: the synchronous send runs after all command output, bounded by
a dedicated 1.5-second timeout, with redirects rejected and no automatic retry
— and never changing a single byte of command output. The week is stamped locally before the send — a
failed carrier send loses that week's ping rather than risking a duplicate.

This is a local-state guarantee, not a server-side uniqueness guarantee.
Restoring, deleting, or rolling back `census.json` can permit another attempt
in the same week; the application JSON body and aggregate rows contain no
installation, device, or user identifier with which to deduplicate it.

A reinstall in the same week can therefore ping twice. The public receiver
also has no client authentication: any schema-valid unauthenticated payload is
accepted, so duplicate or fabricated pings cannot be distinguished from the
released client. Cloudflare, the hosting provider, processes source-IP
transport metadata, but HA NOVA Worker code does not read it and the
application rows do not store it.
The aggregates are therefore a directional activity signal, not a lower bound,
exact total, or verified unique-install count. Two smaller honesty notes: a
ping sent seconds before Monday 00:00 UTC can land in the next week's server
bucket (the client's once-per-week invariant holds either way, only the label
may differ by one at the boundary); and platforms outside the three documented
buckets (macOS, Linux, Windows) are not counted rather than being guessed.

## How it is counted

The receiving end is a small Cloudflare Worker in this repository
(`census-worker/`) backed by one SQLite Durable Object holding aggregate
counter rows `(iso_week, version, os, relay) -> count`. HA NOVA disables Worker
request/invocation logging, does not read source-IP request metadata, and stores
no IP in its application database or public statistics. Cloudflare separately
processes End User and Network Data under its Privacy Policy and
[Data Processing Addendum](https://www.cloudflare.com/cloudflare-customer-dpa/).
Cloudflare still exposes built-in aggregate Worker metrics
(request counts/status/runtime) with up to three months of metrics retention.

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
| Inspect state and the exact application JSON bytes | `ha-nova census status` |
| Environment kill switch (suppresses ask and ping; ANY non-empty value counts as set) | `HA_NOVA_NO_CENSUS=1` |

`ha-nova uninstall` removes the local census state (`census.json`). It retains
two opaque random safety markers outside the managed directories: one stops
census activity until a successful setup, while the other is a persistent,
rotating install generation that prevents stale setup/update processes from
restoring removed files. They carry no consent, timestamps, process data, or
stable device ID and are never sent. A later successful setup removes the
census stop marker and rotates the install generation. There is nothing to delete server-side
because no per-install record exists — only counters.

The local state records the one-time question/answer, enabled flag, last
attempted ISO week, AI-client presentation count, and any recently observed Relay
version with its local observation time. None of those local timestamps or
control fields are sent.
