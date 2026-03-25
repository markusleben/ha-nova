# 2026-03-23 Claude Stale-State Self-Heal

## Problem

Local repo/dev setups could keep `claude` in `~/.config/ha-nova/state.json` while the real Claude plugin records disappeared from:

- `~/.claude/plugins/installed_plugins.json`
- `~/.claude/plugins/known_marketplaces.json`

Effect:
- `ha-nova doctor` warns that Claude is configured but not attached
- `scripts/dev-sync.sh` skips Claude instead of repairing it
- users feel forced to reinstall Claude repeatedly

## Decision

Keep runtime truth strict, but let repo/dev sync self-heal this one stale-state case.

Rule:
- if `state.json` still lists `claude` but `installed_plugins.json` is missing or lacks an `installPath`, `scripts/dev-sync.sh` should reinstall the Claude plugin instead of skipping it
- if Claude cache metadata drifts, `scripts/dev-sync.sh` should keep operator hints on HA NOVA-owned repair paths (`npm run dev:sync`, `ha-nova setup claude`) instead of raw Claude CLI commands
- `ha-nova doctor` should print an explicit repair command instead of only warning about the detached state
  - bundle/end-user path: `ha-nova setup claude`
  - repo/dev path: `npm run dev:sync` or `ha-nova setup claude`

## Why

This keeps `doctor` honest while making the repo/dev maintenance path repair the most likely stale Claude drift automatically.
