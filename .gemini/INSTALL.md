# Gemini CLI Install Overlay

This page only covers Gemini-specific deltas.

For the stable installer, lifecycle commands, and general troubleshooting, use [README.md](../README.md) and the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest).

## Setup Choice

- Use the normal HA NOVA installer flow.
- In the setup wizard, choose `Gemini CLI`.
- Install the Gemini client separately; HA NOVA handles the skills and onboarding, not the Gemini app itself.

## Windows Notes

- Install Node.js first, then install Gemini CLI, then run the HA NOVA installer.
- After installing both, open a fresh PowerShell window and verify:

```powershell
node --version
gemini --version
```

- If either command fails, fix that first before running `ha-nova setup`.
- Gemini CLI on Windows needs Node.js.
- Gemini has basic Windows validation for this release.
- If only an old `%APPDATA%\\npm` Gemini shim exists but Node.js is missing, `ha-nova setup all` now skips Gemini instead of failing later.

## Updates and Repair

- Connect or repair with `ha-nova setup gemini`.
- Validate the install with `ha-nova doctor`.
- Gemini does not surface HA NOVA update notices automatically yet. Use `ha-nova check-update` or `ha-nova doctor` when you want to check manually.

## Gemini-Specific Skill Layout

- HA NOVA installs Gemini skills under `~/.gemini/skills/ha-nova-*`.
- The `ha-nova-*` naming avoids conflicts with other Gemini skills.

## What You Get

After setup, HA NOVA commands like `ha-nova:read`, `ha-nova:write`, and `ha-nova:review` are available inside Gemini CLI.

## Related

- Claude Code: `.claude/INSTALL.md`
- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
