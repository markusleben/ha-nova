# Privacy — the HA NOVA Census

Last updated: 2026-07-25

HA NOVA sends no behavioral or feature-use analytics. Its single optional
measurement is the **Census**: an explicitly approved installation report used
for private maintainer statistics.

## Optional Home Assistant Cloud transport

Home Assistant Cloud remote access is separate from the Census and is available
only in release-gated desktop Beta builds. Selecting `Local only` sends no Home
Assistant traffic through Home Assistant Cloud. Selecting either Cloud mode
uses the user's Nabu Casa service to reach that user's own Home Assistant
through Home Assistant OAuth and Supervisor Ingress. HA NOVA operates no
additional public tunnel, hosted broker, or Cloud endpoint for Home Assistant
requests.

The OAuth refresh token is stored only in a dedicated native OS credential
store. It is not written to HA NOVA config, a file fallback, an environment
variable, command arguments, logs, or AI-visible output. Short-lived OAuth
access tokens, Ingress paths, and Ingress cookies remain process-local and are
not persisted. The Relay still requires its per-device credential; through
Ingress that credential must be bound to both the authenticated Home Assistant
user and the persistent Relay instance.

Cloud setup, reconnect, and explicit unlock may request bounded native
secure-storage prompts for the selected credential slots. Normal Relay calls
use no-UI credential reads and fail closed when the store is locked or
unavailable.

`ha-nova cloud remove` revokes and verifies the HA NOVA OAuth authorization,
then removes its local Cloud secret and metadata. It does not cancel the Home
Assistant Cloud subscription or remove the local NOVA device pairing. Standard
uninstall keeps Home Assistant connection config, device pairing, and Cloud
authorization to support reinstall. `ha-nova uninstall --purge` first revokes
and verifies every configured Cloud authorization, then removes the related
local secrets and configuration; it stops before destructive local cleanup if
safe revocation cannot be completed.

Each voluntary report helps provide a rough picture of participating
installations, their HA NOVA and Relay versions, and the operating-system
distribution. The maintainer uses that directional information to prioritize
compatibility work, testing, bug fixes, and new features where they are likely
to help most. It is not a feature-use measurement, roadmap vote, verified
installed-base count, or feature promise.

Cloudflare is the hosting provider. The HTTPS request therefore exposes the
source IP and connection metadata to Cloudflare under its privacy terms.
HA NOVA ingest code does not read or store the source IP.

**Operator:** Markus Leben (HA NOVA maintainer)
**Contact:** <https://github.com/markusleben/ha-nova/issues> or
[@markusleben](https://github.com/markusleben)

## Exact application JSON

No installation report is sent unless you explicitly choose Yes. The first
report is attempted immediately. Later attempts happen no sooner than seven
days later. A later opt-out or uninstall may send the deletion request
documented below.

```json
{"schema":2,"installation_id":"cns-0123456789abcdef0123456789abcdef","version":"0.21.3","relay":"0.7.1","os":"macos"}
```

- `installation_id` is a dedicated random 128-bit Census ID generated locally.
  It is not derived from or reused from hardware, a device identifier,
  pairing, credentials, a user, a Relay, or Home Assistant. HA NOVA attaches
  no device data.
- `version` is the installed HA NOVA client version.
- `relay` is optional. It appears only when normal Relay traffic observed a
  valid version within the previous 14 days. The Census makes no Relay call.
- `os` is `macos`, `linux`, or `windows`.

No usage, entity, event, Home Assistant, user, pairing, or client timestamp
field is sent. `ha-nova census status` prints the exact JSON body for the
current installation.

## HTTPS provider processing

The JSON body is not the entire network exchange. Cloudflare processes the
source IP, routing data, and connection metadata needed to deliver HTTPS under
its [Privacy Policy](https://www.cloudflare.com/privacypolicy/) and
[Data Processing Addendum](https://www.cloudflare.com/cloudflare-customer-dpa/).

Worker observability and invocation logs are disabled. HA NOVA ingest code
does not read source-IP headers, `request.cf`, or geography. Private statistics
authentication separately validates Cloudflare's Access JWT and does not store
it. The private statistics contain no IP.

## HA NOVA storage

The Worker hashes the Census ID with SHA-256 before Durable Object storage.
The stored installation record contains:

- the Census ID hash
- current HA NOVA version and operating system
- current recently observed Relay version, or no Relay value
- Worker-generated first and last report times

The raw Census ID is not stored. No statistics route exposes individual rows
or hashes. Private breakdowns show only aggregate counts.

An installation is **active** when it reported within 21 days and **known**
when it reported within 60 days. Records without a report for 60 days are
deleted automatically.

Schema-1 pings from older HA NOVA releases contained no Census ID. They remain
only as a separate legacy activity series and are never converted into or
mixed with installation counts.

## Local state

`census.json` stores consent, the random Census ID, the last attempt time,
presentation state, and any recently observed Relay version. It uses mode
`0600` on macOS/Linux and device-local `LOCALAPPDATA` on Windows.

The client records an attempt before its single bounded request. It does not
retry an ambiguous result before another seven days. Restoring or copying
`census.json` may duplicate or share Census identity; HA NOVA does not attempt
cross-machine fingerprinting.

Existing Yes choices made under the older identifier-free disclosure do not
authorize schema 2 and are asked again. Existing explicit No choices remain
No.

## Accuracy

The server deduplicates repeat reports with the hashed Census ID. This supports
a useful participating-installation count, but the public ingest route cannot
attest that a payload came from an official client. Fabricated random IDs are
possible. Numbers are voluntary, self-reported participating installations,
not verified people or the complete installed base.

The Worker limits valid mutations separately by hashed Census ID and route,
retains at most 20,000 installation rows, and admits at most 500 previously
unseen IDs per UTC day. Version and Relay breakdowns retain only the 20 largest
rows plus `other`. These bounds limit storage and cardinality; they do not
authenticate clients, and fabricated IDs can temporarily consume the daily
admission budget. Same-day admission rejections are visible only in the private
maintainer dashboard.

The private dashboard separately reads the official Home Assistant Analytics
count for the NOVA Relay App. That metric covers opted-in Home Assistant
Analytics installations and must not be added to HA NOVA client-installation
counts.

## Private statistics

`/stats` and `/stats/api` are visible only to the maintainer through Cloudflare
Access and Worker-side Access JWT verification. Responses are private and
non-cacheable. The dashboard contains no per-installation table.

## Controls

- `ha-nova census status` — inspect consent, cadence, exact JSON, provider
  disclosure, and the private stats URL.
- `ha-nova census on` — explicitly opt in and attempt the first report.
- `ha-nova census off` — first disable future reports locally, then request
  deletion of the matching server record. If deletion is not confirmed, the
  record expires after 60 days without another report.
- `HA_NOVA_NO_CENSUS=1` — suppress the question, passive Relay observations,
  reports, and withdrawal network requests while set.
- `ha-nova uninstall` — best-effort server withdrawal, then removal of local
  Census state and ID.

The opt-out/uninstall deletion request contains exactly:

```json
{"schema":2,"installation_id":"cns-0123456789abcdef0123456789abcdef"}
```

It contains no version, OS, Relay, usage, or Home Assistant data. Cloudflare
processes its JSON and transport metadata as one HTTPS request under the same
provider terms above.

Details: [docs/reference/census.md](docs/reference/census.md).
