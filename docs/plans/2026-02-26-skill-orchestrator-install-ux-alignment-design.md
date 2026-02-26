# Skill Orchestrator + Install UX Alignment Design

## Goal
Adopt proven patterns from `micromagnet-workshop` and `superpowers` to improve HA NOVA skill behavior and installation UX without adding runtime complexity.

## Scope
- Skill side:
  - make `skills/ha-nova.md` a strict orchestrator with mandatory onboarding-health gate
  - make `skills/ha-automation-crud.md` explicitly capability-aware (`HA_LLAT` direct REST vs App-context Supervisor path)
- Install UX side:
  - standardize install docs into one predictable structure for Codex and Claude Code
  - keep one-link-first onboarding and explicit verification/troubleshooting/security sections

## Design Rules
- KISS/DRY: no new runtime components, docs + skill contracts only.
- No backward-compatibility alias flows for removed launcher commands.
- App + Relay terminology only.
- English-only project content.

## Output
1. `skills/ha-nova.md`: strict gate rules + deterministic routing.
2. `skills/ha-automation-crud.md`: capability matrix + two execution paths.
3. `.codex/INSTALL.md`: normalized sections (`Quick Install`, `Verify`, `Troubleshooting`, `Security`).
4. `.claude/INSTALL.md`: same section layout as Codex doc with client-specific install command.
5. Contract tests updated for canonical install section layout.

## Verification
- `npm test -- tests/onboarding/macos-onboarding-script-contract.test.ts`
- `rg -n "## Quick Install|## Verify|## Troubleshooting|## Security" .codex/INSTALL.md .claude/INSTALL.md`
- `shellcheck scripts/onboarding/*.sh`
