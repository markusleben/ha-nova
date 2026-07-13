---
name: external-sources
description: Use when Home Assistant's own data is not enough — querying long-term history from InfluxDB (or another external store Home Assistant writes to but cannot read back) directly from this machine.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA External Sources

## Scope

Data that Home Assistant SENDS somewhere but cannot read back:
- InfluxDB long-term history (the common case: the recorder purges after ~10 days, InfluxDB keeps years)
- the same pattern for other read-only stores (Prometheus, Grafana's backend) when the user has one

Not in scope: Home Assistant's own history and statistics (`ha-nova:history`), energy data (`ha-nova:energy`), or writing to any external system. This skill is read-only by contract.

## The premise correction (say this first)

The `influxdb` integration is **write-only from Home Assistant's side**: it streams states out, and Home Assistant has no API to query them back. So "show me last year's temperature from InfluxDB" cannot go through the relay at all — the relay only talks to Home Assistant.

Instead, this skill queries InfluxDB's own HTTP API **directly from this machine**, using the client's normal HTTP tooling. Say that plainly before doing it: it is a different data path, with the user's own credentials.

## Bootstrap (once per session)

1. Confirm the integration exists: `ha-nova relay core --method GET --path /api/components` and look for `influxdb`. If it is absent, Home Assistant is not writing to InfluxDB and there is nothing to query.
2. Credentials come from the user's environment, never from chat and never from a file in the repo:
   - `HANOVA_INFLUXDB_URL` (e.g. `http://192.168.1.10:8086`)
   - `HANOVA_INFLUXDB_TOKEN` (2.x/3.x) or `HANOVA_INFLUXDB_USER` + `HANOVA_INFLUXDB_PASSWORD` (1.x)
   - `HANOVA_INFLUXDB_BUCKET` (2.x) or `HANOVA_INFLUXDB_DB` (1.x)
   - `HANOVA_INFLUXDB_ORG` (2.x — the query API requires it on self-hosted OSS; optional on Cloud, where the token implies it)
   If they are unset, explain what to set and where (their shell profile) — then STOP. Never ask the user to paste a token into the conversation.

## Relay Contract

The relay is NOT used for the query itself — it cannot reach other hosts, by design. It is used only to check the integration is present (above) and to resolve entity names (`ha-nova:entity-discovery` rules apply).

The query goes out with the client's own HTTP tool.

## Flow

1. Detect the InfluxDB version once: `GET <url>/ping` (1.x answers with an `X-Influxdb-Version` header) or `GET <url>/health` (2.x/3.x).
2. Resolve what the user means to a real Home Assistant entity first, then split it for the query: the writer tags rows with `domain` and with `entity_id` = the object id WITHOUT the domain prefix (`sensor.kitchen_temperature` → `"domain" = 'sensor'`, `"entity_id" = 'kitchen_temperature'`). Querying the full ID silently returns nothing.
3. Query, bounded, and read-only:
   - **1.x**: `GET <url>/query?db=<db>&q=<InfluxQL>` — e.g. `SELECT mean("value") FROM "°C" WHERE "domain" = 'sensor' AND "entity_id" = 'kitchen_temperature' AND time > now() - 365d GROUP BY time(1d)`
   - **2.x**: `POST <url>/api/v2/query?org=<org>` with `Authorization: Token <token>`, a Flux body, and `Accept: application/csv`
   - **3.x**: `POST <url>/api/v3/query_sql` with SQL
   Always bound the time range and aggregate in the query (`GROUP BY time(...)`) — never pull raw points for a year and summarize client-side.
4. Present the result as an answer, not a data dump: the trend, the numbers that matter, the time range, and the source (InfluxDB, not Home Assistant).

## When this comes up repeatedly

If the user keeps asking for the same InfluxDB series, the better answer is a Home Assistant entity: an `influxdb` sensor in YAML materializes a query as a real sensor, which then works with `ha-nova:history` and dashboards like anything else. Hand off to `ha-nova:yaml-config` for that — it is the durable fix.

## Error Handling

- Missing credentials: a configuration gap, not an error. Say what to set, stop.
- `401`/`403`: the token is wrong or lacks read permission on that bucket — the user fixes it in InfluxDB, not here.
- Empty result: usually the full entity ID in the `entity_id` tag (it holds only the object id) or a wrong measurement name (InfluxDB names measurements after the unit, e.g. `°C`). Show what you queried before concluding the data does not exist.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Render the Report shape (output-rules.md). Name the source explicitly (InfluxDB, queried directly — not Home Assistant), the time range, and the query in short form; answer the question, do not paste raw rows.

## Safety

- Read-only skill: never issue mutating relay or service calls.
- For write intent, hand off to the owning skill; unfamiliar writes go through `ha-nova:fallback` first.

- **Read-only by contract**: this skill never constructs an INSERT, DELETE, DROP, or any schema change against an external store. If the user asks for one, refuse and point at the store's own tooling.
- Credentials live in the user's environment only. Never echo a token, never write one into a file, never ask for one in chat.
- The relay is not involved in these queries: say so, so the user knows their data path.

## Guardrails

- Always bound the time range; always aggregate server-side.
- Never guess an `entity_id` or measurement name — resolve, then query.
- Never present external data as if it came from Home Assistant.
