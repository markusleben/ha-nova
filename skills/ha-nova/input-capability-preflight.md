# Input-Capability Preflight

Verify what an input device can actually emit BEFORE planning or applying a remap.
Applies to any device whose value is the events it fires — buttons, remotes, wall
switches, dials, scene controllers. A remap planned against an unverified gesture
produces a configuration that looks correct but can never trigger.

## Evidence Classes

Classify every candidate gesture (single, double, hold, rotate, ...) internally:

- **advertised** — integration or device metadata lists it: a
  `device_automation/trigger/list` row (type/subtype), an `event.*` entity's
  `event_types` attribute, or Z2M action metadata for the device.
- **observed** — a bounded, existing observation shows it fired on this device:
  automation trace history, logbook entries, `event.*` state history, or a prior
  MQTT capture.
- **assumed** — neither source covers it; the gesture merely sounds plausible.

Presentation binds to the context skill's Claim-Evidence tiers:

| Evidence | Tier | Consequence |
|---|---|---|
| advertised + observed agree | Verified | proceed normally |
| metadata-only (advertised, never observed) | Likely | offerable, marked "Based on device metadata, this likely works" |
| observation-only (observed, not in metadata) | Likely | offerable, marked with the observation as evidence |
| conflicting (one source contradicts the other) | Uncertain | blocked — explain the conflict, offer live observation |
| assumed | Uncertain | blocked — never described as supported |

An assumed gesture is never presented as a working option, never in the same tone
as verified, and never silently planned around.

## Discovery Sources

1. Resolve the device (`config/device_registry/list`, `config/entity_registry/list`).
2. Advertised set: `device_automation/trigger/list` for the device_id; for
   event-entity devices read the `event.*` entity's `event_types` attribute
   (`/api/states/{entity_id}`). See relay-api.md → Device Trigger Queries.
3. Observed set: bounded reads only — existing traces of automations already
   listening to the device, logbook window, `event.*` state history. Do not open
   listen windows or fire anything during discovery.

## Name Normalization

Compare action names case-insensitively with separators stripped: `Single`,
`single`, `double_press`, `double-press` normalize before comparison. Never equate
names across different integration paths — a Z2M `action: single` is not evidence
for a ZHA `command: toggle`; the active integration path may expose fewer actions
than generic device documentation claims.

## Mutation Gate

While the selected gesture is only assumed or conflicting, the remap write is
blocked (same hard-block shape as the best-practice stale+complex gate). Offer:

1. a supported alternative from the advertised/observed set (continue after the
   user selects it);
2. live observation — only via context skill → User-Assisted Readiness (MQTT-path
   devices use the bounded-window variant in `ha-nova:mqtt`); an empty window after
   the user acted means re-arm and retry once, not a capability verdict;
3. cancel.

After a live observation confirms the gesture, it is observed evidence; proceed.

## Worked Example

A wall button advertises `single` and `double` but no `hold`. A requested
hold-remap is blocked: "This button does not advertise a hold action (advertised:
single, double). I can remap single or double, observe the device live to check
for hold, or cancel." Never create the hold automation on spec.

## Companion

Repurposing or cleaning up an input also needs the consumer side: what currently
listens to these events is `skills/ha-nova/consumer-discovery-preflight.md`.

## Output

Report evidence quality without dumping unrelated device data (output-rules →
Technical Noise). Name the checked sources in one line; internal class labels
(advertised/observed/assumed) stay internal — the user sees the tier wording.
