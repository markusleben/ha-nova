# Promoted History Non-Numeric Guard

Date: 2026-04-05

## Problem

The promoted history timeline proof can touch real sensors whose bounded history contains non-numeric states such as `unknown`.
Blind `jq` reductions like `min_by(.state|tonumber)` fail even though the read itself is valid.

## Decision

Keep the default proof simple:
- first/last
- count
- broad trend
- logbook presence

Do not require raw-series numeric min/max. If numeric reductions are ever needed later, they must explicitly filter to numeric states first.

## Scope

- `skills/history/SKILL.md`
- `scripts/e2e/codex-ha-nova-promoted-live-e2e.py`
- one contract assertion
