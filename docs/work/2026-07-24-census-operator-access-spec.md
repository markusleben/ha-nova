# Census Installation Counting and Operator Access

Status: active
Date: 2026-07-24

## Goal

Replace identifier-free weekly ping counts with an honest count of distinct,
opted-in HA NOVA client installations. Keep that client count separate from
the official NOVA Relay App count published by Home Assistant Analytics.

## Client contract

- A new explicit consent contract sends schema 2 only after Yes:
  `installation_id`, HA NOVA version, operating system, and an optional
  recently observed Relay version.
- `installation_id` is a dedicated random 128-bit Census identifier. It is not
  derived from or reused from `client_install_id`, pairing, hardware/device
  identifiers, a user, a Relay, or Home Assistant. HA NOVA attaches no device
  data.
- Existing schema-1 Yes answers reopen because the payload now contains a
  pseudonymous identifier. Existing No answers remain No.
- The first report follows each disabled-to-enabled Yes immediately. Later
  attempts while enabled are separated by at least seven rolling days;
  user-facing copy never mentions ISO weeks.
- Relay observations remain eligible for 14 days. Missing values are described
  as "not recently observed", never as an unknown Relay version.
- `census off` and uninstall stop sends first, then make one bounded,
  best-effort withdrawal request. Failure never re-enables Census; the record
  expires through retention.
- Skill-mediated Yes/No actions use a random one-time choice ID. A stale UI
  action cannot overwrite newer consent.

## Receiver and statistics

- `POST /ping` continues accepting legacy schema 1 into the existing aggregate
  counters. Schema 2 upserts one installation record by a SHA-256 hash of the
  wire identifier; the raw identifier is never stored.
- `POST /withdraw` deletes that hashed record idempotently.
- Active means last report within 21 days. Known means last report within 60
  days. An alarm removes records after 60 days without a report.
- Legacy ping activity, client installations, and official Relay App
  installations are three separate measures and are never summed.
- `/stats` and `/stats/api` are maintainer-only through Cloudflare Access and
  a second in-Worker JWT verification. `/ping` and `/withdraw` remain public.
- The dashboard contains no per-installation records. It shows active/known
  client totals and breakdowns, legacy activity, and the exact
  `2368fcfa_ha_nova_relay` aggregate from Home Assistant Analytics.
- The Worker retains at most 20,000 installation rows, admits at most 500 new
  IDs per UTC day, and bounds version/Relay breakdowns to the 20 largest rows
  plus `other`. The private dashboard reports same-day admission rejections.

## Privacy and honesty

- Cloudflare is named as the hosting provider. It necessarily processes source
  IP and connection metadata for HTTPS. HA NOVA ingest code does not read the
  source IP or `request.cf`, and application storage has no IP field.
- The endpoint is public and unauthenticated. Counts are voluntary,
  self-reported participating installations, not verified people or the total
  installed base. Fabricated IDs can consume the daily admission budget; the
  resource bounds limit damage but do not authenticate clients.
- Worker observability and invocation logs remain disabled.

## Verification

- Contract tests pin the exact schema-2 body and new-consent migration.
- Same identifier twice counts once; changed version replaces the old
  breakdown; withdrawal removes it; separate identifiers count separately.
- Local Wrangler integration proves the real SQLite Durable Object path before
  production deployment.
- Production deduplication and withdrawal use an uncapped private aggregate for
  the reserved `0.0.0-rc999999` smoke version. The verifier never queries an
  installation ID, and unrelated live client reports cannot move this signal.
- Unauthenticated statistics fail; browser and service-token Access JWTs pass.
- The v0.21.3 RC exercises consent on macOS and Windows before the reviewed
  Worker is deployed; a removable production test identifier proves the final
  deployment before the stable tag.
