# Linux SSH Keyring Guidance

Date: 2026-04-17
Status: implemented

## Scope

Tighten Linux setup messaging for the verified SSH/headless Secret Service recovery path without external shell workarounds.

## Plan

1. Keep the Linux keyring failure class strict; do not add insecure runtime fallback.
2. Detect recoverable Linux Secret Service states precisely: `needs-init` vs `locked`.
3. Recover those states inside setup with the built-in GNOME Keyring DBus flow so SSH/headless users can initialize or unlock local secure storage inline.
4. Surface matching user guidance in interactive warnings and save-time errors.
5. Cover the new behavior and wording in regression tests.

## Exit Criteria

- Linux setup/setup-save messaging still fails loud on blocked secure storage.
- Recoverable Linux states are handled inline during setup for both fresh initialization and unlock.
- SSH/headless users are guided through local secure storage creation or unlock without being told to run a shell command manually.
- Regression tests cover the new wording and the recovery flow.
