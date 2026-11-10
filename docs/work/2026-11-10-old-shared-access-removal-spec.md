# Spec: the NOVA App stops accepting the old shared access (#905)

Status: active (0.30 train). Decision: Markus, 10.11. (D3): remove it in 0.30. Breaking: a computer still on the old shared access pairs again.

## Problem

Since 0.27 every computer pairs on its own (device credential over pinned TLS, port 8792). The old shared token still works on the App: 0.27 migrated it into the device registry as one `legacy` record, and the plain port 8791 serves `/ws`, `/core`, `/files`, `/backups`, `/supervisor`, `/health` and the gate's ask routes to it, in cleartext. Setup, doctor, update and check-update have said "the next release won't accept it" since 0.27.

## What changes

### NOVA App (Relay, App mode)

1. **Plain port 8791 serves pairing only** (`GET /pair/v1/info`, `POST /pair/v1/start`, `POST /pair/v1/finish`). Every other request is refused before any handler runs:
   - a device credential: `403 SECURE_TRANSPORT_REQUIRED` (unchanged);
   - any other bearer: `401 PAIRING_REQUIRED`, message "The NOVA App no longer accepts the old shared access. Pair this computer again: run ha-nova setup.";
   - no bearer: `401 UNAUTHORIZED` (unchanged).
   The status stays 401 so CLIs before 0.30 keep their existing answer ("local Relay rejected the saved local credential; run: ha-nova setup").
2. **The resolver accepts device credentials only.** The legacy branch in `principal.ts` goes; a non-device bearer on the secure port gets the same `401 PAIRING_REQUIRED`.
3. **The registry retires the record, and a rollback stays closed.** Opening a healthy registry drops a stored `legacy` record and writes `legacy: null, legacyImportCompleted: true` before any listener serves (also on a fresh install and after a reset; the schema version stays 1, so an older App still reads the file). A failed write stops the App start (fail loud). An older App reads that tombstone and never imports a token again, so a rollback cannot bring the old access back. The registry API loses `hasLegacy`, `resolveLegacySecret`, `importLegacy`, `markLegacyMigrated`, `revokeLegacy`.
4. **Deleted:** `runtime/legacy-migration.ts`; the App-mode token path in `config/relay-token.ts`; the NOVA page's "Legacy access" section and its `revoke_legacy` action; the App options `relay_auth_token` and `ha_llat` (`nova/config.yaml`, `translations/en.yaml`, `nova/run`).
5. **Kept for older CLIs:** `GET /auth/device/peers` still sends `legacy_access: false`. Uninstall reads a missing field as unknown and would keep the App.

### Standalone container (unchanged)

The standalone container keeps its own token (`RELAY_AUTH_TOKEN`, `createHttpServer`): one listener, no registry, no pairing. Its principal is renamed from `legacy` to `shared` so the code no longer calls it legacy; the gate worker ignores the principal (`cli/command_gate.go`), so the wire rename changes nothing in Go.

### CLI

One text, two variants (the service install gets the file-backed pair):

> This computer still uses the old shared access, which the updated NOVA App no longer accepts. Run ha-nova setup in a terminal once (takes a minute) to pair this computer again.

A service install names `ha-nova pair --credential-store=file`, a named server its `ha-nova pair --server` line. With Home Assistant Link saved (pairing waits until Link is removed): "Remove Home Assistant Link first (`ha-nova cloud remove --server <name>`), then run … to pair this computer again. Add Link again afterwards if you use it (`ha-nova cloud add --server <name>`)."

The App marks its refusal with the header `x-ha-nova-auth: pairing-required`; the CLI renders its own text from it (never the Relay's message), so the command fits the install.

- **Notice** (setup, doctor, update, check-update): same places and gating as today (shared access, the App can pair, token readable), new text. It holds for an App not yet updated, too.
- **Refusal:** a local Relay answer `401 PAIRING_REQUIRED` becomes that text everywhere a local refusal is rendered (verbs, MCP, doctor, setup checks, the App-update wait). Never a bare 401.
- **Setup:** when the App refuses the token (the marked 401) and can pair, the switch has no "Not now": setup pairs (terminal and setup page). With Link saved it stops at once with the Link text above, before the token step (whose "make a new key" would point at an App option that no longer exists). A service install, `--relay-token`, and `--non-interactive` runs never switch on their own; their connection check stops with the re-pair text ("in a terminal" names the interactive run). The setup texts drop "the next release won't accept it"; `cloud remove` on a shared-token install says to pair next.

## Safety

- No path grants more than before: App mode has no non-device principal left; the plain port has no functional route; the gate sees device principals only in App mode.
- Rollback to the previous App: the registry tombstone keeps the old token out (fail closed). Kept by `tests/security/device-registry*.test.ts` and the supervisor-app lane.
- Home Assistant Link already refused the shared token; unchanged.

## Lanes

- `pairing-e2e` (CI): the shared token on the real bootstrap listener gets `401 PAIRING_REQUIRED`; the real CLI says how to pair again.
- Disposable-HA e2e, app-mode section: a seeded paired device proves the upstream path over the secure port; the old token is refused on both ports.
- Supervisor-app lane: OLD with a seeded paired device and the token as an App option (both work) → NEW: the token is refused on both ports with the mark, the device still works, the registry is retired with the device kept, TLS identity and instance id byte-identical. The NEW+1 local-build rollback: the same. Then NEW → OLD source (rollback on the same `/data`, the token seeded again as an App option): the token stays refused, the device works, the tombstone stays. Once OLD is itself 0.30+ (no token option), the token steps skip and the rest still runs.
- Client matrix (`--from v0.28.3 --to local`): an App-mode Relay from the bed's own image (no Supervisor) next to the test bed. Scenario A points its config at it: before the update the v0.28.3 doctor fails and names `ha-nova setup`; after it, doctor and `relay health` fail with the re-pair text.
- Windows VM lane: no delta. It installs and pairs fresh, so it never holds a shared token.

## Known limits

- Restoring an App backup taken before 0.30 restores that App version with its data, the old record included. The rollback guarantee covers going back to older App code on the current `/data`, not restoring old data.
- The client matrix proves the re-pair message, not a full re-pairing: pairing needs the NOVA page behind Supervisor ingress. Pairing itself is unchanged and runs end to end in `pairing-e2e`; setup's route into pairing is covered by unit tests.
- A leftover `/data/relay_auth_token` from an App before 0.27 stays on disk (dead; the tombstone keeps an older App from importing it).
- HA NOVA 0.28.3 and older can't be changed: after the App update its setup still offers the switch (the right answer), but its "Not now" and a service install land in the token step, which asks for an App option that no longer exists. The release notes say so.
- The notice and the refusal text name the default profile's state only (`legacySharedAccess`); a named profile on the shared token gets the refusal text when it is used.
- The pre-0.30 App's update notice in CHANGELOG is written in the release-prep PR (the App version is bumped once per train); its line is queued in `docs/work/next-release-body.md`, with the README changes.

- An App option `relay_auth_token` stored by Supervisor before 0.27 and never cleared stays in Supervisor's own storage (the token is dead; an older App clears it on a rollback).
