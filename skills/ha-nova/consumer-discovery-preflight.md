# Consumer-Discovery Preflight

Find everything that currently consumes an input device's events BEFORE repurposing
or cleaning it up. Companion to `skills/ha-nova/input-capability-preflight.md`: that
contract verifies what the device CAN emit; this one verifies what currently LISTENS.
Skipping it leaves an old mapping running or duplicates actions after a remap.

## Consumer Result Schema

Report every hit as one row:

- **source family** — which consumer class matched (automation, script, blueprint
  instance, extension adapter, ...);
- **reference** — the concrete item (entity_id or config id);
- **matched action** — the trigger/action of this device it consumes (normalized per
  the input-capability preflight's name normalization);
- **confidence** — direct (static trigger matches the device/entity/event exactly)
  or indirect (template, wildcard, or partial match that cannot be proven statically).

## Standard Families (always scanned)

1. **Automations & scripts** — `search/related` on the device and each of its
   entities (`item_type: device` / `entity`).
2. **Event-type consumers** — `search/related` does not index event listeners: scan
   readable automation configs for current (`trigger: event`) and legacy
   (`platform: event`) triggers with the same static `event_type` (e.g. `zha_event`),
   applying literal `event_data` filters (`device_ieee`, `command`). Templated event
   types and non-automation listeners are not safely enumerable — disclose that limit.
3. **Blueprint-backed automations** — instances are normal automations with
   `use_blueprint`; their triggers live in the blueprint, not the instance
   (`blueprint/list` returns only metadata, no triggers). Read the instance's
   `use_blueprint.path` and `use_blueprint.input` from its automation config, then
   expand the real trigger surface with
   `{"type":"blueprint/substitute","domain":"automation","path":"<path>","input":{...}}`
   and match the expanded triggers against the device/entities. If substitution
   fails (missing blueprint, rejected inputs), report the instance as an indirect
   match, never as cleared.

## Extension Adapter Contract

Consumers managed by extensions (their own storage, not the automation registry) are
scannable only through a documented adapter declaring:

- family name and the storage source it reads;
- read method (which relay read reaches it) — read-only, never a write;
- match semantics (how a device/gesture maps to that format);
- a confidence cap (an adapter result is never more than direct-with-named-source).

No adapters are registered yet. Known consumer-managing families without an adapter —
Node-RED flows, AppDaemon apps (e.g. ControllerX), HACS consumer managers — are
reported as **not checkable**, consistent with the External tier (fallback skill):
their storage is never parsed heuristically and never mutated. Mutating an
extension-owned configuration stays in that extension's own supported workflow.

## Coverage Report (mandatory)

Every discovery result names BOTH lists:

- families checked (with per-family hit counts, zero included);
- families not checkable (unregistered adapters, templated listeners, anything
  `search/related` does not index — dashboards and templates included).

While any family is not checkable, never claim the input is unused and never claim
complete cleanup. The strongest allowed claim: "no consumers found in the checked
families" — followed by the not-checkable list.

## Boundaries

- Discovery and interpretation live in this skill layer; the relay stays a generic
  transport and contains no extension-specific consumer logic.
- Discovery is read-only: never fire an event, toggle an entity, or open a listen
  window to find consumers.
- Output follows output-rules → Technical Noise: name families and matched items,
  not raw storage dumps.
