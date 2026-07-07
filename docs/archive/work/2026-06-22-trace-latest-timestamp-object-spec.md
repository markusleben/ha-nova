# Trace Latest Timestamp Object Spec

Status: implemented locally.

## Problem

`ha-nova trace latest` selected an older Home Assistant trace when `trace/list`
returned entries whose `timestamp` field is an object:

```json
{"timestamp":{"start":"2026-06-21T21:42:08.527830+00:00","finish":"..."}}
```

The CLI only read string timestamps. It then fell back to `last_step`, so
lexicographic sorting could prefer an older trace with a later-looking step path.

## MVP Fix

- Read `timestamp.start` first.
- Fall back to `timestamp.finish` only if `start` is missing.
- Parse timestamps as RFC3339/RFC3339Nano for sorting.
- Keep string fallback only for non-standard payloads.
- Keep the Relay dumb; this is CLI parsing only.

## Verification

- Go regression test for object-shaped timestamps where the newest trace has a
  lexicographically smaller `last_step`.
- Live read-only check against `automation.nachtladung_prepare`.
