# HA NOVA Skill Namespacing

Date: 2026-03-12

## Goal

Make HA NOVA sub-skills globally understandable in shared skill lists such as Codex `/skills`.

## Problem

- Generic sub-skill IDs like `helper`, `onboarding`, `read`, and `guide` are ambiguous outside the local project context.
- Open skill metadata guidance expects `name` to be a unique identifier and to match the parent directory.
- In shared/global discovery surfaces, HA NOVA sub-skills should be clearly grouped.

## Decision

- Rename sub-skill directories and `name` frontmatter to `ha-nova-*`.
- Keep the main context skill as `ha-nova`.
- Update all path references, dispatch references, installers, tests, and docs to the namespaced IDs.

## Scope

- `skills/*`
- `scripts/onboarding/install-local-skills.sh`
- `scripts/update.sh`
- `scripts/dev-sync.sh`
- `scripts/onboarding/macos-lib.sh`
- docs/tests that reference skill paths or skill IDs

## Non-Goals

- Changing the main plugin/package name `ha-nova`
- Reworking relay behavior
