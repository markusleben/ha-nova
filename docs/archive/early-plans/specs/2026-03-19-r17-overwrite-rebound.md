# Spec: R-17 Overwrite/Rebound Review Check

Date: 2026-03-19
Issue: `#92`

## Goal

Add one narrow review rule that catches a specific helper/entity write hazard inside one automation or script:
- the same target is written in multiple control-flow branches
- one branch advances live state incrementally
- another branch later recomputes or resets from a baseline/snapshot/fallback

## Non-Goals

- no relay or CLI changes
- no generic static analyzer
- no bulk review redesign
- no cross-automation conflict expansion

## Decisions

- New rule id: `R-17 [MEDIUM → HIGH]`
- Default severity: `MEDIUM`
- Escalate to `HIGH` only when later-branch overwrite/reset risk is concrete
- Rule applies only intra-config
- Collision scan must never be the trigger source for `R-17`

## Validation

- targeted repo contract tests
- mandatory local live skill validation on Markus's machine
- read-only real config first
- sandbox fallback allowed with `nova_test_rebound_*` helper-only artifacts
