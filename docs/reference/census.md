# The HA NOVA Census

HA NOVA sends no behavioral or feature-use analytics. The Census is an
explicitly opt-in count of participating HA NOVA client installations,
versions, operating systems, and recently observed Relay versions. Statistics
are private to the maintainer.

Cloudflare is the hosting provider. The HTTPS connection therefore exposes the
source IP and connection metadata to Cloudflare under its privacy terms.
HA NOVA ingest code does not read or store the source IP.

## Why participation helps

By participating, you help give the maintainer a rough picture of how many HA
NOVA installations participate, which HA NOVA and Relay versions they use, and
how operating systems are distributed. That directional picture helps
prioritize compatibility work, testing, bug fixes, and new features where they
are likely to help most.

The Census does not measure feature use or what an individual user wants. Its
numbers are one planning input, not a roadmap vote, verified installed-base
count, or promise that a particular feature will be built.

## The one-time choice

The CLI presents a separate Yes/No/Show exact data action:

```text
  One-time privacy choice

  May this HA NOVA installation contribute to the maintainer's private
  installation and version statistics?
  HA NOVA sends no behavioral or feature-use analytics.
  Why this helps: by contributing, you give the maintainer a rough picture of
  how many installations participate, which HA NOVA and Relay versions they use,
  and how operating systems are distributed. This helps prioritize compatibility
  work, tests, bug fixes, and new features where they are likely to help most.
  If you agree, HA NOVA sends the first report now. Further reports are sent
  no sooner than seven days later.
  The message content (JSON) contains only:
      payload schema  ·  random Census installation ID
      HA NOVA version  ·  operating system
      recently observed relay version (when available)
  The random ID only lets the same participating installation count once. It
  is not derived from or reused from hardware, a device identifier, pairing,
  a user, a Relay, or Home Assistant. HA NOVA attaches no device data.
  No usage or Home Assistant data is sent.
  Cloudflare is the hosting provider for the census endpoint. It processes the
  source IP and connection metadata for HTTPS under its privacy policy.
  HA NOVA ingest code does not read or store the source IP.
  Counts are voluntary and self-reported, not verified people or the complete
  installed base.

  Inspect exact JSON: ha-nova census status   Change: ha-nova census on|off
```

Only an explicit Yes or No changes consent. Blank, dismissed, free-form, or
ambiguous input changes nothing. “Show exact data” prints the literal JSON and
then offers the same choice again.

The direct terminal question and the skill-mediated question are each claimed
once before rendering. An interruption may therefore leave the choice
unanswered rather than cause repeated privacy prompts. Skill actions carry a
random local choice ID so an old button cannot overwrite a newer manual or
concurrent consent change. Existing schema-1 Yes choices are asked again
because that older disclosure explicitly promised an identifier-free payload.
Existing explicit No choices remain No.

## Exact schema-2 payload

```json
{"schema":2,"installation_id":"cns-0123456789abcdef0123456789abcdef","version":"0.21.3","relay":"0.7.1","os":"macos"}
```

- `schema` — payload format, currently `2`.
- `installation_id` — 128 random bits generated locally with a `cns-` prefix.
  This ID is dedicated to Census and is not derived from or reused from
  pairing, credentials, hardware/device identifiers, a Relay, or Home
  Assistant. HA NOVA attaches no device data.
- `version` — installed HA NOVA client version.
- `relay` — included only when normal Relay traffic observed a valid version
  during the previous 14 days. The Census makes no Relay request. When omitted,
  private statistics say “not recently observed”; omission is not a Relay
  version.
- `os` — one of `macos`, `linux`, or `windows`.

No user, pairing, device, Home Assistant, usage, or client timestamp field is
sent. `ha-nova census status` prints the exact body for the current state.
Contract tests pin the Go and Worker field sets.

## Transport and storage

Cloudflare processes the JSON and transport metadata as parts of the same
HTTPS request. Worker observability
and invocation logs are disabled. The application request adapter reads only
the HTTP method, URL, bounded body, content headers, and explicit Cloudflare
Access authentication headers for private statistics. It never reads
`request.cf`, source-IP headers, or geographic metadata.

Before Durable Object storage, the Worker replaces the Census ID with its
SHA-256 hash. The installation table contains:

- Census ID hash
- current HA NOVA version and OS
- current recently observed Relay version, or no value
- first and last report time generated by the Worker

