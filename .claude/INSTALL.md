# Claude Code Install Overlay

This page only covers Claude-specific deltas.

For the stable installer, lifecycle commands, and general troubleshooting, use [README.md](../README.md) and the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest).

## Setup Choice

- Use the normal HA NOVA installer flow.
- In the setup wizard, choose `Claude Code`.
- Claude Desktop in the **Code** tab uses this same integration path.

## Windows Notes

- Install Git for Windows first and keep its `Add to PATH` option enabled.
- Then install Claude Code with the official native Windows installer:

```powershell
irm https://claude.ai/install.ps1 | iex
```

- After install, open a fresh PowerShell window and verify:

```powershell
claude --version
claude doctor
```

- Native Windows Claude also needs Git for Windows, because Claude Code requires Git Bash there.
- Claude is the Windows path we have tested most for this release.
- If Git Bash is missing, `ha-nova setup all` skips Claude on Windows instead of failing the whole setup run.
- If you prefer not to install Git for Windows, use Claude inside WSL instead.
- If Git is installed but Claude still cannot find Git Bash, set `CLAUDE_CODE_GIT_BASH_PATH` to your `bash.exe` location in Claude's `settings.json`.
- If you are migrating from the older npm install, run:

```powershell
claude install
```

- Then remove the old global npm package:

```powershell
npm.cmd uninstall -g @anthropic-ai/claude-code
```

- Only use the old npm path if you are explicitly migrating or repairing an older install.

## Updates and Repair

- Connect or repair with `ha-nova setup claude`.
- Shipped installs use a local HA NOVA release snapshot under `~/.config/ha-nova/claude-marketplace/releases/vX.Y.Z`.
- HA NOVA itself shows Claude update notices. When you see one, run `ha-nova update` and then restart Claude.
- If the Claude path looks broken, run `ha-nova setup claude` and then `ha-nova doctor`.

## Local Repo Checkout (macOS / Linux only)

For repo-local development, stage a local Claude marketplace:

```bash
export HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1
bash scripts/onboarding/install-local-skills.sh claude
```

That keeps Claude pointed at the current checkout for validation work and refreshes the local plugin cache before reinstalling.

Skills are then available as `/ha-nova:read`, `/ha-nova:write`, and related commands.

Alternative per-session path:

```bash
claude --plugin-dir /path/to/your/ha-nova-checkout
```

On Windows, prefer the normal installed path or rerun `ha-nova setup claude` instead of `--plugin-dir`.

## Related

- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
