# Codex Agent Live E2E Harness Design

Date: 2026-02-26

## Goal

Provide a real end-to-end automation test that behaves like a user session:
- run Codex non-interactively in the real environment
- require `ha-nova` skill usage for task execution
- perform a concrete Home Assistant automation CRUD scenario
- capture full machine-readable logs and evaluate pass/fail automatically

## Scope

In scope:
- add one script `scripts/e2e/codex-ha-nova-live-skill-e2e.sh`
- add npm shortcut
- add contract test + docs mention

Out of scope:
- CI-run against public cloud HA
- marketplace release automation

## Pass Criteria

The harness must fail loudly unless all are true:
1. onboarding doctor is healthy
2. Codex exec finishes successfully
3. log shows skill file usage evidence (`ha-nova` SKILL.md)
4. log shows CRUD evidence (`create`, `update`, `delete`, reload/verify)
5. final structured result marker indicates success

## KISS Decisions

- no nested frameworks; plain bash + `codex exec --json`
- no hidden fallback paths
- keep this as contributor validation (heavy) while `onboarding:macos:quick` remains daily fast check
