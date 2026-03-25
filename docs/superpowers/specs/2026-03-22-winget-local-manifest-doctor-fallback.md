# Spec: Windows Local `winget --manifest` Inventory Fallback

Date: 2026-03-22

## Problem

Private Windows RC tests use `winget install --manifest ...`.

That install path can appear in `winget list` as `ARP\\...__DefaultSource` instead of a published `--source winget` package record.

Current HA NOVA behavior assumes every winget-managed install is visible through `winget list --id markusleben.ha-nova --exact --source winget`.

Result:

- `ha-nova doctor` can warn even though the runtime is healthy
- the warning reads like a broken product install during private QA

## Goal

Keep the public `winget` contract strict, but make private local-manifest QA read cleanly.

## Non-Goals

- Do not weaken the final published-source release gate
- Do not pretend local-manifest installs prove public `winget upgrade` behavior

## Decision

1. Query published-source inventory first.
2. If that misses, query generic `winget list ha-nova`.
3. If generic inventory finds a local-manifest/ARP install:
   - treat it as installed for `doctor`
   - report it as `winget_local_manifest`
   - do not claim published-source update truth
4. Keep published-source update availability checks only for real `--source winget` matches.

## Expected UX

- Private `winget --manifest` test:
  - `ha-nova doctor` stays green
  - output includes a short info line for local-manifest inventory
- Public published package:
  - `check-update` and `doctor` still use real package-source truth
