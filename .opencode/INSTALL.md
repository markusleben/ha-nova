# OpenCode Install Overlay

This page only covers OpenCode-specific deltas.

For the stable installer, lifecycle commands, and general troubleshooting, use [README.md](../README.md) and the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest).

## Setup Choice

- Use the normal HA NOVA installer flow.
- In the setup wizard, choose `OpenCode`.
- Install the OpenCode client separately; HA NOVA handles the skills and onboarding, not the OpenCode app itself.

## Windows Notes

- Prefer WSL if you have the choice.
- Once OpenCode itself works, run the HA NOVA installer.
- Windows support for OpenCode is still early and not fully tested yet.

## Updates and Repair

- Connect or repair with `ha-nova setup opencode`.
- Validate the install with `ha-nova doctor`.
- OpenCode does not surface HA NOVA update notices automatically yet. Use `ha-nova check-update` or `ha-nova doctor` when you want to check manually.

## What You Get

After setup, HA NOVA commands like `ha-nova:read`, `ha-nova:write`, and `ha-nova:review` are available inside OpenCode.

## Related

- Claude Code: `.claude/INSTALL.md`
- Codex: `.codex/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
- Hermes Agent: `.hermes/INSTALL.md`
