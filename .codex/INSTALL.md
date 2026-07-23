# Codex Install Overlay

This page only covers Codex-specific deltas.

For the stable installer, lifecycle commands, and general troubleshooting, use [README.md](../README.md) and the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest).

## Setup Choice

- Use the normal HA NOVA installer flow.
- In the setup wizard, choose `Codex CLI`.
- Install the Codex client separately; HA NOVA handles the skills and onboarding, not the Codex app itself.

## Windows Notes

- Follow Codex's own Windows setup first, then run the HA NOVA installer.
- Windows support for Codex is still early and not fully tested yet.

## Updates and Repair

- Connect or repair with `ha-nova setup codex`.
- Validate the install with `ha-nova doctor`.
- Before the first Home Assistant task, the first HA NOVA skill used runs one quiet update check for both HA NOVA and the NOVA Relay App. Relay calls keep a cache-only nudge as fallback; silence only that fallback with `HA_NOVA_NO_UPDATE_NUDGE=1`. `ha-nova check-update` still works manually.
- After `ha-nova update` succeeds, start a new AI client session to load the updated HA NOVA skills.

## What You Get

After setup, HA NOVA commands like `ha-nova:read`, `ha-nova:write`, and `ha-nova:review` are available inside Codex.

## Related

- Claude Code: `.claude/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Google Antigravity: `.antigravity/INSTALL.md`
- Hermes Agent: `.hermes/INSTALL.md`
