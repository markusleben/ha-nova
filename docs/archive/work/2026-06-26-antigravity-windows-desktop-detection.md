# Antigravity Windows Desktop Detection

## Context

Windows VM smoke test:

- Antigravity Desktop was installed at `%LOCALAPPDATA%\Programs\antigravity\Antigravity.exe`.
- `agy` was not on `PATH`.
- HA NOVA `0.6.0` was installed and could not run `ha-nova status`.
- Antigravity skills were not installed under `%USERPROFILE%\.gemini\config\skills`.

## Decision

Treat a standard Windows Antigravity Desktop install as an Antigravity runtime marker, even when the CLI is not installed.

Accepted markers:

- `%LOCALAPPDATA%\Programs\antigravity\Antigravity.exe`
- `%LOCALAPPDATA%\Programs\Antigravity\Antigravity.exe`

Keep this KISS: do not parse `.lnk` shortcuts, inspect registry keys, or add COM/WMI logic.

## Client Adapter Line

Keep client support split by responsibility:

- `clients/registry.json`: product metadata only (`id`, `label`, `adapter_kind`, `supported_os`, setup metadata).
- `cli/client_status.go`: shared status flow only.
- `cli/client_<client>.go`: client-specific install layout, attachment layout, runtime markers, and cleanup.

Do not move concrete OS paths into the registry until at least two clients need the same declarative mechanism. Today the OS paths are adapter behavior, not public product metadata.

Linux stays command based for Antigravity:

- `agy` is the CLI marker.
- `antigravity` / `antigravity-ide` are desktop launcher markers when present on `PATH`.
- Stale profile directories under `~/.gemini` do not count as installed runtime markers.
- Do not guess tarball extraction paths such as `/opt/antigravity`; Linux packaging is not stable enough to hard-code that as release behavior.

## Validation

Add regression coverage for:

- Windows Desktop-only runtime detection without `agy`.
- Windows Desktop-only setup choice readiness without `agy`.
- Linux Desktop launcher detection through `antigravity` / `antigravity-ide` commands on `PATH`.
- Windows public onboarding helper readiness for Antigravity Desktop-only.

## E2E Result

Windows VM `win11-test-local` passed the Antigravity Desktop-only private validation flow on 2026-06-28:

- Antigravity Desktop marker present: `%LOCALAPPDATA%\Programs\antigravity\Antigravity.exe`.
- `agy` absent from `PATH`.
- Local bundle installed HA NOVA `0.7.0`.
- `ha-nova setup antigravity` completed.
- `ha-nova doctor` completed in a separate process.
- Same-version update completed without changing the installed version.
- Antigravity root and subskill files were present.
- Standard uninstall kept config and test relay token.
- Purge uninstall removed config, cache, state, runtime, and test relay token.

Windows validation must place `HA_NOVA_TEST_KEYRING_FILE` under `%APPDATA%\ha-nova`, not `%USERPROFILE%\.config\ha-nova`; the latter is a legacy path migrated by HA NOVA on Windows.
