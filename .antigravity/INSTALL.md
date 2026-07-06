# Google Antigravity Install Overlay

This page only covers Google Antigravity-specific deltas.

For the stable installer, lifecycle commands, and general troubleshooting, use [README.md](../README.md) and the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest).

## Setup Choice

- Use the normal HA NOVA installer flow.
- In the setup wizard, choose `Google Antigravity`.
- Install the Antigravity client separately; HA NOVA handles the skills and onboarding, not the Antigravity app itself.

## Former Gemini Users

- Google Antigravity is the current Google client path for HA NOVA.
- `ha-nova setup gemini` is kept as a legacy alias and resolves to Antigravity.
- Existing HA NOVA-owned legacy Gemini skill copies under `~/.gemini/skills/ha-nova*` are cleaned during Antigravity setup.

## Windows Notes

- Install Google Antigravity Desktop or CLI first, then run the HA NOVA installer.
- If you use Antigravity CLI, open a fresh PowerShell window and verify:

```powershell
agy --version
```

- If you use Desktop only on Windows, HA NOVA detects the standard per-user install at `%LOCALAPPDATA%\Programs\antigravity\Antigravity.exe` or `%LOCALAPPDATA%\Programs\Antigravity\Antigravity.exe`; otherwise make `agy` available before running setup.
- If you use CLI and the command fails, fix that first before running `ha-nova setup`.
- Antigravity has basic Windows validation for this release.

## Linux Notes

- Install Google Antigravity Desktop, Antigravity IDE, or Antigravity CLI first, then run the HA NOVA installer.
- If you use Antigravity CLI, verify `agy --version`.
- If you use Desktop or IDE only, a working `antigravity` or `antigravity-ide` launcher is enough; `agy` is not required.
- HA NOVA intentionally does not guess tarball extraction paths such as `/opt/antigravity`.

## macOS Notes

- Install Google Antigravity Desktop, Antigravity IDE, or Antigravity CLI first, then run the HA NOVA installer.
- HA NOVA detects `agy`, `/Applications/Antigravity.app`, and `/Applications/Antigravity IDE.app`.

## Updates and Repair

- Connect or repair with `ha-nova setup antigravity`.
- Validate the install with `ha-nova doctor`.
- Antigravity does not surface HA NOVA update notices automatically yet. Use `ha-nova check-update` or `ha-nova doctor` when you want to check manually.
- `ha-nova setup gemini` is kept as a legacy alias and resolves to Antigravity.

## Antigravity-Specific Skill Layout

- HA NOVA installs the Antigravity context skill under `~/.gemini/config/skills/ha-nova/`.
- HA NOVA installs Antigravity sub-skills under `~/.gemini/config/skills/ha-nova-*`.
- The `ha-nova-*` naming avoids conflicts with other Antigravity skills.
- HA NOVA-owned legacy Gemini flat skills under `~/.gemini/skills/ha-nova*` are removed during Antigravity setup.

## What You Get

After setup, HA NOVA commands like `ha-nova:ha-nova-read`, `ha-nova:ha-nova-write`, and `ha-nova:ha-nova-review` are available inside Google Antigravity.

## Related

- Claude Code: `.claude/INSTALL.md`
- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Hermes Agent: `.hermes/INSTALL.md`
