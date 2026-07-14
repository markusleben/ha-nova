---
name: calendar
description: Use when listing Home Assistant calendars or reading bounded calendar event windows through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Calendar

## Scope

Read-only calendar work:
- list calendar entities
- read events for one calendar over a bounded window
- summarize upcoming events across selected calendars

Not in scope:
- create, update, delete, or edit calendar events
- recurring-event management
- calendar dashboard edits
- automation writes that use calendar triggers

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use REST through relay core only:
- `ha-nova relay core --method GET --path /api/calendars --out <result-file>`
- `ha-nova relay core --method GET --path <calendar-events-path> --out <result-file>`
- `ha-nova relay jq --file <result-file> --jq-file <filter-file>`

`<calendar-events-path>` is:
`/api/calendars/<calendar_entity_id>?start=<timestamp>&end=<timestamp>`

Relay-core response body is under `.data.body` (envelope contract: `skills/ha-nova/relay-api.md` → Standard Envelope).

## Flow

1. List calendars with `/api/calendars`.
2. Resolve the requested calendar by exact `entity_id` or exact/clear name match.
3. If multiple calendars match, ask one blocking question.
4. Set a bounded event window:
   - if the user gave start/end, use them
   - otherwise default to now through the next 7 days
   - if the user asks for an unbounded or very broad range, narrow it before querying
5. Query events with `/api/calendars/<entity_id>?start=<start>&end=<end>`.
6. Summarize events:
   - title/summary
   - start and end
   - all-day vs timed; all-day events carry date-only values with an EXCLUSIVE end — a one-day event on the 14th returns end = the 15th; report it as "on the 14th", never as "ends the 15th"
   - timed events: render times in the user's local timezone, and say which timezone applies when the returned offset differs from it
   - recurring events arrive pre-expanded as individual instances within the window — count and report them as such, not as one series
   - location only when useful
   - omit private descriptions unless the user explicitly asks

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

- `Calendar`
- `Window`
- `Events`
- `Next step`

These slots render the Report shape (output-rules.md); event groups follow the List Frame. For multiple calendars, group by calendar and keep each group short.

## Safety

- Read-only skill: never issue mutating relay or service calls.
- For write intent, hand off to the owning skill; unfamiliar writes go through `ha-nova:fallback` first.

- Read-only skill. No event writes.
- Always use bounded windows.
- Never guess a calendar id from a partial name when multiple matches exist.
