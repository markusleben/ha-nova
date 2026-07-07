# Claude Detach Root Cause + Fix Spec

Date: 2026-04-14

## Problem

On the real Mac, HA NOVA keeps getting detached from Claude again after a successful attach.

Observed live sequence from the drift audit:

1. `known_marketplaces.json` loses the `ha-nova` marketplace entry first.
2. `installed_plugins.json` then loses `ha-nova@ha-nova`.
3. `settings.json` loses the enabled plugin state.
4. Claude writes `.orphaned_at` inside the cached HA NOVA plugin tree.

This is no longer just a false-positive HA NOVA status bug. Claude is actually orphaning the HA NOVA plugin state.

## Working Hypothesis

The unstable piece is the local staged Claude marketplace source at:

- `~/.config/ha-nova/claude-marketplace`

Current bundle installs still prefer that local directory source for Claude. The real Mac evidence shows Claude first removing that marketplace source and then orphaning the plugin cache. Official Claude marketplaces on this machine use GitHub sources, not local directory sources.

## Goal

Make Claude installs stable for all users without adding a new background runtime or a second state model.

## KISS Fix Plan

1. Stop preferring the staged local Claude marketplace for normal bundle installs.
2. Keep local marketplace mode only for dev or explicit override.
3. Pin production Claude installs to the installed HA NOVA release tag, not a floating repo source.
4. Migrate existing bundle installs from the staged local source to the pinned GitHub marketplace source on the next repair/sync.
5. Keep the stricter attach verification:
   - plugin record must exist
   - marketplace record must exist
   - install path must be usable
6. Add regression coverage for:
   - bundle install uses pinned GitHub source
   - legacy local staged source migrates to pinned GitHub source
   - pinned GitHub refs stay distinct and preserved when intentionally requested

## Out of Scope

- No new daemon
- No product telemetry service
- No speculative auto-repair loop outside existing setup/update/sync paths
