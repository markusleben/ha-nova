# CLI Structure And Release Parity

Date: 2026-03-17

## Goal

Reduce maintainability risk before rollout without changing HA NOVA behavior.

## Problems

- `cli/commands.go` mixed setup, doctor, update, uninstall, bundle staging, and Windows helper code.
- `cli/clients.go` mixed install dispatch, Gemini rewriting, Claude plugin lifecycle, uninstall cleanup, and filesystem copy helpers.
- Prompt primitives existed twice for normal and wizard flows.
- Release workflows had Node-version drift and the active release path skipped signing while `.goreleaser.yml` still carried signing config.

## Approach

- Split Go CLI files by responsibility only.
- Keep function names and behavior unchanged.
- Remove dead wrappers that no longer have any callers.
- Align release workflows to the same Node lane as CI.
- Remove signing config that is unreachable in the active release workflow.

## Verification

- `gofmt`
- `cd cli && go test ./...`
- `cd cli && go test -race ./...`
- `npm run verify`
- targeted release/doc contracts
