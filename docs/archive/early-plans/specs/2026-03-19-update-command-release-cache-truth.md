# Update Command Release Cache Truth Spec

Date: 2026-03-19

## Goal

Keep `ha-nova update` aligned with the actual latest GitHub release, even when the shared release cache still points to an older tag from earlier the same day.

## Problem

- The runtime stores `~/.cache/ha-nova/latest-release.json` for 24h.
- `ha-nova update` reused that cache as if it were authoritative.
- When `v0.2.1`, `v0.2.2`, and `v0.2.3` ship within the same TTL window, a machine that cached `v0.2.1` can later print `Already on newer version v0.2.3 than target v0.2.1`.
- GitHub `releases/latest` is correct in that state; the misleading target comes from local cache reuse, not from the release workflow.

## Required Fix

1. Keep the shared cache for passive notice surfaces such as `check-update` and SessionStart.
2. Make `ha-nova update` bypass the cached latest-release file and resolve `releases/latest` directly when no explicit `--version` is passed.
3. Add a regression test that seeds a fresh `0.2.1` cache, mocks GitHub `latest = 0.2.3`, and proves `ha-nova update` no longer reports `target v0.2.1`.

## Why This Is The Smallest Safe Fix

- No release-flow change.
- No installer-flow change.
- No cache format change.
- The user-facing update command stops lying about the target version, while cheap read-only notice paths can still reuse the cache.
