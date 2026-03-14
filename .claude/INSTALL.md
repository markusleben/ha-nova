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

Choose `Claude Code` when prompted.

## Activating Skills

`ha-nova setup claude` handles the local plugin registration automatically.

If you need to repair it manually:

```text
claude plugin marketplace add ~/.local/share/ha-nova
claude plugin install ha-nova@ha-nova
```

If you are working from a local repo checkout instead of an installed bundle, use the repo root instead of `~/.local/share/ha-nova`.

Skills are then available as `/ha-nova:read`, `/ha-nova:write`, etc.

**Alternative — per-session (development):**
```sh
claude --plugin-dir ~/ha-nova
```

## Already Set Up?

Run diagnostics:

```bash
ha-nova doctor
```

Common commands:

```bash
ha-nova setup claude
ha-nova update
ha-nova uninstall
```

Old pre-Go install?

- macOS / Linux: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh | bash`
- Windows PowerShell: `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.ps1 | iex`

## Related

- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
