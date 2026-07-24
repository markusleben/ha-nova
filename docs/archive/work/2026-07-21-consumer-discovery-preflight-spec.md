# Consumer-Discovery Preflight Spec (#397)

Status: merged — #405
Date: 2026-07-21
Trigger: issue #397 — repurposing an input requires finding everything that consumes its
events; searching only standard automations misses blueprint-backed or extension-managed
consumers, so an old mapping keeps running or actions duplicate.

## Contract (new file `skills/ha-nova/consumer-discovery-preflight.md`)

Preflight before reassigning or cleaning up an input device's events (companion to the
input-capability preflight: #396 verifies what the device CAN emit, this verifies what
currently CONSUMES it).

- **Consumer result schema** (one row per hit): source family, reference (id/entity),
  matched action/trigger, confidence (direct match vs indirect).
- **Standard families (always scanned):**
  - automations & scripts via `search/related` on the device and its entities;
  - event-type consumers via the existing readable-config scan (`service-call` pattern:
    static `event_type` match + `event_data` filters; templated event types are
    disclosed as not safely enumerable);
  - blueprint-backed automations: instances are normal automations (`use_blueprint`) —
    read the blueprint's trigger surface and the instance's input bindings to decide
    whether this device/gesture is consumed. New capability.
- **Adapter contract for extensions:** a documented shape (family name, storage source,
  read method, match semantics, confidence cap) under which a known, stable extension
  format can be added to the scan. v1 registers **zero adapters** — named known-but-
  unsupported families (Node-RED flows, AppDaemon/ControllerX, HACS consumer managers)
  are reported as not checkable, per the standing External-tier policy (fallback skill:
  External = outside scope, never parsed). Unknown storage is never parsed heuristically
  and never mutated.
- **Coverage report (mandatory):** every result names the families checked AND the
  families not checkable. While coverage is incomplete, the response never claims the
  input is unused and never claims complete cleanup — "no consumers found in the checked
  families" is the strongest allowed claim.
- **Relay stays transport:** discovery and interpretation live in the skill layer; no
  relay change, no extension-specific logic outside this reference file.

## Edits

- `skills/write/SKILL.md`: extend the #396 gate line's context — repurpose/cleanup of an
  input routes through the consumer-discovery preflight; on-demand reference entry.
  Budget check (currently 1677/1700; ratchet with documented bump if needed).
- `skills/service-call/SKILL.md`: point the existing event-consumer scan at the shared
  contract (one line; budget 49 words headroom — ratchet if needed).
- `skills/ha-nova/fallback/SKILL.md` → `skills/fallback/SKILL.md`: one line anchoring the
  adapter contract at the External tier (extensions become scannable only via a
  documented adapter, never ad-hoc parsing).
- `skills/ha-nova/input-capability-preflight.md`: one cross-link line (companion doc).

## Tests (extend existing suites — no new file)

- `tests/skills/ha-nova-contract.test.ts`: pin the result schema fields, the three
  standard families, the blueprint input-binding capability, the adapter-contract shape,
  the zero-adapters/not-checkable naming, the never-claim-unused rule, and the
  no-relay-logic sentence.
- Fixture triple per issue acceptance: standard consumer / known extension adapter
  (the documented contract shape + named family) / unknown storage source (not-checkable
  reporting).
- Word-budget ratchets where tripped (documented attribution to #397).

## Non-goals

- No live e2e analyzer (same rationale as #396: no input devices in disposable HA).
- No mutation of extension-owned config — that stays in each extension's dedicated
  supported workflow (or the HA UI per External policy).
- No new relay endpoints.

## Verification

- `npx vitest run tests/skills/` green; pins fail on revert.
- Live read-only spot-check: `search/related` on a real button device + config scan for
  one known event type; verify a blueprint-backed automation's input bindings resolve.
- Side-work: safety.md guarantee row (verbatim test title), skill-architecture.md
  inventory entry, 0.20.0-release-body.md claim, breadcrumbs/choices update (includes
  the pending Batch-1 entries).
