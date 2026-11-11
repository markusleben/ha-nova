# The App's own check reads a UI thermostat's heater (kind `probe`)

Status: active (built for 0.31, MAR-153)

Date: 2026-11-12. Owner: the gate worker (`cli/gate_verdict.go` `gateProbe`), the helper read (`cli/tier_helper_options.go`), the Relay (`nova/src/http/gate.ts`). High-risk: it widens what the App's own gate principal may send through the Relay.

## Problem

MAR-131 bounds what a UI `generic_thermostat`/`generic_hygrostat` calls by reading its heater: start the entry's options flow (`POST /api/config/config_entries/options/flow {"handler": <entry>}`), read the suggested value, abort it (`DELETE …/options/flow/<flow_id>`). On a gated App the worker rates through the Relay as its own principal, and the Relay forwards that principal's requests only on `pass` + `kind: read`. Both requests rated `kind: write`, so the Relay refused them, the call set stayed unknown, and every `call_service` listener counted: in a home with a UI thermostat and a listener whose action is typed or unreadable, every everyday call the App rated asked on the phone (0.30 known limit).

## Change

1. `gateVerdict` answers `kind: probe` (verdict unchanged) only when the verdict is `pass` and the request, as the Relay sends it (method upper-cased), is exactly:
   - `{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"<id>"}}`: exactly these keys, the path as written (no query or fragment), `handler` matching `flowIDPattern`, and, read live on the check's session, the entry listed by `config_entries/get` with an allowlisted domain and `supports_options`, that domain built in (`manifest/list`). `helperEntry` is that check; `helperOptions` runs the same function before its start.
   - `{"method":"DELETE","path":"/api/config/config_entries/options/flow/<id>"}`: exactly these keys, the id matching `flowIDPattern`.
   A failed session or read leaves `write`. Everything else is unchanged: a step POST (the save), a subentry or config flow, another integration's entry, a custom integration on the domain, an extra key.
2. `gate.ts` forwards the gate principal's request on `pass` with `read` or `probe`; `gate-worker.ts` accepts `probe`. Device verdicts don't change (that branch never reads `kind`). Worker and Relay ship in one App image; an older Relay refuses the unknown kind (fail closed).
3. No nested flow: when `gateVerdict` rates a core POST to the start path, the session it opens is marked, and `helperOptions` refuses on a marked session before sending anything. Depth is fixed: a check, the check of its start (live reads), plain reads (no reads).
4. Bounded cost: the start goes out only while 5 s of the session budget are left (after the entry check), on its own context with a 35 s timeout (the Relay's 20 s worker wait plus Home Assistant's 10 s), so a started flow's id arrives and is aborted even past the budget; the result of a read that outlasted the budget is not used (unknown). No retry.

## Accepted limits

- The gate principal may abort any options flow by id (the Relay keeps no state): at worst it ends someone's open options dialog unsaved. Loopback-only internal principal, not a device path.
- A start whose answer is lost (Home Assistant answered after the Relay's own 10 s timeout, or the link dropped) leaves one unsaved flow in memory until Home Assistant restarts; a failed abort likewise.
- A typed rating of the start (an automation on the thermostat's reload unlocks) refuses it: the set stays unknown, every listener counts (as in 0.30).

## Tests and lanes

- Go: `cli/gate_probe_test.go`: `probe` for the start and the abort; `write` for the save, other flows and entries, a custom integration, no options, extra keys, a query, an abort with a body, an unreadable entry list, no session, and a typed start; the start's own session refuses the read; the budget reserve; a worker-path test through a fake Relay that forwards only on `pass` + `read`/`probe` (as `gate.ts`): a light call beside a UI thermostat and an unlock listener on climate calls passes (typed without `probe`), a call on the thermostat stays typed, each flow aborted.
- TS: `tests/http/gate.test.ts` (gate principal: `probe` forwarded only on `pass`; a computer's `probe` is a plain pass), `tests/runtime/gate-worker.test.ts` (the kind is accepted).
- Bed: `scripts/e2e/testbed/calls.mjs` (run by `calm.mjs`, RC lane) on the gated bed: with the fan thermostat and an unlock listener on climate calls, a light call sent to the Relay passes with no ask, a call on the thermostat is held (428), and the entry stays as stored. Why the lane missed it in 0.30: its thermostat checks were the computer's own plans, never a request the gated Relay rated.

## Review

- Scope (HANO-45): Astra and Fable, A (ship 0.30 with the limit), both prefer `probe` over `read`.
- Build decisions (HANO-46, Astra medium): agrees with the detection, the session mark, `probe` in the gate branch only, and the abort limit. On the cost bound it disagreed with "always aborted" (a lost answer leaves a flow), so that is recorded above, and asked that a probe past the budget not be used (built).
