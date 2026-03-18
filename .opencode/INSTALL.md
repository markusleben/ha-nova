# Installing HA NOVA for OpenCode

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
```

No automatic startup update banner yet on OpenCode. Use `ha-nova check-update` or `ha-nova doctor` when you want an explicit update check.

Old pre-Go install?

- macOS / Linux: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh | bash`
- Windows PowerShell: `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1 | iex`

## Related

- Claude Code: `.claude/INSTALL.md`
- Codex: `.codex/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
