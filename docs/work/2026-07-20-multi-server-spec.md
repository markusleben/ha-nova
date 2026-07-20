# Multi-Server Support Spec (draft)

Status: draft (design sketch — not scheduled)
Date: 2026-07-20
Trigger: issue #343 — one client machine should reach more than one Home Assistant server; today `config.json` holds exactly one flat server configuration.

## Problem

`runtimeConfig` (`cli/config.go`) is a single flat object: one `ha_url`, one `relay_base_url`, one secure endpoint + SPKI pin. A user with two HA instances (for example home + remote site) cannot express the second one; the only workaround is a second OS user, which the issue author rightly calls a workaround, not a solution.

## Direction (sketch)

- **Named profiles in one config.** Bump `schema_version` and introduce a `servers` map keyed by a user-chosen name, plus `default_server`. Existing configs migrate automatically: the current flat fields become `servers.default`. Rollback-safe: the migration writes a new schema version only after a successful parse round-trip.
- **Selection order:** `--server <name>` flag > `HA_NOVA_SERVER` env > `default_server`. Skills pass the selection through untouched; the relay stays dumb.
- **Per-server credential slots.** Device-credential slots namespace by server name (keyring service string and secret file name gain a per-server suffix; the `default` profile keeps today's names so nothing re-pairs on upgrade). The credential-store backend decision (`.file-backend` marker) stays machine-wide — it describes the machine, not the server.
- **Pairing per server:** `ha-nova pair --server <name> [--relay-url …]` reuses the existing bootstrap; `--relay-url` already seeds fresh configs today.
- **Second-server install docs:** installing the Relay App on another HA instance is independent of the client machine; document that adding a server never touches existing profiles.
- **Wizard integration — progressive disclosure (decision 2026-07-20).** Adding a server IS a wizard flow, but never a first-run question: the single-server majority must not pay for the multi-server case.
  - First-run setup stays exactly as it is today.
  - Re-run entry point: when `ha-nova setup` runs on a completed install, the existing "Setup complete" state screen offers "Add another Home Assistant server". This is also the path the onboarding skill tells AI clients to use.
  - Discovery assist: when the existing discovery pick list finds MORE than one instance during first-run setup, the wizard may offer a one-line follow-up after the first server is set up ("Found a second instance — connect it too?"). One line, no pressure.
  - The add flow reuses the existing stages (discovery with already-configured instances filtered out → Relay App install on the new instance → pairing with that instance's code → verify); the only new mechanics is parameterizing which profile the results are written to.
  - Division of labor: the wizard onboards ("add"), the CLI administers. Profile management (rename, delete, switch default) ships as small CLI commands, not wizard stages; runtime selection stays `--server`/env/default — the wizard sets up, it never routes.

## Non-goals (first iteration)

- No per-directory config overrides (the issue's alternative idea) — profiles cover the need with less config-resolution magic; revisit only on real demand.
- No concurrent multi-server fan-out in a single skill call; one call, one server.
- No profile-aware Home Base UI changes.
- No new first-run wizard questions, and no profile management (rename/delete/default) inside the wizard — see the wizard-integration decision above.

## Open questions

- How do skills surface which server answered (prefix in output vs. only on ambiguity)?

## Verification (when implemented)

- Migration round-trip tests (flat → profiles, idempotent, rollback-safe).
- Credential-slot isolation tests (pairing server B never touches server A's slot).
- Selection-order contract tests (flag > env > default).
- Wizard contract tests: first-run flow byte-identical for single-server users; completed-install re-run offers the add-server entry; the add flow filters already-configured instances.
- Live acceptance with two HA instances.
