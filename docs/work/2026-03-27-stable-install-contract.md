# Stable Install Contract

Date: 2026-03-27

## Goal

Make the public stable install path fully release-reproducible.

## Current Problem

- Public stable docs on `main` still teach `main/install.sh` and `main/install.ps1`.
- Those bootstrap scripts then install the latest release bundle.
- Result: floating bootstrap plus release-pinned payload.

## Decision

- Public stable install commands must be fully release-pinned.
- `main` docs become informational and point users to the latest GitHub release for the canonical stable command.
- Stable release notes must publish tag-pinned raw installer URLs plus matching `HA_NOVA_VERSION`.
- Installer runtime behavior stays unchanged.

## Scope

- `README.md`
- per-client install overlays
- `.goreleaser.yml`
- `docs/releasing.md`
- onboarding contract tests

## Non-Goals

- no installer runtime redesign
- no update semantics changes
- no stable alias service
