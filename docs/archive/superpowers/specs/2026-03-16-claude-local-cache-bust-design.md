# 2026-03-16 Claude Local Cache Bust

## Problem

Local HA NOVA validation installs can still load stale Claude plugin skill files even when the local marketplace root points at the current checkout or install bundle.
The practical symptom is old macOS-only skill text such as `~/.config/ha-nova/relay` and `npm run onboarding:macos` showing up after a supposedly fresh local install.

## Root Cause

Claude caches plugin payloads under `~/.claude/plugins/cache/<marketplace>/<plugin>/<version>`.
In local validation mode we keep the plugin version at `0.1.12`, so Claude can keep serving the old cached payload even after the local marketplace source changes.

## Goal

Make local validation installs always refresh the Claude plugin payload.

## Non-Goals

- No change to the end-user GitHub marketplace flow
- No change to published plugin version semantics
- No new plugin architecture

## Design

- Detect local Claude marketplace mode (`HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1` or repo/dev install path).
- Before local install:
  - ignore-remove the installed plugin
  - delete the local cached plugin payload directory
  - clear stale installed plugin record if needed
- Then run `claude plugin install ha-nova@ha-nova` instead of `update`.

## Success Criteria

- Local validation installs do not reuse the stale `0.1.12` cache payload.
- GitHub marketplace installs still use the normal install/update flow.
- Regression tests cover local reinstall + cache cleanup.
