# Input-Capability Preflight Spec (#396)

Status: merged — #404
Date: 2026-07-21
Trigger: issue #396 — a remap plan may assume a gesture (double-press, hold) that the
device/integration never emits; the resulting automation looks correct but can never fire.

## Contract (new file `skills/ha-nova/input-capability-preflight.md`)

Preflight required before planning or applying a remap of an input device
(button, remote, wall switch, dial — anything whose value is the events it emits).

- **Evidence classes** for every candidate gesture:
  - *advertised* — integration/device metadata lists it: `device_automation/trigger/list`
    (type/subtype rows), `event.*` entity `event_types` attribute, Z2M action metadata.
  - *observed* — a bounded, existing observation shows it fired: trace history, logbook,
    `event.*` entity state history, MQTT capture.
  - *assumed* — neither; the gesture merely sounds plausible.
- **Presentation binding:** reuse Claim-Evidence tiers (context skill) — advertised+observed
  agree → Verified tone; advertised-only or observed-only → Likely, marked; assumed →
  Uncertain, marked. An assumed gesture is NEVER described as supported or offered as a
  working option.
- **Mutation gate:** while the selected gesture is only assumed, the write flow is blocked
  (same hard-block shape as the BP stale+complex gate). Offer: (a) pick a supported
  alternative from the advertised/observed set, (b) live observation via User-Assisted
  Readiness (context skill sequence; MQTT devices use the bounded-window variant), or
  (c) cancel.
- **Normalization rule:** compare action names case-insensitively with separators stripped
  (`Single` / `single`, `double_press` / `double-press`); never equate names across
  different integration paths (a Z2M `action: single` is not evidence for a ZHA
  `command: toggle`) without an observation on the actual path.
- **Evidence matrix** (pinned by tests): metadata-only → Likely, offerable with marker;
  observation-only → Likely, offerable with marker; conflicting (metadata lists it, fresh
  observation contradicts, or vice versa) → Uncertain, blocked, explain the conflict;
  advertised+observed confirmed → Verified, proceed.
- **Worked example** (pinned by tests): a button advertising `single` and `double` but no
  `hold` — requested hold-remap is blocked, alternatives offered.
- Report evidence quality without dumping unrelated device data (output-rules Technical
  Noise rule applies).

## Edits

- `skills/ha-nova/relay-api.md`: new WS commands `device_automation/trigger/list`
  + `device_automation/trigger/capabilities` (device_id-keyed), `event.*` entity
  reads for `event_types`. Pass-through verified: `nova/src/http/handlers/ws-proxy.ts`
  rejects only subscription commands — no relay change. Contingency: if live verification
  finds a block, STOP; separate relay PR + spec addendum first.
- `skills/write/SKILL.md`: one Flow-intro gate line (pattern of the Multi-Target line):
  input-device remaps run the preflight before drafting; mutation blocked while the
  gesture is only assumed. Plus one On-demand reference entry. Word budget `write`
  ratchets 1700 → documented bump.
- `skills/ha-nova/best-practices.md`: cross-link from Zigbee Button Patterns (discovery
  is no longer manual-only: preflight first, Developer-Tools hint stays as fallback).

## Tests (extend existing suites — no new file)

- `tests/skills/ha-nova-contract.test.ts`: pin the write-flow gate line (same shape as
  the BP-gate pins), the preflight file's evidence matrix rows, the never-supported rule,
  the normalization rule, and the worked single/double/no-hold example.
- `tests/skills/skill-template-contract.test.ts`: WORD_BUDGETS ratchet for `write`
  (documented, attributed to #396).
- New `skills/ha-nova/*.md` file passes repo-wide lints (confirmation-code terminology,
  App wording, check-code allowlist — no allowlist growth expected).

## Non-goals

- No live e2e analyzer: the disposable-HA e2e environment has no physical input device;
  a gesture-remap scenario cannot run there honestly. Contract tests carry the coverage.
- No relay change; no new subscription-type WS usage (proxy rejects subscriptions).
- Consumer discovery of the input's existing automations is #397, not this issue.

## Verification

- `npx vitest run tests/skills/` green; new pins fail on revert of the skill edits.
- English-only check over changed skill files.
- Live spot-check against the real HA instance (read-only): `device_automation/trigger/list`
  for one Z2M button returns type/subtype rows through the relay.
- Mandatory side-work: `docs/reference/safety.md` guarantee row quoting the new test title
  verbatim; register the new reference file in `docs/reference/skill-architecture.md`
  inventory; append the user-facing claim to
  `docs/archive/work/0.20.0-release-body.md`.
