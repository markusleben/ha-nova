# PR #124 Follow-up Hardening

## Scope

- Align the release contract tests with the current `winget` handoff policy.
- Keep the safer Windows uninstall recovery semantics from the latest review.
- Push the corrected delta to the clean replacement PR branch and restart the fast review cycle on the new SHA.

## Decisions

- Treat RC `winget` artifacts as rehearsal-only.
- Treat the exact final stable GitHub release asset as the only valid source for a real public `winget-pkgs` submission.
- Keep purge-token-cleanup recovery strict: a failed purge still requires purge recovery.
- Keep corrupt Windows uninstall markers strict: default to standard recovery guidance instead of silently accepting purge.

## Verification

- `npx vitest run tests/onboarding/release-contract.test.ts`
- `cd cli && go test ./... -run 'TestRunUninstallBlocksStandardRecoveryAfterPurgeTokenFailure|TestRunUninstallBlocksPurgeRecoveryWhenMarkerIsCorrupt|TestRunWingetUninstallUsesPurgeFlags'`
- `git diff --check`
