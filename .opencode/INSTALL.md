# Installing HA NOVA for OpenCode

## Quick Start
>
> **Stable install (recommended)**
> 1. Open the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest)
> 2. Copy the one-liner for your OS
> 3. Run it unchanged
>
> This makes sure you install exactly the version shown on that release page.

### macOS / Linux

### Windows PowerShell

Windows uses a single supported install path: `install.ps1`.
HA NOVA installs itself on your computer; the Home Assistant connection steps stay guided in the browser.

Windows currently ships an x64 build. On Windows ARM64, use x64 emulation.

On Windows, install the OpenCode client separately first. HA NOVA handles the skills and setup, not the OpenCode app itself. Prefer WSL if you have the choice. Once the client works, run the HA NOVA installer. The installer launches a setup wizard — choose `OpenCode` when prompted.
Windows support for OpenCode is still early and not fully tested yet.

## Already Set Up?

Check if everything works:

```bash
ha-nova doctor
```

Useful commands:

```bash
ha-nova setup opencode
ha-nova update
ha-nova uninstall
ha-nova uninstall --purge
```

`ha-nova uninstall` now means standard remove and keeps your local HA NOVA connection settings and sign-in for easier reinstall/repair. Use `ha-nova uninstall --purge` for a full local wipe.

Recent Windows builds also migrate older `~/.local` / `~/.config` HA NOVA data into native `%LOCALAPPDATA%` / `%APPDATA%` locations automatically.

OpenCode does not show an HA NOVA update notice automatically yet. Use `ha-nova check-update` or `ha-nova doctor` when you want to check manually.

Old pre-Go install?

- macOS / Linux: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh | bash`
- Windows PowerShell: `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1 | iex`

## What's Next

After setup, HA NOVA commands like `ha-nova:read`, `ha-nova:write`, and `ha-nova:review` are available when you need them.

## Troubleshooting

Run `ha-nova doctor` to check connection and setup problems.

## Related

- Claude Code: `.claude/INSTALL.md`
- Codex: `.codex/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
