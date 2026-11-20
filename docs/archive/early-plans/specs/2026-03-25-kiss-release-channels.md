# 2026-03-25: KISS Release Channels

## Summary

Keep HA NOVA on one public channel plus explicit prerelease pins:

- stable: `vX.Y.Z`
- rc: `vX.Y.Z-rcN`

Do not add a persisted preview/stable channel toggle.
Do not turn RC into a first-class public support lane.

## Decisions

- Normal install and normal `ha-nova check-update` / `ha-nova update` always target stable.
- RC selection stays explicit via exact version pin only:
  - `HA_NOVA_VERSION=vX.Y.Z-rcN`
  - `ha-nova update --version vX.Y.Z-rcN`
- Return an RC install to stable with plain `ha-nova update`.
- Only stable releases are candidates for public `winget` rollout.
- RC releases never require `winget`.
- Stable Windows docs must keep exactly one recommended user path; until public `winget` is published and proven, that path stays `install.ps1`.
- On Windows, the installer path is the clearest RC test path.

## Reason

This is the smallest model that:

- supports frequent early skill releases
- avoids `winget` churn for every RC
- avoids persistent channel state in the product
- keeps stable docs simple
- keeps support load low when users move between bundle installs and published Windows packaging
