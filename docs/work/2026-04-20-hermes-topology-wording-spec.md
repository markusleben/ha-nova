# Hermes Topology Wording Spec

Date: 2026-04-20
Status: implemented

## Goal

Explain Hermes as a local-first HA NOVA client without overclaiming `LAN-only`, while making the current VPS mismatch and safe remote/private-path story clear.

## Scope

- tighten public README wording around network/privacy posture
- add Hermes-specific topology guidance in `.hermes/INSTALL.md`
- keep wording English, user-facing, and low-jargon
- avoid hard-promising Telegram or hosted remote access features

## Non-Goals

- no new runtime behavior
- no new public feature promise for Telegram or VPS hosting
- no topology deep dive in the main install flow

## Planned Changes

1. Soften the absolute README network claim into a local-first / no-cloud-relay statement.
2. Add one short advanced-note sentence in README that separates private-path remote access from public exposure.
3. Add a Hermes-specific network model section that explains:
   - local-first default
   - same LAN or private VPN/overlay as the practical path today
   - VPS as an advanced mismatch, not the intended default
   - remote entrypoints can later feed the same local executor model
4. Record the wording defaults in `docs/choices.md` and `docs/breadcrumbs.md`.

## Exit Criteria

- public docs no longer imply a brittle permanent `nothing leaves your network` promise
- Hermes docs explain local-first vs VPS in plain English
- docs leave room for future remote entrypoints without promising a specific Telegram/VPS roadmap
