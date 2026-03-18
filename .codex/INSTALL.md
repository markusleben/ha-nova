# Installing HA NOVA for Codex

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

On Windows, install the Codex client separately first. HA NOVA only installs the skills/config side; it does not prove the Codex runtime for you. Client availability can differ from macOS/Linux, so follow the Codex project's own Windows instructions first, then run the HA NOVA installer above and choose `Codex CLI`.
Current HA NOVA Windows support for Codex is still experimental until explicit Windows smoke completes.

## Already Set Up?

Run diagnostics:

```bash
ha-nova doctor
```

Common commands:

```bash
ha-nova setup codex
ha-nova update
ha-nova uninstall
```

Old pre-Go install?

- macOS / Linux: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh | bash`
- Windows PowerShell: `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1 | iex`

## Related

- Claude Code: `.claude/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
