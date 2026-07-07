# Claude Drift Evidence Capture Spec

Date: 2026-04-14

## Goal

Capture durable evidence when Claude detaches HA NOVA again, without adding a new daemon system inside the product.

## Scope

- Reuse the existing Claude state helper.
- Add a compact machine-readable snapshot of the relevant Claude registry files.
- Add a small local watcher script for macOS using `fswatch`.
- Start the watcher on the current Mac after implementation.

## Files To Track

- `~/.claude/plugins/installed_plugins.json`
- `~/.claude/plugins/known_marketplaces.json`
- `~/.claude/settings.json`
- `~/.claude/settings.local.json`

## Output

- append-only `events.jsonl`
- rolling `latest.json`
- enough metadata to answer:
  - which file changed first
  - what the Claude attach state looked like before/after
  - whether plugin / marketplace / settings drifted together or separately

## Non-Goals

- no long-running product daemon
- no OS-wide audit subsystem
- no guessing which external process changed the file

## Success

- We can start one command locally and keep collecting Claude drift evidence.
- The watcher output stays simple JSON so later analysis is easy.
