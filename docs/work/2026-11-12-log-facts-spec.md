# Log facts: which integration has been failing since when

Status: active (built for 0.31, Markus D4 "Ja, in 0.31")

Date: 2026-11-12. Owner: the CLI (`cli/log_facts.go`), the gate worker (`cli/command_gate.go` `/redact`, `cli/gate_verdict.go`), the `diagnose` skill. High-risk: it opens a read the gate refuses today. Source: HANO-34 F4 (narrowed after review C2).

## Problem

Since 0.29 every log read is refused (`gateLogRead`, `rawLogRefusal`): log lines are free text that can hold passwords, keys, addresses, and instructions aimed at the AI, and no redactor bounds free text. The AI so loses its cheapest signal ("the Hue integration has logged 40 errors since 08:12") and makes the user search the log page for it.

## Change

One request, WS `{"type":"system_log/list"}` with no other key, answers **log facts** instead of the log. Its answer holds only values HA NOVA builds itself:

```json
{"log_facts": [{"domain": "hue", "level": "ERROR", "count": 40, "first_retained": "2026-11-12T07:12:03Z", "last": "2026-11-12T09:40:11Z"}],
 "unattributed_count": 3}
```

- `domain`: the first segment after `homeassistant.components.` or `custom_components.` in the entry's logger name (so an automation's logger suffix never passes; any other `homeassistant.*` logger is `homeassistant`), kept only when it is one of this home's integrations (`manifest/list`, a read the AI already has) and matches `^[a-z0-9_]{1,64}$`.
- `level`: one of `DEBUG`, `INFO`, `WARNING`, `ERROR`, `CRITICAL`.
- `count`: the sum of the entries' counts, each a whole number from 1 to 10¹², the sum capped at 10¹².
- `first_retained` / `last`: the oldest `first_occurred` and the newest `timestamp` of the group, written by HA NOVA as UTC RFC 3339 to the second. "Retained": Home Assistant keeps a bounded number of entries (50 by default) and drops the oldest, so this is the oldest occurrence it still holds, not the first ever.
- An entry without a trusted domain, a known level, a valid count, or both times adds its count (or 1) to `unattributed_count`.
- Grouped by (domain, level), newest `last` first. Message, exception, logger name, and `source` are never read into the output. Every output string is a trusted domain, a fixed level word, or a time HA NOVA formatted.
- Failures carry fixed words only: Home Assistant's own error text could quote the log, so the worker answers an error envelope with `LOG_FACTS_FAILED` and a fixed message, and the CLI words a Relay or Home Assistant error with its HTTP status and a fixed code (`^[A-Z][A-Z0-9_]{0,63}$`, else `ERROR`), never its message; HA NOVA's own errors (unreachable, pairing, timeouts) keep their words. Only `GATE_REFUSED` (the 0.30 gate) adds "this App is older than 0.31" and exits as a refusal (4).

Where it is projected (one function, `projectLogFacts`):

