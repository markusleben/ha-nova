# Final Review Hardening Design

Date: 2026-03-15

## Goal

Close the last correctness and contract gaps found by the full branch review before any release claim:

- normalize non-interactive setup like the interactive wizard
- make wizard back-navigation work with `--relay-token`
- prevent uninstall from removing unrelated user PATH edits
- remove stale Claude validation state from Windows dev cleanup
- make Claude install recover from stale `installed_plugins.json`
- fix invalid Windows relay fallback packaging in dev installer
- make local validation harness truthfully reflect served assets
- stop docs/contracts from over-promising Windows client support
- remove obsolete legacy update wording

## Constraints

- Keep changes small and local
- No new platform abstraction layers
- Preserve Go-first runtime direction
- Keep existing update principle for production Claude installs

## Acceptance

1. `npm run verify` passes
2. `cd cli && go test ./...` passes
3. Full-review findings above are either fixed or intentionally documented with a narrower contract
4. Another multi-agent review over the delta returns no findings
