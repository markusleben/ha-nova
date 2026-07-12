# Spec: guided relay update from the CLI (stage 2)

Status: `draft` — approved direction (maintainer, 2026-07-12); implementation queued for a later version.

## Problem

Since v0.14.1 the CLI warns when the relay sits below `min_relay_version` (doctor, update, check-update, proxy header path), and since stage 1 the agent asks whether to install the App update via `ha-nova:updates`. But a user running plain `ha-nova update` in a terminal — outside any AI session — still has to switch to Home Assistant and click through the App update themselves.

## Goal

`ha-nova update` (and `doctor`, TTY only) offers to install the relay App update right there, with explicit consent — ask first, never automatic.

## Flow

1. After a successful self-update (or doctor run) the existing `relayFloorNotice` fires.
2. TTY + App-backed relay only: prompt `Install the relay update in Home Assistant now? [Y/n]` (reuse `promptWizardYesNoFromReader`).
3. On yes:
   - Resolve the App's `update.*` entity: `GET /api/states`, domain `update`, match the NOVA Relay App slug (the entity carries the App title; never guess — on ambiguity, print the manual path instead).
   - `POST /api/services/update/install` via the existing `/core` proxy with `{"entity_id": ..., "backup": true}` (partial App backup, mirrors `skills/updates/SKILL.md`).
   - Expect the relay to restart mid-call: tolerate the dropped response, then poll `GET /health` (existing `fetchRelayHealth`) every ~5 s for up to ~3 min until the reported version satisfies `min_relay_version`.
   - Report the verified version, or a plain timeout message with the manual path.
4. Non-TTY, `--yes`-style automation, or standalone container: keep today's warning only (a container cannot replace itself; ha-nova has no Docker-host access).

## Boundaries

- Ask-first always — an unprompted relay restart kills in-flight relay calls (project preview/confirm DNA).
- Client-side only: generic HA endpoints through the existing proxy; no new relay endpoint, no HA domain logic in the relay (charter).
- Detection of "App-backed vs container" comes from the update-entity lookup itself: no entity → treat as container/manual.

## Tests

- Entity resolution: exact match, no match (→ manual path), ambiguous match (→ manual path).
- Install flow with a mock relay that drops the install response, then serves the new version on `/health` (restart simulation).
- Poll timeout → honest failure message, exit code unchanged.
- Non-TTY and container paths stay warning-only.

## Out of scope

- "Relay update available" (above-floor) notifications — needs the current relay version in release metadata; separate decision, mainly benefits container users.
- Container auto-pull (Watchtower guidance stays documentation).