1. **The gate worker** (`/redact`): a device credential's `system_log/list` passes the check (`kind: read`) only in that exact shape (an added exception before `gateDenied`; `gateLogRead` and `rawLogRefusal` stay unconditional, so a `collect_events` envelope around it or an extra key stays refused). The worker rebuilds the whole answer envelope from the facts. It reads `manifest/list` on its own session; a failed read or an answer of unknown shape is no answer (the Relay withholds it, 502), never the raw log. So on an App with this worker `curl` with a paired credential gets facts too, never the log. Every other log read (`/api/error_log`, log events, Supervisor logs) stays refused.
2. **The CLI** (`ha-nova relay ws` and `ha_read`, the chat apps' read): the exact body is answered on a read session before the raw guard: it sends `system_log/list`; a list (the chat bridge, which the gate exempts; an App without the gate) is projected with `manifest/list` read on the same session; facts a 0.31 worker already built are checked field by field and rebuilt (projecting twice changes nothing); any other shape is an error. Every other shape of the request keeps hitting `rawLogRefusal`; raw plans still refuse it. On the hosted demo both routes refuse it (`mcpDemoRefusal`: the demo's log is other visitors').

Why the worker and not the Relay: the Relay stays transport (gate spec §2); the worker is the CLI's own code, which already redacts every device answer there. The App changes only through the CLI it bundles.

Compatibility: a 0.31 CLI on a 0.30 App gets the 0.30 gate's generic refusal; the CLI's fixed words then say an App older than 0.31 refuses the read and point to the log page. An older CLI on a 0.31 App still refuses the read itself (its own guard). An App without the gate (before 0.30) forwards a direct `curl` read unchanged, as it forwards everything: the guarantee holds on an App with this worker. No MCP tool text changes.

## Skill

`diagnose` → Logs: read log facts to see which integration fails and since when, say "since at least", treat a fact as a lead (an integration's code names its logger), and keep asking the user for the lines when the message would decide the diagnosis. API matrix, skill architecture, and safety rows follow.

## Accepted limits

- Attribution follows the logger name, which an integration's own code chooses: a custom integration can log under another integration's name and make it look failing. Facts are a lead, never a finding on their own (the skill says so).
- `system_log.write` lets anyone with service access write entries under any logger name (its default is `homeassistant.components.system_log.external`); they count only under a trusted domain and a fixed level, and can make an integration look failing.
- The worker's own loopback key could now read the raw list (the gate passes the exact read for every principal, and the Relay doesn't redact that key's answers). The worker never sends it, and that key can already read every other answer unredacted; not a device path.
- The output is an allowlist of values, not a channel bound: whoever controls a logger could encode bits in counts and times. Nothing from the log's text passes.
- Counts and times are what Home Assistant reports; a restart empties the list.

## Review record

- KISS (Sonnet): dropped source-path attribution (the logger name already attributes a frame in the integration's file; a library logger's path is no integration), the `unattributed` groups (one number instead), and an `OTHER` level. Kept the CLI projection: the chat bridge is exempt from the gate, so it needs it.
- Astra (gpt-6-astra, medium): partly agreed, two corrections applied: (P1) error envelopes and parser errors carry fixed words only, the worker rebuilds the whole envelope; (P2) raw entries and facts decode separately into one form, so projecting twice is stable (tested, also above 10⁹). Keep `gateLogRead` unconditional with a separate exact exception (done); a worker failure after the read reaches devices as 502.
- Adversarial reviews (Sonnet, privacy lens and sibling/UX lens): no P1, no leak found. Fixed: `relay ws` skipped the demo refusal (also Codex), the "older App" hint showed on every error and hid pairing words, diagnose's Flow step 3 still asked for lines first (also Codex), the refusal text named no route, domain length. Accepted: the worker key note above.

## Tests

- Go `cli/log_facts_test.go`: a poisoned list (passwords, URLs and injection text in messages and exceptions, a token as logger name, an automation logger with a secret suffix, a home path in `source`, an injection string as level, string, huge, negative, and fractional counts, bad times, extra keys, non-object entries) yields only trusted domains, fixed levels, numbers, and times, and no byte of any poison string; grouping, order, the capped sum; projecting twice; a gate's facts with a bad domain, a stray key, or a non-UTC time are refused with fixed words; the exact shape only. Gate: the exact read passes as `read`, an extra key, a duplicate key, `collect_events`, log events and `/api/error_log` stay refused; `/redact` projects, answers an error envelope with fixed words, and withholds an unknown shape or a failed `manifest/list`. `ha_read` and `relay ws`: facts from a list and from a gate's facts, fixed words on an error, refused shapes send nothing.
- Bed (`scripts/e2e/testbed/log-facts.mjs`, the bed's Relay runs the gate with this CLI): after `system_log.write` with a poisoned message under a demo logger, an automation logger with a secret suffix, and a secret custom logger, the Relay's own key and `ha-nova relay ws` get facts only (`demo`, `automation`, the rest unattributed), no poison byte; an extra key and a `collect_events` envelope stay refused.
