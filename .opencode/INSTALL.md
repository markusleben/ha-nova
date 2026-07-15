# OpenCode Install Overlay

This page only covers OpenCode-specific deltas.

For the stable installer, lifecycle commands, and general troubleshooting, use [README.md](../README.md) and the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest).

## Setup Choice

- Use the normal HA NOVA installer flow.
- In the setup wizard, choose `OpenCode`.
- Install the OpenCode client separately; HA NOVA handles the skills and onboarding, not the OpenCode app itself.
- OpenCode Desktop uses the same skill path as the terminal client: `~/.config/opencode/skills/ha-nova`.

## Windows Notes

- Prefer WSL if you have the choice.
- Once OpenCode itself works, run the HA NOVA installer.
- Windows support for OpenCode is still early and not fully tested yet.

## Updates and Repair

- Connect or repair with `ha-nova setup opencode`.
- Validate the install with `ha-nova doctor`.
- OpenCode surfaces HA NOVA update notices during normal skill use (relay calls check a local cache; silence them with `HA_NOVA_NO_UPDATE_NUDGE=1`). `ha-nova check-update` still works for a manual check.
- After `ha-nova update` succeeds, start a new AI client session to load the updated HA NOVA skills.

## What You Get

After setup, HA NOVA commands like `ha-nova:read`, `ha-nova:write`, and `ha-nova:review` are available inside OpenCode.

## Related

- Claude Code: `.claude/INSTALL.md`
- Codex: `.codex/INSTALL.md`
- Google Antigravity: `.antigravity/INSTALL.md`
- Hermes Agent: `.hermes/INSTALL.md`
