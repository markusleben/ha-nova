# Multi-Server Support Spec (draft)

Status: active — layer 1 shipped in v0.20.0; wizard layer 2 is tracked in #411
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

## Implementation hardening (2026-07-21 code-review findings)

Verified against the working tree; all of these are LAYER-1 scope, not follow-ups.

- **Save path must un-flatten (P1).** `saveConfig` writes the flat struct verbatim
  (`cli/config.go:74-77`); 8 read-modify-write sites exist (`cmd_pair.go:127`,
  `setup_device_verify.go:35`, `setup_pairing.go:280`, `setup_interactive.go:45,299,448,548`,
  and `command_doctor.go:69` — even `doctor` writes config via `resumePendingActivation`).
  After migration, the first such save would destroy the `servers` map. Save re-reads the
  raw profile document, writes the flat fields into the selected profile, preserves
  siblings and unknown top-level fields. Roundtrip test: "pair/doctor-resume on profile B
  leaves profile A byte-identical".
- **Raw readers need a profile-aware path (P1).** Four deliberate `loadJSONConfig`
  bypasses would silently break at schema v2: `keyring_service.go:100` (issue-#200
  fix: token storage must not depend on relay fields — regression risk: headless
  Secret-Service hang returns), `uninstall_preflight.go:69`, `command_uninstall.go:301-304`
  (purge would find nothing to revoke), `command_setup.go:55`. Route them through a
  profile-aware raw loader; add the #200 regression test.
- **Purge/uninstall iterates all profiles (P1).** `purgeDeviceCredentialWithReport`
  (`uninstall_device.go:22-75`) and `removeDeviceFileStorageResidue`
  (`device_credential_storage.go:218-233`) handle only the default slots and remove the
  machine-wide `.file-backend` marker while server B's files remain — breaking the
  "file exists IFF marker exists" invariant and leaving B's device active on its relay.
  Purge revokes per profile against that profile's pinned endpoint, deletes every
  namespaced slot, removes marker/dir only when all slots are gone.
- **Legacy-token fallback is default-profile-only (P2).** `relayFunctionalTransport`
  (`relay_transport.go:17-33`) falls back to the machine-wide legacy token for any config
  without secure fields — a half-paired profile B would send server A's token to server
  B's URL. Decision: non-default profiles are device-credential-only and fail closed.
  Slot routing happens via one process-global selected-profile seam resolved at startup
  (zero-arg slot API call sites stay untouched).
- **Field partitioning (P2).** Per-server: `ha_host`, `ha_url`, `relay_base_url`,
  `relay_secure_base_url`, `relay_spki_pin`, pending-pair fields, `relay_token_file`
  (default profile only). Install-wide (top-level): `schema_version`,
  `client_install_id` (comment says "this OS-user installation"; per-profile flattening
  would mint new install ids), `default_server`.
- **Downgrade floor (P2).** An older binary reading v2 config sees empty
  `relay_base_url` → "not set up yet". Mirror the default profile into the legacy flat
  fields for at least one release (write both shapes); name the supported downgrade
  floor in the release notes.
- **Doctor/update scope declared (P2).** Layer 1 keeps `doctor`, relay floor warnings,
  and guided App updates per selected server; doctor output names the checked profile;
  docs say "run `HA_NOVA_SERVER=<name> ha-nova doctor` per server".
- **Profile-name constraints (P2).** `[a-z0-9-]{1,32}` at creation (keyring service
  strings and secret file names inherit the name; Windows-invalid chars and
  case-insensitive-FS collisions are otherwise unhandled). `HA_NOVA_KEYRING_SERVICE`
  override applies to the legacy token only, which is default-profile-only (above) —
  profiles do not multiply the override.
- **Pair bootstrap (P2).** `pair --server <new-name>` without `--relay-url` is a hard
  error (the saved-URL fallback at `cmd_pair.go:80-99` would bootstrap against the
  default profile's relay).
- **Unknown selection fails loud (P2).** Unknown `--server`/`HA_NOVA_SERVER` exits
  non-zero listing known profiles (a typo must never route a mutation to the wrong
  house). Contract test + one context-skill routing line (`HA_NOVA_SERVER` env is the
  AI-client selection path — skills never read config.json; ~39 skill files shell out
  to `ha-nova relay` without a server argument) ship with layer 1.
- **File-backend canary + keyring→file migration are profile-aware (P3).**
  `fileStorageCanary` checks the target profile's slot names;
  `migrateKeyringDeviceCredentialToFile` moves ALL profiles' slots before committing
  the machine-wide marker.

## Release split (decision 2026-07-21)

- **Release A:** layer 1 (profiles, migration, credential namespacing, selection,
  hardening above) + layer 3 (profile management subcommand) + docs slice
  (`pair --server`, onboarding note, context-skill routing line). Ships together with
  the accumulated 0.20.0 skills features. Issue #343 closes with this release; the
  wizard ships as a follow-up issue.
- **Release B:** layer 2 (wizard "add another server" via `renderSetupAlreadyDoneBanner`,
  discovery filtering, profile-parameterized stages) — riskiest test surface
  (`setup_interactive_test.go`, 2625 LOC), gets a released layer-1 baseline to diff
  against.
- Both releases: full gate incl. RC rehearsal (Go + onboarding flow per CLAUDE.md).
- Rationale: the reporter is CLI-capable and unblocked by `pair --server` alone; the
  one-way migration gets bake time before the wizard churn lands.

## Open questions

- How do skills surface which server answered (prefix in output vs. only on ambiguity)?
  To resolve in the layer-1 docs slice: default is no prefix; on any ambiguity or
  non-default selection, name the profile once per response.

## Verification (when implemented)

- Migration round-trip tests (flat → profiles, idempotent, rollback-safe) incl. the
  save-path un-flatten roundtrip and the legacy-mirror downgrade shape.
- Credential-slot isolation tests (pairing server B never touches server A's slot);
  profile-aware purge/canary/migration tests; the #200 raw-reader regression test.
- Selection-order contract tests (flag > env > default; unknown selection fails loud
  with the profile list).
- Wizard contract tests: first-run flow byte-identical for single-server users; completed-install re-run offers the add-server entry; the add flow filters already-configured instances.
- Live acceptance with two HA instances.
