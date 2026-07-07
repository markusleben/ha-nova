## New Features

- Service / gateway mode for headless setups: `ha-nova setup --service hermes` stores the relay token in a hardened local file instead of the desktop keyring — built for SSH, systemd, and gateway sessions. Running plain `ha-nova setup hermes` later migrates the token back into the OS keyring automatically and cleans the file up.
- Hermes Agent early support (preview): HA NOVA installs its bundled Hermes skill set with both desktop and service setup paths, and `ha-nova doctor` points straight to the repair command when an older Hermes install drifts. Linux is validated today; macOS native and Windows WSL2 validation is tracked in `docs/reference/hermes-platform-validation.md`.
- Linux keyring recovery: `ha-nova setup` can now unlock or initialize a locked/fresh GNOME Keyring inline — including over SSH into the same logged-in desktop session.
- Self-healing client attachment: when a Claude Code update detaches HA NOVA, it reattaches itself automatically in the background — no manual repair needed.
- Smarter automation reviews: new checks catch restore branches that can fire without their save step ever running, and restart-recovery designs built on storage that does not survive a reboot.

## Bug Fixes

- Headless Linux: the CLI no longer hangs in Secret Service unlock prompts when a relay token file is configured, and unreadable configs fail loud with a fix hint instead of silently falling back to the keyring.
- Security: updated `axios` to `1.17.0` and `ws` to `8.21.0` in both the main app and the Relay package, closing all open dependency alerts.

## Install

**macOS / Linux**
```sh
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/<RELEASE_TAG>/install.sh | HA_NOVA_VERSION=<RELEASE_TAG> bash
```

**Windows (PowerShell)**
```powershell
$env:HA_NOVA_VERSION = '<RELEASE_TAG>'
irm https://raw.githubusercontent.com/markusleben/ha-nova/<RELEASE_TAG>/install.ps1 | iex
```

## Already Installed?

Run `ha-nova check-update` or `ha-nova update`.
