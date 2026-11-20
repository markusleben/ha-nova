# 2026-03-16 Claude Current Cache Layout Fix

## Problem

Local Claude validation on macOS fails during plugin reinstall with:

- `ENOTDIR: not a directory, rm '~/.claude/plugins/cache/ha-nova/ha-nova/0.1.12'`

The current cleanup only deletes the older nested cache layout, but current Claude installs cache HA NOVA directly under:

- `~/.claude/plugins/cache/ha-nova`

There is a second edge in the Go installed-bundle local-validation path:

- installed bundle roots contain a top-level regular file `ha-nova`
- Claude's directory-marketplace installer collides with that file and its cache path `.../cache/ha-nova/ha-nova/<version>`

## Goal

Make local Claude reinstall robust against the current real cache layout and the installed-bundle payload shape without adding a larger cache abstraction.

## Constraints

- KISS
- DRY
- preserve end-user GitHub marketplace flow
- local validation only needs small cache cleanup hardening
- keep backward compatibility with the older nested cache layout

## Decision

Treat `~/.claude/plugins/cache/ha-nova` as the canonical cache root to clear in local validation mode.

- delete that root during local reinstall cleanup
- in the Go installed-bundle local-override path, always stage the Claude marketplace payload under `~/.config/ha-nova/claude-marketplace/ha-nova`
- if that installed bundle root contains a top-level regular file `ha-nova`, exclude it from the staged plugin payload
- keep tests for both:
  - current direct-root layout
  - legacy nested layout

## Non-Goals

- no new Claude install strategy
- no new cache schema detection layer
- no end-user marketplace behavior changes
