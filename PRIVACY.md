# Privacy — the HA NOVA Census

Last updated: 2026-07-23

HA NOVA sends no behavioral or feature-use analytics. The single, strictly
opt-in measurement is the **census**: a small HTTPS request with an
identifier-free application JSON body, used as a directional signal about
participating versions and operating systems. It is not a verified count of
unique installs. Cloudflare is the hosting provider for the census endpoint
and processes source-IP and connection metadata for the HTTPS request. This
page describes both layers.

**Operator:** Markus Leben (the HA NOVA maintainer).
**Contact:** via GitHub — <https://github.com/markusleben/ha-nova/issues>
(or GitHub profile [@markusleben](https://github.com/markusleben)).

## Application JSON body — verbatim

Nothing is ever sent unless you explicitly said yes (`ha-nova census on`, or
answering yes to the one-time question). Opting in makes the first eligible
send attempt immediately; after that, HA NOVA records one attempt per ISO week
before sending this exact JSON payload alongside a normal update check. While
that local state remains intact, it does not attempt again in the same week. Update checks run automatically
as part of normal use — AI clients trigger one at session start or before the
first Home Assistant task, depending on the client, and a background refresh
keeps the release cache warm — so an opted-in install
normally attempts one ping per week without further action. To be precise: the census ping IS one
additional small HTTPS request to the census endpoint (that is all it is); it
never adds update-check traffic, relay calls, redirects, or retries beyond that
single weekly POST attempt:

```json
{"schema":1,"version":"0.21.0","relay":"0.7.1","os":"macos"}
```

- `schema` — payload format version, currently `1`
- `version` — the installed HA NOVA version
- `relay` — the relay version, only when it was observed during your normal
  relay traffic within the last 7 days; otherwise the field is omitted
- `os` — one of `macos`, `linux`, `windows`

That is the whole application JSON body. It is not the whole HTTPS exchange:
Cloudflare, the hosting provider for the census endpoint, processes source-IP,
routing, and connection metadata for the request under its Privacy Policy.
`ha-nova census status` prints the literal JSON bytes that would be sent from
your machine right now. The JSON contract is tested on both ends (client and
server), so it cannot grow silently.

## What HA NOVA application code does not collect

- **No installation, device, or user ID in the JSON body.** No UUID, install
  ID, or fingerprint. HA NOVA's application database cannot recognize the same
  installation twice; it holds only aggregate counters per
  week/version/OS/relay.
- **No source-IP collection by HA NOVA Worker code.** Cloudflare hosts the
  census endpoint and processes the source IP and connection metadata for the
  HTTPS request. It makes request metadata available to the Worker runtime. HA
  NOVA code does not read source-IP headers or `request.cf`; its application
  database and public statistics do not store the IP. Worker
  request/invocation logs are disabled.
- **Nothing about your home.** No entities, no usage, no events, no client
  timestamps in the payload, no Home Assistant data of any kind.

## Local state on your machine

`census.json` records the one-time question and answer, whether census is
enabled, the last attempted ISO week, the AI-client presentation count, and —
when observed through normal Relay traffic — the recent Relay version plus its
local observation time. This state never enters the payload. On macOS and Linux
it lives in the current user's config directory with mode `0600`; on Windows it
uses device-local `LOCALAPPDATA`, not roaming `APPDATA`. `ha-nova uninstall`
removes it.

Restoring, deleting, or rolling back local census state can permit another
attempt in the same ISO week; there is no server-side identifier with which to
deduplicate it. Uninstall retains two opaque random safety markers outside the
managed directories: a census stop marker prevents stale processes from
recreating census state, and a rotating install-generation marker prevents
stale setup/update work from restoring the installation. They contain no
answer, timestamp, process data, or stable device ID and are never sent. A
later successful setup removes the census stop marker and rotates the install
generation.

## Purpose and accuracy limit

To get a directional signal about which versions and operating systems opt-in
participants use, so decisions about what to build and test first are not based
only on download guesses. The application JSON body and aggregate rows have no
installation, device, or user identifier, and the public receiver has no
client authentication. HA NOVA Worker code does not read Cloudflare's source-IP
metadata. Any schema-valid unauthenticated payload can increment a counter, so
duplicate or fabricated pings cannot be separated from released clients. The
aggregates are not an exact total, a lower bound, or a verified unique-install
count.

## Processing and hosting

The endpoint is hosted by Cloudflare as a Cloudflare Worker whose full source
lives in this repository (`census-worker/`). Cloudflare describes its
processing of End User IP addresses, routing data, Customer Logs, and derived
Network Data in its
[Privacy Policy](https://www.cloudflare.com/privacypolicy/) and
[Data Processing Addendum](https://www.cloudflare.com/cloudflare-customer-dpa/).
Those Cloudflare processing terms remain applicable even though HA NOVA
disables Worker request/invocation logs and does not read or store source-IP
metadata in its application. HA NOVA application storage contains aggregate
counter rows only. Cloudflare also provides built-in aggregate Worker metrics
such as request counts, status, and runtime duration; Cloudflare documents up
to three months of metrics retention.

## Public numbers

The public endpoint is
<https://ha-nova-census.markusleben.workers.dev/stats>. It publishes a sparse
accepted-ping series within a 26-week horizon plus 4-week
OS/version/Relay breakdowns and the peak weekly ping count in that window.
Aggregate counter rows have no automatic expiry and remain until the operator
deletes them; older rows simply age out of the public horizon. To bound storage
abuse, new dimension combinations beyond 256 rows in one week fold into an
`other` overflow bucket; this does not represent another client OS. There is
no per-install record or view for either the maintainer or the public.
`ha-nova census status` shows the same URL.

## Your controls

- `ha-nova census status` — see on/off state and the exact application JSON
  bytes.
- `ha-nova census off` — after the command returns successfully, no new ping
  can begin. If one bounded request had already started, the command waits for
  it to finish first. There is nothing to delete server-side, because no
  per-install record exists — only counters.
- `ha-nova census on` — opt back in.
- `HA_NOVA_NO_CENSUS=1` — environment kill switch; suppresses both the
  question and any ping while set (ANY non-empty value counts as set).
- `ha-nova uninstall` removes the local census state file and retains only the
  two opaque, data-free lifecycle safety markers described above.

Details and design rationale: [docs/reference/census.md](docs/reference/census.md).
