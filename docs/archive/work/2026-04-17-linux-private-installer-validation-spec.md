## Linux Private Installer Validation

Date: 2026-04-17

### Scope

Prove the unreleased Linux installer path on a real Linux machine by serving a private local `install.sh` plus a private local Linux install bundle and checksum over LAN.

### Plan

1. Build fresh snapshot binaries and install bundles locally.
2. Serve only `install.sh`, the Linux `tar.gz` bundle, and its `.sha256` from a temporary local HTTP server.
3. Run the real Linux `install.sh` flow on the remote Linux host with `HA_NOVA_BUNDLE_URL` and `HA_NOVA_BUNDLE_SHA256_URL` overrides.
4. Verify runtime install root, public symlink, bundle metadata, and post-install CLI behavior on the remote host.
5. Fix any installer or bundle noise surfaced by the Linux proof and rerun the same path.

### Exit Criteria

- The remote Linux host installs the unreleased bundle through the real `install.sh` path.
- `~/.local/share/ha-nova` and `~/.local/bin/ha-nova` are created correctly.
- `ha-nova version` works on the remote Linux host.
- Non-interactive installer output is clean: no `/dev/tty` leak and no macOS xattr warning noise from the bundle.
- No host-specific SSH or LAN details are committed to the repo.
