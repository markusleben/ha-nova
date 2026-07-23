# Spec: Above-Floor Relay Update Offer

Status: active
Date: 2026-07-16

## Problem

`ha-nova doctor`, `ha-nova update`, and human `ha-nova check-update` only compare the running Relay with `min_relay_version`. A compatible but stale Relay therefore looks current even when Home Assistant's exact `NOVA Relay` update entity reports a newer App version. Live acceptance reproduced this with client 0.18.0, Relay 0.4.1, floor 0.4.0, and App update 0.6.0.

## Decision

- Keep `min_relay_version` as the compatibility floor.
- After the floor check passes, join states with the entity registry and resolve
  exactly one `update.*` entity with `platform: hassio` and the NOVA Relay App
  update `unique_id`. Titles are display data, not provenance.
- Report an available update only when the entity is on, not in progress, and
  its valid `latest_version` is newer than its valid `installed_version`.
- Interactive `doctor` and `update` offer the existing guided App install only
  when install plus backup are supported. Non-interactive commands report the
  update without blocking. Human `check-update --quiet` now emits the exact
  Relay notice; `--json` remains unchanged and machine-clean.
- Missing, duplicate, malformed, or unreachable update-entity evidence stays silent; standalone Container/Core remains manual.
- After install, verify the Relay health version reached the offered target version and still satisfies the compatibility floor. Neither condition alone is sufficient.

## Acceptance

- Above-floor pending update: notice and interactive offer.
- Current update entity: no notice.
- Wrong-platform/title-only, duplicate, malformed, in-progress, or missing
  update entity: no guess and no notice.
- Below-floor Relay: existing outdated warning and failure semantics remain.
- Guided install waits for the offered target version and does not claim success while the old above-floor version still runs.
- Guided install does not accept a stale offered target that remains below the compatibility floor.
- Existing TTY, quiet, JSON, Windows-background, and standalone behavior remains covered.
