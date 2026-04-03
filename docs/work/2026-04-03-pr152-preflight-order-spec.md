## Spec

- Scope: finish PR #152 by fixing current Codex findings on the generic scenario harness.
- Problems:
  - the generic scenario harness marks `doctor` / `relay health` as prohibited preflights even when they happen after the first `relay ws/core` action
  - the write-review proof accepted `/api/states/automation.<id>` instead of the required target-entity state read
  - rule-code marker regexes were broad enough to hit dotted entity IDs like `sensor.h2`
- Decisions:
  - only classify doctor/health calls as preflight violations when they occur before the first Home Assistant action
  - require the post-write state read to target the configured collision entity only
  - narrow rule-code detection so dotted entity IDs are not treated as internal check shorthands
- Verification:
  - targeted Vitest contracts for preflight ordering, rule-code detection, and post-write proof parsing
  - targeted `npm run verify`
  - re-request Codex review on the new SHA before merge
