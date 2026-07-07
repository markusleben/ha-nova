# Promoted Skill Live Proofs

Date: 2026-04-03

## Goal

Close the remaining verification gap for the newly promoted `dashboard`, `organize`, and `history` skills with real Codex live proofs against the local HA NOVA setup.

## Scope

- add one dedicated live harness for the promoted skills
- cover the promoted lifecycle and delete-gate paths:
  - dashboard storage create + metadata update + config save + readback
  - dashboard delete with exact token and refusal on natural-language confirmation
  - organize category create + rename + entity assignment/unassignment
  - organize category delete with exact token and verified entity cleanup
  - bounded history + logbook timeline read
- add a contract test for the harness
- expose one npm script for contributor use

## Decisions

- Use a dedicated Python harness instead of overloading the generic scenario harness.
- Seed delete fixtures outside the assistant session when the proof needs a second-turn confirmation context.
- Keep dashboard writes storage-only and verify mode through `lovelace/dashboards/list`.
- Use category CRUD and entity category assignment instead of labels for the promoted organize proof.
- Keep the history live proof bounded and read-only; allow an empty logbook result as long as the bounded query is real and successful.
- Treat cleanup as mandatory proof behavior: stale promoted dashboards/categories from older failed runs must be removed before a new run, and every run must remove its own HA fixtures plus local temp output even on failure.

## Acceptance

- each scenario runs through `codex exec --json`
- transcript validation proves the intended relay calls happened
- dashboard lifecycle and delete state are read back and verified after the assistant run
- natural-language dashboard delete refusal performs no mutation
- category delete clears both the registry entry and the entity category mapping for that scope
- history proof shows bounded `/api/history/period` and `/api/logbook` calls for one real entity
- harness may write ephemeral result and summary artifacts during the run, but it must remove its temp output directory before exit
- no `nova_codex_*` dashboards, no `nova_codex_scope*` categories, and no related scoped entity-category mappings remain after the run
