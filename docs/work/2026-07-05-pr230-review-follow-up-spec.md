# PR #230 Review Follow-Up Spec

Date: 2026-07-05

## Goal

Close the current Codex review findings on PR #230 without widening the release scope.

## Scope

- Keep `/ws` generic forwarding intact when Home Assistant commands contain fields named `message` or `collect_events`.
- Keep finite event collection available only through the explicit HA NOVA envelope.
- Make the local Home Assistant App deploy script delete stale remote `nova/src` files before copying the current source tree.

## Verification

- `git diff --check`
- `npx vitest run tests/http/ws-proxy.test.ts tests/app/deploy-script-contract.test.ts`
- `npm run verify:docs`
