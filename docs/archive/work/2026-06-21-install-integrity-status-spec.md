# Install Integrity Status Spec

Status: active

## Problem

`ha-nova check-update` reports update availability, but support/debugging now needs a side-effect-free install-integrity view after the v0.6.2 update-cleanliness work. Users can have:

- current runtime and install-root metadata
- stale state markers
- active client roots that still point at transient update backups
- inactive legacy/dev artifacts that should not be treated as active drift

## Scope

- Add a side-effect-free JSON status command.
- Report binary/build version, bundle metadata, install-root `version.json`, state version, `clients_verified_version`, active client roots, and stale inactive artifacts.
- Keep `check-update` focused on update state.
- Teach `doctor` to distinguish active configured-client drift from inactive legacy/dev artifacts.

## Out Of Scope

- Home status and calendar skills.
- Dependency cleanup or manifest edits.
- Artifact removal/cleanup actions.

## Acceptance

- `ha-nova status --json` prints machine-clean JSON and writes no state.
- Active configured clients with transient-backup residue are reported as active drift.
- Known inactive legacy/dev artifacts are reported separately and do not fail `doctor`.
- Dev builds do not run self-heal through the new status command.
- Focused Go tests cover JSON shape, active-vs-inactive classification, and the no-side-effect path.
