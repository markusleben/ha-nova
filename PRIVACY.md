# Privacy — the HA NOVA Census

Last updated: 2026-07-22

HA NOVA has no telemetry, no analytics, and no phone-home — by design, and
that stays. The single, strictly opt-in exception is the **census**: a
voluntary count of installs. This page is the complete description of it.

**Operator:** Markus Leben (the HA NOVA maintainer).
**Contact:** via GitHub — <https://github.com/markusleben/ha-nova/issues>
(or GitHub profile [@markusleben](https://github.com/markusleben)).

## What is sent — verbatim

Nothing is ever sent unless you explicitly said yes (`ha-nova census on`, or
answering yes to the one-time question). Opting in sends the first ping
immediately; after that, HA NOVA sends this exact JSON payload at most once
per ISO week, during a normal update check. Update checks run automatically
as part of normal use — AI clients trigger one at session start and a
background refresh keeps the release cache warm — so no extra network
activity is created for the census, and an opted-in install is counted weekly
without further action:

```json
{"schema":1,"version":"0.21.0","relay":"0.7.0","os":"macos"}
```

- `version` — the installed HA NOVA version
- `relay` — the relay version, only when it was observed during your normal
  relay traffic within the last 7 days; otherwise the field is omitted
- `os` — one of `macos`, `linux`, `windows`

That is the whole payload. `ha-nova census status` prints the literal bytes
that would be sent from your machine right now. The payload is contract-tested
on both ends (client and server), so it cannot grow silently.

## What is NOT sent or stored

- **No ID of any kind.** No UUID, no install ID, no fingerprint. We could not
  recognize your install twice even if we wanted to — the server only holds
  aggregate counters per week/version/OS/relay.
- **No IP storage.** The receiving endpoint stores no IP addresses, and its
  request/invocation logging is disabled. Your IP transits Cloudflare's edge
  network momentarily to deliver the request, as with any HTTPS call, and is
  not retained by us.
- **Nothing about your home.** No entities, no usage, no events, no client
  timestamps, no Home Assistant data of any kind.

## Purpose

To know roughly how many installs exist, on which OS and versions — so
decisions about what to build and test first rest on real numbers instead of
download guesses. That's all.

## Processing and hosting

The endpoint is a Cloudflare Worker whose full source lives in this repository
(`census-worker/`). Cloudflare, Inc. acts as a processor under its standard
[Data Processing Addendum](https://www.cloudflare.com/cloudflare-customer-dpa/).
Stored data: aggregate counters only.

## Public numbers

Everyone sees the same aggregates the maintainer sees:
`https://ha-nova-census.markusleben.workers.dev/stats`
(the concrete URL is finalized with the worker deployment and also shown by
`ha-nova census status`).

## Your controls

- `ha-nova census status` — see on/off state and the exact bytes.
- `ha-nova census off` — stop immediately. There is nothing to delete
  server-side, because no per-install record exists — only counters.
- `ha-nova census on` — opt back in.
- `HA_NOVA_NO_CENSUS=1` — environment kill switch; suppresses both the
  question and any ping while set (ANY non-empty value counts as set).
- `ha-nova uninstall` removes the local census state file with the rest.

Details and design rationale: [docs/reference/census.md](docs/reference/census.md).
