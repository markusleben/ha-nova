## New Features

- Hermes Agent is now a first-class HA NOVA client path. HA NOVA now installs the bundled Hermes skill set directly, supports native macOS/Linux plus WSL2 on Windows, and can repair older Hermes installs with the simple `ha-nova doctor` -> `ha-nova setup hermes` flow.
- Linux setup can now recover a locked or fresh GNOME Keyring inline during `ha-nova setup`, including SSH into the same logged-in desktop session.
- HA NOVA now ships dedicated `dashboard`, `organize`, and `history` skills, making those workflows easier to discover and safer to use.
- Gemini now picks up newly shipped HA NOVA sub-skills automatically during install and sync.

## Bug Fixes

- Updated `axios` to `1.15.0` in both the main app and the Relay package to close critical production dependency findings.

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