There is no operator endpoint containing individual rows or hashes.

## Sending cadence and accuracy

After Yes, one report is attempted immediately. Later attempts happen no
sooner than seven rolling 24-hour periods after the previous attempt. The
timestamp is written locally before the single bounded HTTPS attempt; ambiguous
failures are not retried early.

The report normally rides after an update check. It never changes command
output or exit status, never runs on Relay hot paths, rejects redirects, and
has a 1.5-second request deadline.

The server upserts by Census ID hash, so repeat reports update one participating
installation rather than inflating the count. The endpoint is still public and
has no client attestation: fabricated IDs cannot be distinguished from released
clients. Counts are voluntary, self-reported participating installations, not
verified people or the complete installed base.

To bound resource use, valid schema-2 mutations are limited separately by
hashed Census ID and route. The Worker retains at most 20,000 installations,
admits at most 500 previously unseen IDs per UTC day, and exposes any same-day
admission rejections on the private dashboard. Version and Relay breakdowns
show the 20 largest rows plus an `other` bucket. These controls limit storage
and cardinality; they cannot prevent someone from fabricating valid IDs or
temporarily consuming the daily admission budget.

## Retention and counting

- **Active client installations:** last report within 21 days.
- **Known client installations:** last report within 60 days.
- Records with no report for 60 days are deleted automatically.
- Breakdowns by client version, OS, and recently observed Relay version use
  active installations only.
- Version and Relay breakdowns contain the 20 largest rows plus `other`; OS
  remains bounded by the three accepted values.
- Identifier-free schema-1 pings remain in a separate legacy activity series.
  They are never converted to or mixed with installation counts.

The private dashboard also fetches the official Home Assistant Analytics entry
for the NOVA Relay App slug `2368fcfa_ha_nova_relay`. This produces a second,
separate number: opted-in Home Assistant installations reporting that App.
Client installations and Relay App installations must never be added together.
If Home Assistant Analytics is unavailable, the dashboard reports it as
unavailable rather than inventing or silently reusing a value.

`GET /stats` and `GET /stats/api` are private maintainer routes protected by
Cloudflare Access and a second JWT validation inside the Worker. Responses use
`Cache-Control: private, no-store`. `/stats` contains no per-ID table.

## Controls

| Action | Command |
|---|---|
| Opt in and attempt the first report | `ha-nova census on` |
| Opt out and request immediate server-side deletion | `ha-nova census off` |
| Inspect state, exact JSON, provider disclosure, and private stats URL | `ha-nova census status` |
| Suppress asks, passive Relay stamps, reports, and withdrawal traffic | `HA_NOVA_NO_CENSUS=1` |

`ha-nova census off` first saves local No and disables future reports, then
issues one bounded `/withdraw` request if a report may have reached the server.
That deletion request contains only:

```json
{"schema":2,"installation_id":"cns-0123456789abcdef0123456789abcdef"}
```

Cloudflare processes its JSON and transport metadata under the same terms.
If deletion cannot be confirmed, no new reports are sent and the server record
expires after 60 days. Running `off` again retries a pending deletion.

Uninstall performs the same best-effort withdrawal before removing local Census
state. It then removes the Census ID. Existing lifecycle safety markers remain
local, contain no Census ID or consent, and are never sent.

## Production isolation for tests and releases

Production census statistics represent voluntary real participants only
(#446). Tests, smokes, release runs, and deployment verification never call
the production Worker's `/ping` or `/withdraw`:

- `scripts/release/verify-census-deployment.sh` is read-only by contract — it
  verifies deployment identity, authentication, headers, and the schema-2
  stats contract, nothing else.
- Functional ping/deduplication/withdrawal checks run exclusively against the
  isolated test Worker (`census-worker/wrangler.toml` → `[env.test]`, its own
  Durable Object storage): `npx wrangler@4.113.0 deploy --env test`, then
  `bash scripts/release/verify-census-functional.sh`.
- The invariant is enforced statically by
  `scripts/test/check-census-production-isolation.mjs`
  (tests/onboarding/census-production-isolation.test.ts).
- Test and E2E helpers that invoke the built binary export
  `HA_NOVA_NO_CENSUS=1`, which suppresses asks, reports, withdrawals, and
  passive relay-version stamps.
