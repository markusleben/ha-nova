# Claude Real-Machine Test Spec

Date: 2026-04-13

## Goal

Verify the merged Claude attach-verification fix on the real macOS machine without depending on a new public release first.

## Scope

- Build a clean CLI binary from merged `origin/main`.
- Keep the user's current Claude + HA NOVA state recoverable.
- Exercise the real local Claude attach / doctor / repair path.
- Confirm whether the stricter verifier catches detached or half-attached Claude state on this machine.

## Safety

- Snapshot relevant user state before any mutation:
  - `~/.claude/plugins/`
  - `~/.claude/settings.json`
  - `~/.claude/settings.local.json`
  - `~/.config/ha-nova/`
- Build from a clean worktree, not from the dirty repo checkout.
- Do not remove user data unless the test explicitly needs it.

## Plan

1. Capture current local state and installed versions.
2. Create a clean worktree at merged `origin/main`.
3. Build a standalone test binary from that exact SHA.
4. Run non-mutating checks first:
   - `doctor`
   - `version`
   - `claude plugin list`
   - `claude plugin marketplace list`
5. Run the smallest repair path that exercises the fix:
   - `setup claude`
6. Re-check doctor/plugin/marketplace/version state.
7. Record result and next release implication.

## Success

- The test binary runs against the real user home.
- Claude attach state is either:
  - confirmed healthy, or
  - detected as detached/half-attached and repaired cleanly.
- We end with a clear answer whether the fix changes real-machine behavior.
