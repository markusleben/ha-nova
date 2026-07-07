# Claude Release Snapshot Implementation Spec

Date: 2026-04-14

## Problem

The earlier Claude fix path moved bundle installs to GitHub-backed marketplace sources.

That reduced one detach trigger, but it did not solve the user's original release-integrity requirement:

- the installed Claude source could still drift away from the shipped HA NOVA release
- later `main` commits could become visible through the marketplace source shape
- the old flat local root and the new GitHub path did not give one exact release snapshot target

## Decision

Bundle installs now treat Claude like a release-pinned local artifact again, but not through the old flat root.

Production target:
- `~/.config/ha-nova/claude-marketplace/releases/vX.Y.Z`

Dev-only target:
- `~/.config/ha-nova/claude-marketplace`

Legacy GitHub marketplace forms remain supported only as migration inputs.

## Implementation

1. Bundle installs build and register the exact versioned local snapshot path.
2. Dev installs and explicit `HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1` continue to use the flat dev root.
3. Claude health now requires:
   - marketplace record present
   - plugin record present
   - usable `installPath`
   - expected marketplace source match
4. Detection and repair treat legacy GitHub or flat-local Claude states as repairable, not healthy.
5. Bundle installs no longer fall back to GitHub when the local Claude payload is missing; that is now a loud failure.

## Verification

- Go regressions cover:
  - bundle default -> versioned local snapshot
  - legacy GitHub migration -> versioned local snapshot
  - configured-Claude repair when marketplace record is missing
  - bundle payload missing -> hard failure
  - old flat local marketplace root -> not healthy
- Vitest contract lanes cover:
  - repo/dev Claude path still uses the flat local root
  - dev sync still repairs Claude
  - desktop validation contract still reflects explicit local override for private RC/dev flows
