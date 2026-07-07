# Promoted Live Suite Orchestrator

Date: 2026-04-05

## Problem

One long promoted live process becomes less deterministic as scenario count grows.
Single Codex hangs or late timeouts make the whole suite harder to trust and harder to clean up.

## Decision

Keep the current promoted live script as the single-scenario runner.
Add a thin suite orchestrator that:

- lists scenarios from the single-scenario runner
- runs each scenario in its own subprocess and output directory
- gives each child scenario and cleanup phase its own parent timeout
- kills timed-out child process groups instead of leaving orphaned runners behind
- performs promoted cleanup before and after the suite
- runs a final residue check against HA
- removes local suite temp output automatically on green runs

## Scope

- `scripts/e2e/codex-ha-nova-promoted-live-e2e.py`
- `scripts/e2e/codex-ha-nova-promoted-live-suite.py`
- `package.json`
- promoted live contract test

## Why

- KISS: one thin orchestrator, no new complex framework
- DRY: reuse existing scenario runner and cleanup logic
- better maintainability: isolate failures per scenario
