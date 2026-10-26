# Codex Install Overlay

This page only covers Codex-specific deltas.

For the stable installer, lifecycle commands, and general troubleshooting, use [README.md](../README.md) and the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest).

## Setup Choice

- Use the normal HA NOVA installer flow.
- The setup page finds Codex on this computer and sets it up: the `codex` command, or on a Mac also the Codex desktop app (now the ChatGPT desktop app) without the command. In the terminal wizard, choose `Codex`.
- HA NOVA's skills go to `~/.agents/skills/ha-nova`. The `codex` command, the Codex IDE extension, and Codex in the desktop app (macOS, Windows) all read them. In the app, start a new chat to load HA NOVA.
- Install the Codex client separately (the Codex CLI, the IDE extension, or the desktop app); HA NOVA handles the skills and onboarding, not Codex itself.

## Windows Notes

- Follow Codex's own Windows setup first, then run the HA NOVA installer.
- Windows support for Codex is still early and not fully tested yet.

## Updates and Repair

- Connect or repair with `ha-nova setup codex`.
- Headless or SSH session without an unlocked desktop keyring (macOS/Linux): run `ha-nova setup --service codex`. It keeps HA NOVA's credential in a protected file under `~/.config/ha-nova/`.
- Validate the install with `ha-nova doctor`. It also flags an older or second HA NOVA copy that Codex would load (in `~/.agents/skills` or the older `~/.codex/skills`); `ha-nova setup codex` moves HA NOVA's own old copies into `~/.config/ha-nova/backups/`.
- Before the first Home Assistant task, the first HA NOVA skill used runs one quiet update check for both HA NOVA and the NOVA Relay App. Relay calls keep a cache-only nudge as fallback; silence only that fallback with `HA_NOVA_NO_UPDATE_NUDGE=1`. `ha-nova check-update` still works manually.
- After `ha-nova update` succeeds, start a new AI client session to load the updated HA NOVA skills.

## What You Get

After setup, HA NOVA commands like `ha-nova:read`, `ha-nova:write`, and `ha-nova:review` are available inside Codex.

## Related

- Claude Code: `.claude/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Google Antigravity: `.antigravity/INSTALL.md`
- Hermes Agent: `.hermes/INSTALL.md`
