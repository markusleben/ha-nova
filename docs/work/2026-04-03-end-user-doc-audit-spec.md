# End-User Doc Audit Spec

Date: 2026-04-03
Status: merged

## Goal

Audit the end-user-facing documentation surfaces for current `v0.3.2` product truth.

## Scope

- Check public install and client overlay docs.
- Check relay/operator docs that end users may read.
- Fix any stale public install guidance that still points to moving branch refs instead of the current stable install contract.

## Current Finding

- `nova/DOCS.md` still shows the moving `main/install.sh` path in the setup section.

## Fix

- Point `nova/DOCS.md` back to `README.md` and the latest GitHub release for the stable installer.
- Keep `ha-nova setup` as the path for users who already installed HA NOVA.
- Add a regression assertion so this drift does not come back.
