# Locked-Keyring Onboarding UX Spec

Status: merged — #388
Date: 2026-07-20
Trigger: live 0.19.0 rollout on a headless Ubuntu VM (systemd user services with linger, no graphical login) failed device pairing with "desktop keyring locked", and the AI client could not discover any supported way out.

## Problem

On Linux systems that ship a desktop stack but never see a graphical login (agent VMs, autologin boxes, homelab servers), gnome-keyring starts as a user unit with its default collection permanently locked. Device pairing then fails hard by design ("no silent downgrade"), but:

1. `.hermes/INSTALL.md` promises that `ha-nova setup --service <client>` stores the paired **device credential** in a protected file. The code only does that for the legacy relay token; the device-credential probe still requires the keyring, so service setup fails on exactly the machines it is documented for.
2. The locked-keyring error names no way forward. The file backend and its `.file-backend` marker exist but are internal — reaching them requires reading source code.
3. `min_relay_version` stayed at 0.4.0 through the 0.19.0 auth-generation change, so a 0.19.0 CLI talking to a pre-pairing App surfaces a bare 401 instead of the built-in "Relay outdated" guidance.

## Goal

A machine whose keyring is present but never unlockable can complete secure device pairing through documented, explicit commands — and every failure on that path names its own way out.

## Scope

- `ha-nova setup --service <client>` forces the device-credential file backend before the pairing stage, fulfilling the documented contract. Marker persistence still happens only at credential promotion.
- New `ha-nova pair --credential-store=file` opt-in (only valid value: `file`) for the standalone pairing path. Works on all platforms; documented primarily for Linux headless/VM use.
- The locked/uninitialized-keyring probe error gains one actionable line naming both commands.
- The legacy relay-token locked-keyring message additionally names `ha-nova setup --service`.
- `min_relay_version` 0.4.0 → 0.7.0 in both `version.json` files, plus a parity guard test.
- Docs: onboarding skill, `.hermes/INSTALL.md` consistency, client-integration,
  safety, `nova/DOCS.md`; release claims collected in
  `docs/archive/work/0.20.0-release-body.md` (README changes waited for the
  release-prep PR).

## Non-goals

- No interactive backend-switch prompt inside the wizard.
- No automatic downgrade to file storage without explicit opt-in (`--service` or `--credential-store=file`).
- No cross-backend cleanup: after a switch, an orphaned keyring entry is inert (the relay replaces credentials per `client_install_id`).
- The actionable hint attaches at the probe path only; the rare resume-with-relocked-keyring path keeps its current error.
- No multi-server support (tracked separately, issue #343).

## Security and compatibility

- The file backend remains the existing 0600-file/0700-dir mechanism under `~/.config/ha-nova/secrets/`; forcing it is an explicit owner decision, equivalent in protection level to the documented service token file.
- Existing service installs with an unlocked keyring switch to the file backend on their next `--service` re-setup — this is the documented behavior; release notes flag it under "What To Watch".
- Fail-closed guarantee unchanged: a one-time pairing code is never consumed when storage cannot work.
- Floor bump only strengthens guidance (warning + guided update offer); the CLI still operates against older relays.

## Verification

- Regression test: service setup with a locked-keyring stub reaches pairing in file mode (today's real-world failure).
- `pair --credential-store=file` pairs under a locked-keyring stub; marker persists only after promotion; invalid values fail with the valid set.
- Locked probe error contains both command hints.
- Floor tests updated to 0.7.0; parity guard over both `version.json` files.
- `go test ./cli/...`, `npx vitest run` (targeted suites), `bash scripts/check-docs.sh`.
- Live acceptance on the maintainer VM: `ha-nova setup --service hermes` or `ha-nova pair --credential-store=file` with a fresh code.
