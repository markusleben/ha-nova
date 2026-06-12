# Hermes Agent Install Overlay

> **Release status:** Hermes ships as early support (preview) starting with HA NOVA v0.5.0. The Ubuntu/GNOME Keyring Linux route is maintainer-validated; validation for other Linux Secret Service backends, macOS native, and Windows WSL2 is tracked in [docs/reference/hermes-platform-validation.md](../docs/reference/hermes-platform-validation.md). `README.md` lists Hermes in the public client list once that validation matrix is complete.

This page only covers Hermes-specific deltas.

For the stable installer, lifecycle commands, and general troubleshooting, use [README.md](../README.md) and the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest).

## Setup Choice

- Desktop terminal path: run `ha-nova setup hermes`.
- Service, headless, SSH, systemd user service, or gateway path: run `ha-nova setup --service hermes`.
- If you run `ha-nova setup hermes` and HA NOVA detects that the desktop keyring is locked or unavailable, setup asks whether to switch to the service/gateway token file.
- Install Hermes separately; HA NOVA handles the skills and onboarding, not the Hermes app itself.

## What Supported Means Here

Support status:
- `Supported` means HA NOVA intentionally supports that Hermes route and keeps install docs plus product behavior aligned to it.
- `Supported with limitation` means the route is real, but one UX slice is intentionally narrower today.
- `Not supported` means the route is outside the current HA NOVA support model.

Evidence status:
- `Maintainer-validated` means the maintainer ran that exact path on a real machine recently.
- `Community validation` is welcome and useful, but it is advisory only and does not replace maintainer release proof.
- `Planned / not yet validated` means the path is part of the intended support model, but the current release cycle does not claim a fresh maintainer proof yet.

## Platform Routing

Current support/evidence truth lives in [docs/reference/hermes-platform-validation.md](../docs/reference/hermes-platform-validation.md).

- macOS native Hermes follows the native macOS path.
- Linux native Hermes is the current focus for inline secure-storage recovery; GNOME Keyring is the actively maintainer-validated path.
- Linux non-GNOME Secret Service backends stay supported only when secure local storage already works; inline recovery is not claimed there yet.
- Windows native Hermes is not supported. Use WSL2 instead.
- Windows + WSL2 Hermes must run entirely inside the same WSL shell where `hermes --help` already works.

## Network Model

- Hermes is a local-first HA NOVA client, not a cloud-hosted control plane.
- The simple path today is to run Hermes on a machine you control with direct private reachability to Home Assistant and the HA NOVA Relay.
- In practice, that usually means the same home network or a private VPN/overlay route.
- A generic public VPS is not the intended beginner path today. It changes the trust and networking model, and it is not the current maintainer-validated Hermes story.
- If you want remote access later, the clean model is still local execution: a future remote entrypoint can forward requests into your trusted local Hermes + HA NOVA flow instead of moving execution onto a public server.
- Do not expose the HA NOVA Relay directly to the public internet. If you need remote reachability, use a private tunnel or VPN path you control.

## Linux Notes

- On Linux desktop sessions, HA NOVA uses the OS Secret Service / keyring for secure local token storage.
- For Hermes sessions that run without an unlocked desktop keyring, use `ha-nova setup --service hermes`. This stores only the Relay Auth Token in `~/.config/ha-nova/relay-token` with strict local file permissions.
- If HA NOVA asks for a local Linux keyring password, it stays on this machine. HA NOVA only uses it to unlock or create local secure storage. It is not your Relay token, not your Home Assistant token, and it is not sent to the Relay, Home Assistant, Hermes, or any AI provider.
- If no Secret Service provider is running, setup fails early with an explicit prerequisite message instead of raw `org.freedesktop.secrets` D-Bus errors.
- If Linux is using GNOME Keyring and the default collection is locked or uninitialized, `ha-nova setup hermes` can guide recovery inline.
- If Linux uses another Secret Service backend, keep the backend working first; HA NOVA does not pretend inline GNOME-only recovery exists there.
- If you run setup or repair over SSH and want desktop keyring storage, use the same logged-in desktop user session that owns the user D-Bus / Secret Service session. If that is not available, use the service path instead.

## Updates and Repair

- Run `ha-nova doctor` first. If Hermes is configured but not attached, doctor points you to `ha-nova setup hermes`.
- Connect or repair with `ha-nova setup hermes`.
- Repair service or gateway credentials with `ha-nova setup --service hermes`.
- `ha-nova setup hermes` also repairs older Hermes installs whose bare sub-skill directory names could make `skills_list` and `skill_view` disagree.
- On Windows with WSL2, run update and repair commands from the same WSL shell where Hermes is installed.
- Validate the current route and proof status in [docs/reference/hermes-platform-validation.md](../docs/reference/hermes-platform-validation.md).
- Validate the install with `ha-nova doctor`.
- Hermes does not surface HA NOVA update notices automatically yet. Use `ha-nova check-update` or `ha-nova doctor` when you want to check manually.

## Hermes-Specific Skill Layout

- HA NOVA installs Hermes skills under `~/.hermes/skills/ha-nova/`.
- The context skill stays `ha-nova`.
- Hermes sub-skills are installed in matching namespaced directories such as `~/.hermes/skills/ha-nova/ha-nova-read/` and `~/.hermes/skills/ha-nova/ha-nova-review/`.
- Each Hermes sub-skill keeps the same canonical identifier for both directory name and frontmatter `name:` to keep `skills_list` and `skill_view` aligned.

## What You Get

After setup, HA NOVA skills are available inside Hermes with the HA NOVA namespaced skill set.

## Community Validation

- If you validate Hermes on another macOS build, Linux distro, desktop session, or Secret Service backend, open the Hermes platform validation issue template and redact all tokens and secrets.
- Community validation helps us expand the matrix faster, but maintainer-validated rows stay the release signoff source of truth.

## Related

- Claude Code: `.claude/INSTALL.md`
- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
