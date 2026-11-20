# 2026-03-23 Winget Maintainer Proof Hardening

## Summary

Harden the staged `winget-pkgs` maintainer packet so it matches the real cross-host publish flow and does not overclaim validation.

## Required Changes

- Split published-source proof into:
  - initial public install/check-update/uninstall proof
  - later upgrade continuity proof once an older published version exists
- Move `ha-nova check-update` before uninstall in the initial proof
- Keep the first public Windows doc flip gated on the initial proof only
- Keep public `winget upgrade` wording conservative until upgrade continuity is actually proven
- Make the generated PR helper host-agnostic:
  - no baked `/Users/...` command paths
  - provide both bash and PowerShell command variants
  - derive the PR head owner from `FORK_REPO`
- Keep the PR body honest:
  - `winget validate` must not be pre-checked before maintainers actually run it

## Acceptance

- The staged maintainer checklist and PR body match the new proof split
- The generated commands file works as a cross-host handoff template instead of a same-mac-only script
- Release contract tests fail if the impossible proof order or premature validation claims return
