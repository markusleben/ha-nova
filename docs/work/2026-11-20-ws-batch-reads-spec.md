# Batched loaded-config reads (MAR-180, PERF-02)

Status: active (built for the 0.35 train (HANO-78), rechecked against release/0.35 (20890e7e) after the 0.34 Relay slimming and MAR-262, KISS-shaped by gpt-6.2-sol, high). App bump: once, in the train's App promote PR.

## Problem
The listener census (`cli/tier_listeners.go` `listenerTable`) reads one loaded config per eligible automation or running script (`automation/config`, `script/config`, at most 2,000). Each read is one gated `POST /ws` exchange. With eight request slots and 110 configs, that is at least 14 waves of round trips. Over Home Assistant Link, a wave costs about 450 ms.

## Contract (loaded-config batches only)
- **Request:** `POST /ws` with the body `{"batch": [{"type": "automation/config" | "script/config", "entity_id": "<matching domain>.<id>"}, ...]}`.
  - The Relay checks the envelope before the safety check (`gated()`'s `prepare`): `batch` is the only key and holds 1 to 16 objects with a string `type`; anything else with a `batch` key is `400 VALIDATION_ERROR`. Other `/ws` bodies are unchanged.
  - The worker owns the item shape: exactly `{type, entity_id}`, one of the two types, an entity id of that domain.
  - Generic WS batching, `collect_events`, blueprints, and `system_log/list` are out of scope.
- **Gate:** one `/check` on the envelope's normalized bytes; duplicate keys are refused by the existing walk.
  - The worker's batch branch checks the shape and rates each item as `POST /ws` alone, with no session. It answers `pass` with `kind: read` only when every item does, else refuse.
  - The Relay forwards a batch only on `pass` plus `read`; a batch never enters grant handling.
  - The App's own keys (chat bridge, gate worker) never send a batch: the Relay refuses it before their shortcuts (the bridge skips the check, the worker's answers aren't redacted). The CLI doesn't batch for them (`gateExempt()`).
- **Relay:** it sends the items one after another through the single `/ws` send and answers `{"ok": true, "data": {"results": [...]}}` in item order. Each result is `{"ok": true, "data": ...}` or `{"ok": false, "error": {"code", "message"}}`, exactly as `POST /ws` answers that item alone. The worker redacts the envelope once (key-based, it walks nested arrays and objects).
- **Bounds:**
  - Items go one after another, so upstream concurrency stays at most the CLI's eight request slots, as with single reads.
  - A batch has 10 s in all: an item still waiting, or still unanswered, when the time is up answers `UPSTREAM_WS_TIMEOUT`, and once the client's connection closes no further item is sent. The CLI's exchange allows 15 s, which leaves the check and the redaction their time.
  - The CLI sends a batch once (no R4 resend), so a resend never repeats reads still under way.
  - No byte cap of its own: 16 configs buffer no more than the eight single reads in flight today, and the worker's `/redact` cap withholds an oversized answer (502, a failed read).
- **Capability:** `GET /health` carries `ws_batch: 1` with `gate: 1`, on every Relay whose `/ws` is gated (App and standalone container). Link uses the same handlers and needs no new route.

## CLI adoption
- The census hands its sorted config reads to the session. With at least two of them, the session reads `/health` once on the selected transport (not `gateOn()`, which persists `gate_seen`) and batches only on exactly `ws_batch: 1`.
- It reserves the memo keys the single read uses (`ws <canonical json>`, `loadedConfigRequest`) for the reads no earlier read made, and sends chunks of 16 through the session's request slots. The census loop then reads as before and finds the answers in the memo; blueprint warm-ups, replay, and the place index are unchanged.
- A chunk's answers are published only after the result count and every item envelope check out. Item errors become `relayCallError` (502) with the item's code and message, `UPSTREAM_WS_COMMAND_ERROR` included, so `answered()` and `isUpstreamNotFound()` behave as for single reads.
- A transport, gate, or HTTP failure of a batch is a plain failed read of each of its items, never `not_found` (census gap, typed), with no retry.
- Without the flag, for the App's own callers, or while replaying a cut preview, the CLI keeps single reads.

## Compatibility
- New CLI, previous App: no `ws_batch`, so single reads.
- A batch that reaches a previous App anyway: its worker refuses the unrecognized shape (`GATE_REFUSED`), and its `/ws` requires `type`, so nothing is sent; a census gap makes the call typed, never ordinary.
- Previous CLI, new App: never sends a batch.

## Shaping decisions (Sol, 23.11.)
- Kept from the review: the internal-principal refusal, `ws_batch` tied to `gate`, the item shape owned by the worker, no R4 resend, stop on client close, one deadline the whole batch fits in.
- Not taken: one batch at a time per census (it would turn one wave into seven round trips; parallel chunks keep today's eight-in-flight bound), and a Relay-wide admission cap (single reads have none either; a batch never raises a client's upstream concurrency above its eight slots).

## Proof
- Relay, worker, and CLI unit tests for shape, parity, the deadline, and the fallback.
- Census timing on the disposable bed, direct and behind a delay proxy.
- Link timing on the maintainer's Cloud lane.
