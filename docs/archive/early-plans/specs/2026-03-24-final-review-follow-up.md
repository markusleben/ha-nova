# 2026-03-24: Final Review Follow-Up

## Scope

- Close the last current-SHA PR review findings before merge.

## Decisions

- Windows uninstall recovery may fall back from `--purge` to standard remove only when the failed step was `token_cleanup`.
- Reason: purge-only token cleanup can fail for external keyring reasons, but users still need a clean path to remove the runtime.
- Keep `doctor`/installer messaging conservative; the primary suggested recovery command stays mode-aware from the stored marker.
- `resolveWingetBundleRoot()` must still scan package roots when the WinGet link is missing.
- Reason: post-update sync needs a working fallback when the link is temporarily absent but the package root is still present.

## Verification

- Add focused Go regression tests for:
  - purge-token failure allowing standard recovery
  - winget bundle-root fallback without the link path
