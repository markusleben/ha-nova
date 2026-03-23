# Installing HA NOVA for OpenCode

## Quick Start

### macOS / Linux

```sh
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

### Windows PowerShell

```powershell
$ProgressPreference = 'SilentlyContinue'
irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex
```

Current public Windows install path: `install.ps1`.
A `winget` manifest is generated for each release, but the public package is not live until that manifest is published and proven on a fresh Windows VM.

Windows currently ships an `amd64` bundle. On Windows ARM64, use x64 emulation.

Install the OpenCode client separately first. HA NOVA only installs the skills/config side; it does not prove the OpenCode runtime for you. For Windows, prefer **WSL** for the OpenCode client if you have the choice. Once the client itself works, run the HA NOVA installer above and choose `OpenCode` when prompted.
Current HA NOVA Windows support for OpenCode is still experimental until explicit Windows smoke completes.

## Already Set Up?

Run diagnostics:

```bash
ha-nova doctor
```

Common commands:

```bash
ha-nova setup opencode
ha-nova update
ha-nova uninstall
ha-nova uninstall --purge
```

`ha-nova uninstall` now means standard remove and keeps local HA NOVA connection config plus the secure token for easier reinstall/repair. Use `ha-nova uninstall --purge` for a full local wipe.

Recent Windows builds also migrate older `~/.local` / `~/.config` HA NOVA data into native `%LOCALAPPDATA%` / `%APPDATA%` locations automatically.

No automatic startup update banner yet on OpenCode. Use `ha-nova check-update` or `ha-nova doctor` when you want an explicit update check.

Old pre-Go install?

- macOS / Linux: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh | bash`
- Windows PowerShell: `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1 | iex`

## Related

- Claude Code: `.claude/INSTALL.md`
- Codex: `.codex/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
