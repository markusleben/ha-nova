# Hermes Platform Validation

This is the active truth source for Hermes support evidence in HA NOVA.

Use it to answer two separate questions cleanly:

- Is this route part of the intended supported product path?
- Has the maintainer actually re-proved that exact route on a real machine recently?

## Support Status

- `Supported`: HA NOVA intentionally supports the route and keeps docs plus product behavior aligned to it.
- `Supported with limitation`: the route is real, but one UX slice is intentionally narrower today.
- `Not supported`: the route is outside the current HA NOVA support model.

## Evidence Status

- `Maintainer-validated`: the maintainer ran this exact route on a real machine recently enough to cite it for release confidence.
- `Community validation`: a user reported a real result for this route. Helpful signal; not release signoff on its own.
- `Planned / not yet validated`: the route belongs to the support model, but does not yet have a fresh maintainer proof in this matrix.

## Current Matrix

| Platform | OS / distro / version | Session / DE | Secret Service backend | Support status | Evidence status | Hermes runtime detected | HA NOVA skills attached | `ha-nova setup hermes` | `ha-nova doctor` | Inline recovery available | Last proof date | Notes |
|:---------|:----------------------|:-------------|:------------------------|:---------------|:----------------|:------------------------|:------------------------|:-----------------------|:-----------------|:--------------------------|:----------------|:------|
| Linux native | Ubuntu 24.04.1 LTS | Logged-in desktop session | GNOME Keyring | Supported | Maintainer-validated | Yes | Yes | Yes | Yes | Yes | 2026-04-18 | Current real-machine Hermes/Linux proof. Also validated over SSH inside the same logged-in desktop user session. |
| macOS native | macOS native path | Local terminal session | n/a | Supported | Planned / not yet validated | n/a | n/a | n/a | n/a | n/a | n/a | Native Hermes is part of the intended support model. Fresh maintainer release proof still needs to be recorded here. |
| Linux native | Distribution varies | Desktop session with non-GNOME Secret Service backend | Secret Service backend other than GNOME Keyring | Supported with limitation | Planned / not yet validated | Depends on local backend health | Depends on local backend health | Depends on local backend health | Depends on local backend health | Not claimed | n/a | Supported only when secure local storage already works. Locked/fresh-init inline recovery is currently GNOME Keyring only. |
| Windows native | Windows 10 / 11 | PowerShell / Windows Terminal | Windows Credential Manager is not the Hermes target path | Not supported | n/a | No | No | No | No | Not claimed | n/a | Native Windows Hermes is not part of the HA NOVA support model. Use WSL2 instead. |
| Windows + WSL2 | Windows host + supported WSL distro | WSL shell | Unknown / not yet validated | Supported | Planned / not yet validated | n/a | n/a | n/a | n/a | Not claimed | n/a | Run Hermes and HA NOVA entirely inside the same WSL shell. Fresh maintainer release proof still pending. |

## Repair Check

- `ha-nova doctor` is the first repair surface for Hermes.
- If Hermes is installed but HA NOVA is still on an older mismatched skill layout, `doctor` should report that Hermes is configured but not attached and point to `ha-nova setup hermes`.
- `ha-nova setup hermes` is the supported repair path.
- After repair, `ha-nova doctor` should report `Hermes Agent ready now`.

## Community Validation Rules

- Community reports are advisory only. They help expand the matrix, but they do not replace maintainer-validated release proof.
- Never paste Home Assistant long-lived access tokens, relay auth tokens, keyring passwords, or unredacted shell history into an issue.
- Prefer structured reports through the Hermes platform validation issue template so distro, session, and backend details stay comparable.

## Current Practical Read

- Best current maintainer proof: Ubuntu 24.04.1 LTS desktop session with GNOME Keyring.
- Best current Windows guidance: WSL2 is the supported Windows route; fresh maintainer proof is still pending, and native Windows Hermes is not supported.
- Best current Linux caveat: inline secure-storage recovery is GNOME Keyring only; other Secret Service backends must already be healthy.
