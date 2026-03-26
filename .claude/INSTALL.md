# Installing HA NOVA for Claude Code

## Quick Start

### macOS / Linux

```sh
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex
```

Windows uses a single supported install path: `install.ps1`.
HA NOVA installs itself locally; Home Assistant Relay setup and token steps stay guided in the browser.

Windows currently ships an `amd64` bundle. On Windows ARM64, use x64 emulation.

The installer launches a setup wizard — choose `Claude Code` when prompted for your AI client.

Claude Desktop in the **Code** tab uses this same Claude integration path.

## Windows Notes

Install Claude Code itself separately if you haven't already.
Claude is validated on Windows for this release.

Claude Code on Windows may need extra setup:
- May require Git for Windows or WSL, depending on your setup.
- If Claude shows an installer-migration hint:

```powershell
claude install
```

- Still on the older npm path and `npm.ps1` is blocked?

```powershell
npm.cmd install -g @anthropic-ai/claude-code
```

## Activating Skills

`ha-nova setup claude` handles local plugin registration automatically.

Default behavior for real installs:
- HA NOVA stages the Claude marketplace from the installed HA NOVA release payload on disk
- Claude does not follow the repo automatically anymore
- HA NOVA itself tells you when a newer release exists
- when you see an update notice, run `ha-nova update` and then restart Claude

Local validation / private-RC behavior:
- set `HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1`
- HA NOVA keeps forcing the same local staged marketplace path
- use this only when validating freshly built local bundles
- this path is intentionally a fresh local reinstall, not an in-place marketplace update

To repair it manually:

```sh
ha-nova setup claude
ha-nova doctor
```

If HA NOVA already showed an update notice, run:

```text
ha-nova update
```

Then restart Claude.

## Local repo checkout (macOS / Linux only)

Working from a local repo checkout instead of an installed bundle? Let HA NOVA stage a local marketplace:

```sh
export HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1
bash scripts/onboarding/install-local-skills.sh claude
```

This keeps Claude pointed at the current local checkout payload for testing/development.
The local validation path also refreshes the Claude plugin cache before reinstalling — edited skills won't stay stuck behind an old cached payload.

Skills are then available as `/ha-nova:read`, `/ha-nova:write`, etc.

**Alternative — per-session (development, macOS / Linux only):**
```sh
claude --plugin-dir /path/to/your/ha-nova-checkout
```

On Windows, prefer the installed-bundle path or rerun `ha-nova setup claude` instead of `--plugin-dir`.

## Already Set Up?

Check if everything works:

```bash
ha-nova doctor
```

Useful commands:

```bash
ha-nova setup claude
ha-nova update
ha-nova uninstall
ha-nova uninstall --purge
```

`ha-nova uninstall` now means standard remove and keeps local HA NOVA connection config plus the secure token for easier reinstall/repair. Use `ha-nova uninstall --purge` for a full local wipe.

Recent Windows builds also migrate older `~/.local` / `~/.config` HA NOVA data into native `%LOCALAPPDATA%` / `%APPDATA%` locations automatically.

Old pre-Go install?

- macOS / Linux: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh | bash`
- Windows PowerShell: `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1 | iex`

## Related

- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
