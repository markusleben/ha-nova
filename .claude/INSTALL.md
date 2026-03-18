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

Windows currently ships an `amd64` bundle. On Windows ARM64, use x64 emulation.

Choose `Claude Code` when prompted.

Claude Desktop in the **Code** tab uses this same Claude integration path.

## Windows Notes

Install Claude Code itself separately before or after HA NOVA.
Claude is the currently smoke-validated HA NOVA client lane on Windows.

Current Anthropic guidance for Windows is stricter than for HA NOVA itself:
- Claude Code may require **Git for Windows** or **WSL**, depending on your setup.
- If Claude shows the installer-migration hint, run:

```powershell
claude install
```

- If you still use the older npm path in PowerShell and `npm.ps1` is blocked, use:

```powershell
npm.cmd install -g @anthropic-ai/claude-code
```

## Activating Skills

`ha-nova setup claude` handles the local plugin registration automatically.

Default behavior for real installs:
- HA NOVA registers the Claude marketplace from the GitHub repo
- Claude plugin updates then follow the repo, as intended by Claude marketplaces

Local validation / private-RC behavior:
- set `HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1`
- HA NOVA then registers a local marketplace root instead of GitHub
- use this only when validating freshly built local bundles
- this path is intentionally a fresh local reinstall, not an in-place marketplace update

If you need to repair it manually:

### macOS / Linux

```sh
claude plugin marketplace remove ha-nova
claude plugin marketplace add https://github.com/markusleben/ha-nova
claude plugin update ha-nova@ha-nova
```

### Windows PowerShell

```powershell
claude plugin marketplace remove ha-nova
claude plugin marketplace add https://github.com/markusleben/ha-nova
claude plugin update ha-nova@ha-nova
```

If this is the first install and `update` says the plugin is missing, run:

```text
claude plugin install ha-nova@ha-nova
```

## Local repo checkout (macOS / Linux only)

If you are working from a local repo checkout instead of an installed bundle, let HA NOVA stage a local marketplace for you:

```sh
export HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1
bash scripts/onboarding/install-local-skills.sh claude
```

That keeps Claude pointed at the current local checkout payload for testing/development.
The local validation path also refreshes the Claude plugin cache before reinstalling, so edited skills do not stay stuck behind an old cached payload.

Skills are then available as `/ha-nova:read`, `/ha-nova:write`, etc.

**Alternative — per-session (development, macOS / Linux only):**
```sh
claude --plugin-dir /path/to/your/ha-nova-checkout
```

On Windows, prefer the installed-bundle path or the plugin marketplace repair commands above instead of `--plugin-dir`.

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
