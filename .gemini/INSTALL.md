# Installing HA NOVA for Gemini CLI

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

On Windows, install the Gemini client separately first. HA NOVA only installs the skills/config side; it does not prove the Gemini runtime for you. Client availability can differ from macOS/Linux, so follow the Gemini project's own Windows instructions first, then run the HA NOVA installer above and choose `Gemini CLI`.
Current HA NOVA Windows support for Gemini is smoke-validated for this release.

## Already Set Up?

Run diagnostics:

```bash
ha-nova doctor
```

Common commands:

```bash
ha-nova setup gemini
ha-nova update
ha-nova uninstall
ha-nova uninstall --purge
```

`ha-nova uninstall` now means standard remove and keeps local HA NOVA connection config plus the secure token for easier reinstall/repair. Use `ha-nova uninstall --purge` for a full local wipe.

Recent Windows builds also migrate older `~/.local` / `~/.config` HA NOVA data into native `%LOCALAPPDATA%` / `%APPDATA%` locations automatically.

Gemini-specific note:

- HA NOVA installs namespaced sub-skills under `~/.gemini/skills/ha-nova-*`.
- Those namespaced identifiers are also the installed Gemini skill names, so Gemini does not have to guess between folder names and shorter shared repo names.

No automatic startup update banner yet on Gemini. Use `ha-nova check-update` or `ha-nova doctor` when you want an explicit update check.

Old pre-Go install?

- macOS / Linux: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh | bash`
- Windows PowerShell: `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1 | iex`

## Related

- Claude Code: `.claude/INSTALL.md`
- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
