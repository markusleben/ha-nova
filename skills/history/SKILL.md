---
name: history
description: Use when reading Home Assistant entity history, logbook timelines, or long-term statistics through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay App in Home Assistant.
---

# HA NOVA History


## Scope

Read-only timeline work:
- entity state history over a bounded time range
- logbook queries over a bounded time range
- long-term statistics over a bounded time range
- concise "what happened?" summaries

Not in scope:
- live subscriptions
- system logs
- trace debugging
- calendar queries (use `ha-nova:calendar`)
- giant raw exports

Use `ha-nova:read` for traces, `ha-nova:calendar` for calendars, and `ha-nova:fallback` for subscriptions.

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based relay requests:
- `ha-nova relay core --method GET --path <history-path> --out <result-file>`
- `ha-nova relay core --method GET --path <logbook-path> --out <result-file>`
- `ha-nova relay ws --data-file <payload-file> --out <result-file>`
- `ha-nova relay jq --file <result-file> --jq-file <filter-file>`

For `ha-nova relay jq`, the filter is positional unless `--jq-file` is used. Do not invent a `--jq` flag.

Canonical paths:
- `/api/history/period/<start>?filter_entity_id=<entity_id>&end_time=<end>`
- `/api/logbook/<start>?entity=<entity_id>&end_time=<end>`
- `recorder/statistics_during_period`

Prefer `minimal_response` and `no_attributes` on large history queries when the task only needs state transitions.
Envelope parsing follows `skills/ha-nova/relay-api.md` → Standard Envelope; relay-core response shape here stays under `.data.body`.
- history series: `.data.body[0]`
- logbook entries: `.data.body`
- do not probe `.[0]` or `.[0][0]` against the relay envelope
Recorder statistics response stays under WS `.data`.

## Flow

1. Resolve the exact entity target when needed.
2. Set a bounded window.
   - if the user gave a range, use it
   - otherwise default to the last 24 hours for history/logbook
   - otherwise default to the last 30 days for statistics/trend questions
   - if the requested window is too broad, narrow it before querying
3. Choose the source:
   - history for state transitions and values
   - logbook for human-readable events and automation activity
   - statistics for multi-day trends, comparisons, or long-term summaries
4. Execute the read into `<result-file>`.
5. Summarize first:
   - key transitions
   - first/last seen values
   - notable gaps or bursts
   - key periods or trend direction for statistics
   - if the raw state series can contain `unknown` or `unavailable`, do not run numeric min/max across the whole series unless you first filter to numeric states safely
   - prefer simple reductions that do not depend on fragile timestamp parsing unless the user explicitly asked for gap analysis
   - for the default summary, do not build complex jq expressions just to recover min/max event timestamps; numeric range plus a few direct sample timestamps is enough
6. Only show raw excerpts if the user explicitly asks.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

- `Target`
- `Window`
- `Summary`
- `Key events`, `Key transitions`, or `Key periods`
- `Next step`

Keep default output compact. Raw payload dumps are opt-in only.

## Safety

- Read-only skill: never issue mutating relay or service calls.
- For write intent, hand off to the owning skill; unfamiliar writes go through `ha-nova:fallback` first.

- Read-only skill. No writes.
- No guessed entity ids.
- If more than one entity could match, ask one blocking question.
- If the data is incomplete for the requested conclusion, say so explicitly.

## Guardrails

- Never run an unbounded history/logbook/statistics query.
- Cap default investigative windows at 24 hours unless the user asked for more.
- If the user asks for a very large export, narrow the request first.
- Prefer a short summary over dumping a long raw timeline.
