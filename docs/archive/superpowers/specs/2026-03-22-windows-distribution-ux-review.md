# Windows Distribution UX Review

Date: 2026-03-22

## Goal

Choose the cleanest Windows UX path for the HA NOVA cross-platform Go CLI in 2025-2026.

## Project Constraints

- Official product entrypoint stays `OS bootstrapper + ha-nova setup`.
- Primary onboarding target is non-technical end users.
- UX ranks above maintainability.
- Maintainability ranks above minimal redundancy.
- Windows should not force a second product model that fights the existing cross-platform Go runtime.

## Options

1. PowerShell bootstrap + self-update
2. `winget` primary + PowerShell fallback
3. MSIX + App Installer

## Decision Criteria

- UX: 50%
- Maintainability: 30%
- Minimal redundancy: 20%

## Recommendation

Choose option 2 as the target architecture: public `winget` first once it is published and proven, with PowerShell fallback throughout.

Why:

- Best Windows-native install/upgrade/uninstall UX for a CLI without forcing HA NOVA into a Windows-specific packaging identity as the main path once the package is really live.
- Preserves the current one-link recovery/install path when `winget` is missing, delayed, blocked, or unsupported.
- Adds less release/process overhead than making MSIX/App Installer the primary path.

## Scoring Matrix

| Option | UX (50) | Maintainability (30) | Redundancy (20) | Total (100) | Verdict |
| --- | ---: | ---: | ---: | ---: | --- |
| PowerShell bootstrap + self-update | 32 | 21 | 18 | 71 | Good fallback, weak primary |
| `winget` primary + PowerShell fallback | 43 | 24 | 16 | 83 | Winner |
| MSIX + App Installer | 38 | 14 | 8 | 60 | Overkill for current product shape |

## Notes

- Until public publication + proof exists, `install.ps1` remains the current public Windows path.
- `winget` primary implies shipping a real `winget` package shape, ideally a portable package for `ha-nova.exe`, not just pointing `winget` at the current bootstrap script.
- Keep `ha-nova update` for cross-platform repair/update flows even if `winget upgrade` becomes the preferred Windows-native path.
- Do not make MSIX/App Installer primary unless HA NOVA later needs Windows package identity features strongly enough to justify the packaging/signing surface.

## Primary Sources

- Project constraints: `PROJECT.md`, `README.md`, `install.ps1`
- WinGet overview: <https://learn.microsoft.com/en-us/windows/package-manager/winget/>
- WinGet install command: <https://learn.microsoft.com/en-us/windows/package-manager/winget/install>
- WinGet upgrade command: <https://learn.microsoft.com/en-us/windows/package-manager/winget/upgrade>
- WinGet repository submission flow: <https://learn.microsoft.com/en-us/windows/package-manager/package/repository>
- WinGet portable package design: <https://raw.githubusercontent.com/microsoft/winget-cli/master/doc/specs/%23182%20-%20Support%20for%20installation%20of%20portable%20standalone%20apps.md>
- App Installer overview: <https://learn.microsoft.com/en-us/windows/msix/app-installer/app-installer-file-overview>
- App Installer update settings: <https://learn.microsoft.com/en-us/windows/msix/app-installer/update-settings>
- MSIX overview: <https://learn.microsoft.com/en-us/windows/msix/overview>
- Packaging a CLI executable as MSIX: <https://learn.microsoft.com/en-us/windows/apps/dev-tools/winapp-cli/guides/packaging-cli>
- Prepare to package a desktop application (MSIX): <https://learn.microsoft.com/en-us/windows/msix/desktop/desktop-to-uwp-prepare>
