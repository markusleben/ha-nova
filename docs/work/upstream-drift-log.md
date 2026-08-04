# Upstream Drift Log

Status: active

Append-only record of upstream-release screenings against skill-pinned
surfaces (rule: AGENTS.md → Research → Upstream drift check). Every run is
logged — hits AND clean passes — so the next run knows where the last one
stopped.

## 2026-08-05 — HA 2026.7 + HACS 2.0.5 (initial entry)

- Window screened: HA 2026.7 release post (2026-07-01) + frontend PRs
  #52215/#52353/#52530; HACS releases through 2.0.5.
- HIT — HA 2026.7 "Update all": frontend-only per-group button; ONE
  multi-target `update.install` (entity_id list, no new API), concurrent,
  sets NO backup flag, first-failure-only reporting, never includes
  core/OS/Supervisor; HACS update entities participate. Modeled in
  `skills/updates/SKILL.md` (mirror the guardrails, deliberately not the
  call shape) via PR #506.
- CLEAN — HACS 2.0.5 WS command surface matches the pinned map in
  `skills/hacs/hacs-commands.md` (source-verified 2026-08-04 and
  runtime-verified read-only against the reference instance).
- Next window starts WITH HA 2026.8 (everything after the 2026.7 post and HACS 2.0.5 is unscreened).
