# Windows Bundle Installer Design

## Goal

Deliver the simplest end-user install path for HA NOVA on Windows while aligning macOS and Windows on one distribution model.

## User Experience

- Windows entrypoint is a single PowerShell command that downloads and runs `install.ps1`.
- `install.ps1` downloads the matching HA NOVA release bundle, installs into `~/.local/share/ha-nova`, creates a public `ha-nova.cmd` launcher in `~/.local/bin`, and starts setup automatically.
- macOS stays a one-liner via `install.sh`, but now installs from release bundles instead of cloning the repo or requiring Node/npm.
- After install, the stable user command is `ha-nova`.
- If a pre-Go install is detected, the installer aborts and points to the dedicated `legacy-uninstall` one-liner instead of attempting migration.

## Distribution Model

- GitHub Releases now publish end-user install bundles in addition to raw `ha-nova` binaries.
- Bundle contents include:
  - `ha-nova` or `ha-nova.exe`
  - `skills/`
  - `docs/reference/`
  - `.claude-plugin/`
  - `version.json`
  - `bundle.json`
- Bundle archives contain a single top-level `ha-nova/` directory so native installers and the Go self-updater can extract consistently.

## Platform Runtime

- Windows onboarding is native Go + PowerShell bootstrap. No Git Bash dependency remains in the end-user path.
- Platform behavior is dispatched in the runtime:
  - macOS: Keychain, `open`, `pbcopy`
  - Windows: secure storage via OS credential APIs, PowerShell/browser helpers
  - Linux: Secret Service-backed secure storage
- File-based client installs use symlinks where possible and copy fallbacks where Windows symlink behavior is unreliable.

## Relay CLI Handling

- Public contract: `ha-nova relay ...`
- Legacy `~/.config/ha-nova/relay[.exe]` shims are no longer part of the main runtime contract.
- Install/update prefer the bundled `ha-nova` runtime; repo/dev flows can still use local Go sources.

## Update Model

- `ha-nova update` is handled by the Go runtime.
- Self-update supports bundle-backed installs via release-bundle refresh.
- Bundle refresh verifies sidecar SHA-256 checksums plus `bundle.json` metadata before replacing the local install.
- Repo/dev flows stay explicit and separate from the end-user contract.
- Version checks use GitHub Releases plus local bundle metadata/state.

## Release Pipeline

- GoReleaser produces raw `ha-nova` binaries.
- Release workflow additionally builds and uploads:
  - `ha-nova-macos-amd64.tar.gz`
  - `ha-nova-macos-arm64.tar.gz`
  - `ha-nova-linux-amd64.tar.gz`
  - `ha-nova-linux-arm64.tar.gz`
  - `ha-nova-windows-amd64.zip`

## Verification

- Contract/integration tests cover:
  - macOS installer contract
  - Windows installer contract
  - platform dispatch and Windows secure storage
  - per-client skill install behavior
  - Go-first update/uninstall shims and bundle refresh paths
- Full Vitest suite must stay green.
