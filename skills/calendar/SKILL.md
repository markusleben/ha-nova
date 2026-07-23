---
name: calendar
description: Use when listing, reading, creating, updating, or deleting Home Assistant calendar events through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Calendar

## Scope

Calendar event work:
- list calendar entities
- read events for one calendar over a bounded window
- summarize upcoming events across selected calendars
- create, update, or delete one event

Not in scope:
- recurring-event creation (finish in the Home Assistant UI; `calendar.create_event` has no recurrence field)
- calendar dashboard edits
- automation writes that use calendar triggers
- calendar sharing, permissions, or provider-account settings

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use REST reads, one create service, and feature-gated WebSocket writes:
- `ha-nova relay core --method GET --path /api/calendars --out <result-file>`
- `ha-nova relay core --method GET --path /api/states/<calendar_entity_id> --out <result-file>`
- `ha-nova relay core --method GET --path <calendar-events-path> --out <result-file>`
- `ha-nova relay core --method POST --path /api/services/calendar/create_event --body-file <payload-file> --out <result-file>`
- `ha-nova relay ws --data-file <payload-file> --out <result-file>` for `calendar/event/update` and `calendar/event/delete`
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
6. Summarize read results:
   - title/summary
   - start and end
   - all-day vs timed; all-day events carry date-only values with an EXCLUSIVE end — a one-day event on the 14th returns end = the 15th; report it as "on the 14th", never as "ends the 15th"
   - timed events: render times in the user's local timezone, and say which timezone applies when the returned offset differs from it
   - recurring events arrive pre-expanded as individual instances within the window — count and report them as such, not as one series
   - location only when useful
   - omit private descriptions unless the user explicitly asks

### Create, update, or delete

1. Resolve one exact calendar and read `/api/states/<entity_id>`. Gate create on `supported_features & 1`, delete on `& 2`, and update on `& 4`; stop when the required bit is absent.
2. Read a bounded event window covering the target. Update/delete require an exact event with `uid`; use `(uid, recurrence_id)` as the instance identity. If several events match the user's description, ask one blocking question. Never guess.
3. Normalize event fields:
   - timed events use `start_date_time`/`end_date_time` for create and `dtstart`/`dtend` inside the WS `event` object; both values use the Home Assistant timezone and end must be after start
   - all-day events use `start_date`/`end_date` for create and date-only `dtstart`/`dtend` for update; end is exclusive
   - create requires `summary`; optional `description` and `location` are included only when set
   - update is a full event replacement: merge requested changes into the current summary/start/end/description/location/rrule instead of sending a patch; omit absent optional fields, use an empty string only to clear description/location, and omit `rrule` to clear recurrence
4. For an occurrence with `recurrence_id`, ask whether the scope is **this occurrence** (`recurrence_range:""`) or **this and future occurrences** (`"THISANDFUTURE"`). Bind that scope to the preview. For a single-occurrence update, omit `rrule` from the replacement event. Do not invent an entire-series mode.
5. Show the Preview Card with operation, calendar, exact event identity, summary, start/end, timezone or all-day range, changed fields, and recurrence scope. Create/update use natural bound confirmation; delete uses the typed `confirm:<token>` from the core rule.
6. Immediately before execution, re-read the target window and calendar state; this event set is the create baseline. Abort and rebuild the preview if capability, identity, current fields, or recurrence scope drifted.
7. Execute exactly one write:
   - create: POST `{"entity_id":"calendar.example","summary":"Event","start_date_time":"<start>","end_date_time":"<end>"}` to `/api/services/calendar/create_event`
   - update: WS `{"type":"calendar/event/update","entity_id":"calendar.example","uid":"<uid>","recurrence_id":"<instance>","recurrence_range":"<range>","event":{"summary":"Event","dtstart":"<start>","dtend":"<end>"}}`; omit recurrence keys for a non-recurring event
   - delete: WS `{"type":"calendar/event/delete","entity_id":"calendar.example","uid":"<uid>","recurrence_id":"<instance>","recurrence_range":"<range>"}`; omit recurrence keys for a non-recurring event
8. Verify through bounded REST read-back, up to three reads over ten seconds:
   - create: compared with the baseline, exactly one new event must match the requested normalized fields; the service response alone is not identity evidence
   - update: the same `(uid, recurrence_id)` must contain the full requested fields; for `THISANDFUTURE`, also check the first later occurrence when one exists in the window and state that later events outside the window were not verified
   - delete: the target identity must be absent; for `THISANDFUTURE`, all occurrences with that `uid` from the selected instance onward must be absent within the window
   - ambiguous, stale, or delayed results are reported as not verified; never repeat a write automatically

## Error Handling

- Relay/upstream failures follow `skills/ha-nova/relay-api.md` → Error Handling.
- Unsupported feature or missing `uid`: stop and name the limitation; do not try another endpoint.
- Timeout/`502`: verify first. Create may already have produced an event, so automatic retry risks a duplicate.
- Provider rejection: surface the Home Assistant reason without guessing a corrected payload.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

- `Calendar`
- `Window`
- `Events`
- `Next step`

These slots render the Report shape (output-rules.md); event groups follow the List Frame. For multiple calendars, group by calendar and keep each group short. Writes use the Preview Card and Result Card; report the exact bounded verification scope.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Create and update require natural confirmation; delete retains the core typed confirmation-code gate.
- Calendar event writes have no HA NOVA revert. A provider recycle bin may exist but is not guaranteed; recreate only from a user-approved preview.
- Always use bounded windows.
- Never guess a calendar id from a partial name when multiple matches exist.
- One event write at a time; no batch mutation.

## References

- Relay API: `skills/ha-nova/relay-api.md`
- Shared write safety: `skills/ha-nova/write-safety.md`
