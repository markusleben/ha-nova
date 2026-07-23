# Spec: guided relay update from the CLI (stage 2)

Status: `implemented` (2026-07-12) — `cli/relay_guided_update.go`; ships with the next user-facing release.

## Problem

Since v0.14.1 the CLI warns when the relay sits below `min_relay_version` (doctor, update, check-update, proxy header path), and since stage 1 the agent asks whether to install the App update via `ha-nova:updates`. But a user running plain `ha-nova update` in a terminal — outside any AI session — still has to switch to Home Assistant and click through the App update themselves.

## Goal

`ha-nova update` (and `doctor`, TTY only) offers to install the Relay App
update there, with an observed preview and explicit consent to install the
latest available version — never automatic. Home Assistant's Supervisor App
update service cannot bind the install to a specific target version.

## Flow

1. After a successful self-update or doctor run, a Relay warning fires for a
   below-floor Relay or an exact above-floor App update.
2. TTY only: join `GET /api/states` with
   `config/entity_registry/list`. Accept exactly one `update.*` entity whose
   immutable registry provenance is `platform: hassio` and whose `unique_id`
   identifies the NOVA Relay App update. A matching title alone is never
   sufficient. No match, ambiguity, current/in-progress state, malformed
   versions, or missing install/backup capabilities produces the manual path
   without an App-install prompt.
3. Show installed → available version, entity, partial App backup, and restart
   impact and state that Home Assistant selects the latest available version
   at execution time. Then prompt
   `Install the latest available NOVA Relay update now? [Y/n]`.
4. On yes:
   - Immediately re-read state and registry, then stop without writing if the
     entity ID, platform, unique ID, state, versions, capabilities, or
     in-progress flag differs from the confirmed preview.
   - `POST /api/services/update/install` via the existing `/core` proxy with
     `{"entity_id": ..., "backup": true}`. Omit `version`: Supervisor App
     updates do not expose a target-version binding, so Home Assistant installs
     the latest version available when the service executes.
   - Expect the relay to restart mid-call: tolerate the dropped response, then poll `GET /health` (existing `fetchRelayHealth`) every ~5 s for up to ~3 min until the reported version reaches the offered target and satisfies `min_relay_version`.
   - Report the verified version, or a plain timeout message with the manual path.
5. Non-TTY, `--yes`-style automation, or `doctor --quiet`: warning only.
   Standalone containers get the manual image-pull path because HA NOVA has no
   Docker-host access.
6. Windows self-update: the replacement finishes in the background helper (stdin unwired, console already returned to the shell — the documented background-complete contract), so it stays warning-only and prints a pointer to the interactive `ha-nova doctor` path, which offers the prompt.

## Boundaries

- Preview and bound confirmation always — an unprompted Relay restart kills
  in-flight Relay calls.
- Client-side only: generic HA endpoints through the existing proxy; no new relay endpoint, no HA domain logic in the relay (charter).
- Detection of "App-backed vs container" comes from registry-proven App
  evidence: no exact entity → treat as container/manual.

## Tests

- Entity resolution: exact registry-proven match, title-only/wrong-platform
  rejection, no match (→ manual path), ambiguous match (→ manual path).
- Preview precedes confirmation; decline performs no write; changed state after
  confirmation or changed registry provenance stops before the install call.
- In-progress entities and candidates without install plus backup support never
  prompt or write.
- Install flow with a mock relay that drops the install response, then serves the new version on `/health` (restart simulation).
- Poll timeout → honest failure message, exit code unchanged.
- Non-TTY and container paths stay warning-only.

## Out of scope

- Container auto-pull (Watchtower guidance stays documentation).
