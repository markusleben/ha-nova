# Contributor Runtime Contract Cleanup Design

## Goal

Align contributor workflow, dev helpers, tests, and docs with the Go-first hard-cut runtime so the repo tells one clear story.

## Decision

- Product contract stays narrow: `install.sh`, `install.ps1`, `ha-nova ...`, legacy cleanup scripts.
- Contributor contract becomes explicit: Node tests plus Go CLI tests are both required.
- Shell-era helpers may remain for dev/smoke/e2e support, but they must be clearly dev-only and must not define the public product contract.
- `package.json` should stop advertising shell-era onboarding aliases as if they were normal entrypoints.

## Scope

- Update contributor docs and install docs where the active verification flow changed.
- Update `package.json` scripts to expose the current contributor verification path.
- Update dev/smoke/e2e scripts that still read `onboarding.env` or call shell onboarding as if it were the supported runtime.
- Update stale tests so they either:
  - verify the current product contract, or
  - explicitly verify dev-only helper behavior.

## Non-Goals

- No new end-user features.
- No legacy migration path.
- No repo-wide deletion spree of all shell scripts in this cut.
