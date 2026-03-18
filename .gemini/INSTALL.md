# Installing HA NOVA for Gemini CLI

## Quick Start

### macOS / Linux

```sh
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex
```

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
```

Gemini-specific note:

- HA NOVA installs namespaced sub-skills under `~/.gemini/skills/ha-nova-*`.
- Those namespaced identifiers are also the installed Gemini skill names, so Gemini does not have to guess between folder names and shorter shared repo names.

Old pre-Go install?

- macOS / Linux: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh | bash`
- Windows PowerShell: `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1 | iex`

## Related

- Claude Code: `.claude/INSTALL.md`
- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
