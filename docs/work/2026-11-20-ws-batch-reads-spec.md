# Batched loaded-config reads (MAR-180, PERF-02)

Status: shaped draft for the 0.34 train (HANO-72), after a Sol KISS pass. App bump: once, in the train's App promote PR.

## Problem
The listener census (`cli/tier_listeners.go` `listenerTable`) reads one loaded config per eligible automation or running script (`automation/config`, `script/config`, at most 2,000). Each read is one gated `POST /ws` exchange. With eight request slots and 110 configs, that is at least 14 waves of round trips. Over Home Assistant Link, a wave costs about 450 ms.

## Contract (loaded-config batches only)
- **Request:** `POST /ws` with the body `{"batch": [{"type": "automation/config" | "script/config", "entity_id": "<matching domain>.<id>"}, ...]}`.
  - The batch holds 1 to 16 items and has no other key.
  - Items are exactly `{type, entity_id}`.
  - The Relay refuses any other batch shape with `400 VALIDATION_ERROR` before the gate.
  - Generic WS batching, `collect_events`, blueprints, and `system_log/list` are out of scope.
- **Principals:** batches from internal principals (chat bridge, gate worker) are refused before `gated()`'s shortcuts. The CLI never batches for a gate-exempt caller.
- **Gate:** one `/check` on the envelope's original bytes; duplicate keys are refused by the existing walk.
  - The worker has a dedicated batch branch. It validates the item shape strictly and rates each item with the single-request verdict, opening no live session.
  - It answers only `pass` with `kind: read`, or refuse.
  - The Relay forwards a batch only on `pass` plus `read`. A batch never enters grant handling.
- **Relay:** it sends the items through the single `/ws` send and error mapping and answers `{"ok": true, "data": {"results": [...]}}` in item order. Each result is `{"ok": true, "data": ...}` or `{"ok": false, "error": {"code", "message"}}`, exactly as `POST /ws` answers that item alone. The worker redacts the envelope once.
- **Bounds:**
  - One admission limit across active batches, with no unbounded queue.
  - At most four items in flight per batch.
  - One deadline per batch that stops queued items when it expires or the client disconnects. It fits the CLI's per-exchange time.
  - An aggregate result-byte cap below the worker's input cap.
- **Capability:** `GET /health` carries `ws_batch: 1` only where the gated `/ws` is wired, in the App and the standalone container. Link uses the same handlers and needs no new route.

## CLI adoption
- The census probes `/health` once, on the selected transport, within the current read budget, and only when enough configs are unread to batch. The probe parses exactly `1`.
- It doesn't use `gateOn()`, which persists `gate_seen`.
- In the sorted census loop, chunks of 16 reserve memo keys through the single read's reservation (`ws <canonical json>`, the same in-flight channel). They never replace an existing entry.
- A chunk is published only after the result count and every item envelope check out. `relayCallError` codes, `UPSTREAM_WS_COMMAND_ERROR` included, are kept, so `answered()` and `isUpstreamNotFound()` behave as for single reads.
- Blueprint warm-ups keep running per chunk.
- A transport, gate, or HTTP failure of a batch leaves its items as unread census gaps, with no blanket retry.
- Without the flag, the CLI keeps single reads. It never batches while replaying.

## Proof
- Relay, worker, and CLI unit tests for shape, parity, the bounds, and the fallback.
- Census timing on the disposable bed, local.
- Link timing on the maintainer's Cloud lane.
