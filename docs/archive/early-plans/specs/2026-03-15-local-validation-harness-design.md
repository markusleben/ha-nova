# Local Validation Harness Design

## Goal

Provide one tiny developer entrypoint for manual local install validation on macOS and Windows.

## Scope

- rebuild fresh local install bundles by default
- serve the repo root on a stable local port so `install.ps1` and `dist/install-bundles/*` are both reachable
- optionally start the tiny fake Home Assistant + fake relay `/health` mock
- print exact copy/paste install commands for:
  - macOS
  - Windows

## Non-Goals

- no automation of the interactive wizard itself
- no GUI automation
- no stop/daemon manager beyond normal `Ctrl-C`

## Design

- add `scripts/dev/start-local-validation-harness.sh`
- keep it foregrounded; background only its child servers
- trap `EXIT/INT/TERM` and stop child processes
- default behavior:
  1. `npm run release:rc:local`
  2. start bundle server on `:8917`
  3. print install commands
- optional `--with-mock`:
  - start `scripts/dev/mock-ha-relay.py`
  - print mock URLs too
- optional `--no-build`:
  - reuse current `dist/install-bundles`

## Why This Shape

- prevents stale-bundle mistakes
- avoids repeating the same server/URL/manual setup steps
- stays small enough that developers still test the real userflow manually
