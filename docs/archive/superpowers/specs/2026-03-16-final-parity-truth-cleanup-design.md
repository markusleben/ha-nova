# 2026-03-16 Final Parity + Truth Cleanup

## Problem

After the larger Go-first onboarding/uninstall migration, a few smaller truth gaps remain:

1. Interactive setup still diverges from `origin/main` when both host and relay token are already supplied.
2. Claude marketplace refresh is riskier than needed on the normal GitHub end-user path.
3. Post-update client refresh trusts `state.json` too much and can miss real installed clients.
4. Some docs/contracts still describe the legacy shell onboarding or overstate current client/platform proof.
5. The onboarding skill still teaches an outdated `/health`-only WebSocket diagnosis rule.

## Goal

Close the remaining behavior-truth gaps with the smallest possible runtime and documentation changes.

## Constraints

- KISS
- DRY
- no new plugin framework
- no broad rewrite of the current Go runtime
- keep local-testing behavior explicit and separate from end-user GitHub marketplace behavior

## Decisions

1. **Host + relay-token flags skip LLAT walkthrough**
   - restore the old `origin/main` fast path
   - keep the LLAT guide for normal interactive first-run flows

2. **Safe Claude marketplace refresh**
   - on the normal GitHub path, do not pre-remove the marketplace if the existing registration is already correct
   - if the current marketplace source differs, do not destructively remove it before a replacement is proven
   - local validation mode may still use a stricter remove/reinstall path

3. **Update sync trusts reality, not only state**
   - refresh the union of `state.InstalledClients` and actually detected installed clients

4. **Truthful docs/contracts**
   - mark legacy macOS shell onboarding suites as legacy/reference-only
   - add an explicit Go-runtime contract suite
   - make README/support wording match the real validation matrix
   - update architecture docs to reflect the current Claude marketplace/plugin path

5. **Onboarding skill diagnosis truth**
   - `ha_ws_connected=false` alone is not enough
   - skill guidance must mention `/ws`/doctor fallback before blaming LLAT

## Non-Goals

- no client registry implementation in this pass
- no Windows helper architecture rewrite
- no automatic deletion of Claude project memory
