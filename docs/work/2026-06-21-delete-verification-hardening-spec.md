# Delete Verification Hardening Spec

Status: active

Date: 2026-06-21

## Problem

The live delete test followed the destructive confirmation gate correctly, but two execution details were still brittle:

- Relay payload handling used shell-generated scratch directory commands instead of the client-native file workflow documented elsewhere.
- A deleted automation made `config/entity_registry/get` return an upstream WebSocket error after deletion. The agent handled this manually, but the expected absence path was not documented clearly enough.

## Goals

- Keep destructive delete confirmation unchanged: typed token only.
- Make payload/result file handling less shell-specific.
- Document post-delete absence evidence so agents do not retry deletes or treat expected not-found responses as failures.
- Keep the Relay dumb; no new Relay endpoint or special case.

## Non-Goals

- No change to write confirmation semantics.
- No new helper script or normalizer.
- No broad rewrite of `skills/write/SKILL.md`, which is already near its word budget.

## Implementation

- Add a relay file-handling rule to the shared Relay contract.
- Add the same execution constraint to the Apply Agent template.
- Add explicit delete verification rules:
  - config read-back not-found is absence evidence,
  - runtime state not-found is absence evidence when an entity exists,
  - entity-registry get may return an upstream WebSocket error after deletion,
  - use registry display search only as optional extra evidence.
- Add contract tests for the new rules.

## Verification

- Focused Vitest for delete safety and HA NOVA relay contracts.
- Docs verification.
- Safe core skill tests.
- Dev sync after passing tests.
