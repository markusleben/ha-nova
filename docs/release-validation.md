# Release Validation Matrix

Manual, release-bound proofs for RC and stable releases. The release runbook
(`docs/releasing.md`) decides when they run; this file owns what they prove.

## Minimum Manual Gate

Before calling an RC ready:

macOS self-managed lifecycle:
1. before this lane, make sure at least one supported client already runs from the same local macOS Terminal session
2. fresh stable install via the public `install.sh` flow in a local interactive macOS Terminal
3. confirm setup starts in the same session; fail if the supported path drops straight to a manual `ha-nova setup` instruction
4. exact RC install by rerunning the installer with `HA_NOVA_VERSION=vX.Y.Z-rcN`
5. `ha-nova check-update`
6. plain `ha-nova update`
7. verify latest stable restored
8. `ha-nova doctor`
9. `ha-nova uninstall --yes`
10. confirm standard uninstall removed runtime/state/cache and kept the Home Assistant config/token
11. reinstall the runtime, then run `ha-nova uninstall --yes --purge`
12. confirm purge removed runtime/config/state/cache, deleted the relay auth token, and reported only the two opaque uninstall-safety markers retained outside managed directories
13. when Home Assistant Cloud support changes, configure a real Cloud profile before the stable-to-RC update, run `ha-nova cloud unlock` after each stable-to-RC, RC-to-stable, and reinstall transition, and confirm the selected existing current/pending Keychain slot works afterward with ordinary no-UI `cloud status`
14. confirm each selected current/pending/retiring slot opens at most one native prompt in a foreground action, the total prompt count stays bounded by those selected slots, and no refresh token appears in argv, environment, logs, diagnostics, or command output; current unsigned macOS artifacts cannot enable Cloud Beta, so a stable signing identity and this successful update proof are both mandatory

Linux real-machine onboarding:
Helper:
- use `scripts/smoke/linux-headless-setup-check.sh` as the executable assistant for the SSH/headless Linux lane; pass the host and install command via env, never hardcode host-specific details in the repo
- by default the helper runs `HA_NOVA_NO_BROWSER=1 ha-nova setup`; for Google Antigravity proof, use `npm run test:desktop:linux:antigravity` or set `HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup antigravity'`; for Hermes desktop-keyring proof, set `HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup hermes'`; for Hermes service/gateway proof, set `HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup --service hermes'`
- `HA_NOVA_LIVE_SKIP_INSTALL=1` is for repair/debug passes only; it does not satisfy the full release-bound fresh-install proof for this lane
1. use a real Linux host with a local graphical desktop session; validate the fail-closed SSH/headless path separately
2. fresh stable install via the public `install.sh` flow
3. confirm Home Assistant auto-discovery prefers a real reachable result over an unverified `.local` guess when Avahi/mDNS evidence exists
4. if secure storage is unavailable because no Secret Service provider is running, confirm setup fails with the explicit provider prerequisite message instead of raw `org.freedesktop.secrets` D-Bus text
5. with GNOME Keyring and KWallet Secrets, confirm a locked or uninitialized default collection makes local interactive `ha-nova setup` offer native Secret Service recovery before host/token work
6. confirm the CLI terminal never asks for, reads, or confirms the keyring master password; the provider-owned desktop prompt is the only place that may request it
7. confirm one setup action opens at most one provider-owned prompt: Unlock for a locked collection or CreateCollection for an uninitialized collection
8. dismiss the provider prompt and confirm setup stays on the matching recovery state; complete the prompt and confirm setup resumes
9. confirm SSH (including X11 forwarding), a text console, a non-graphical `XDG_SESSION_TYPE`, a missing explicit `DBUS_SESSION_BUS_ADDRESS`, and a non-TTY run never launch a prompt and fail with local graphical-session guidance; confirm an unanswered prompt times out instead of hanging
10. finish setup, then run `ha-nova doctor`
11. if the lane includes Hermes or a pre-fix Hermes bundle, confirm `ha-nova doctor` reports a repairable Hermes mismatch instead of silently hiding it
12. run `ha-nova setup hermes` and confirm the Hermes route repairs cleanly
13. re-run `ha-nova doctor` and confirm it reports `Hermes Agent ready now`
14. confirm the relay token is saved and can be reused by a second `ha-nova setup` / `ha-nova doctor` invocation without repeating recovery
15. for the Hermes service/gateway lane, run `ha-nova setup --service hermes`, then `ha-nova doctor`, then one authenticated relay call from a fresh SSH/service-like shell without an unlocked desktop keyring
16. for the Hermes service/gateway lane, run `ha-nova uninstall --yes --purge` and confirm the service token file is removed

Windows self-managed:
1. on fresh-profile runs, preinstall at least one supported client and verify it already runs on that exact machine/session
2. fresh stable install via `install.ps1` from a local PowerShell console or Windows Terminal session
3. confirm the supported path starts guided setup automatically; fail if it ends with a manual `ha-nova setup` instruction
4. exact RC install by rerunning `install.ps1` with `HA_NOVA_VERSION=vX.Y.Z-rcN`
5. confirm the RC install uses the same guided setup contract: no second terminal command, browser/GUI steps allowed
6. `ha-nova check-update`
7. plain `ha-nova update`
8. verify latest stable restored
9. `ha-nova doctor`
10. `ha-nova uninstall --yes`
11. confirm standard uninstall removed runtime/state/cache, cleared `%LOCALAPPDATA%\ha-nova\uninstall-status.json`, and kept the Home Assistant config/token
12. reinstall the runtime, then run `ha-nova uninstall --yes --purge`
13. confirm purge removed runtime/config/state/cache, deleted the relay auth token, cleared `%LOCALAPPDATA%\ha-nova\uninstall-status.json`, and reported only the two opaque uninstall-safety markers retained outside managed directories

Windows uninstall contract:
- bundle uninstall completes through a short background handoff once the helper and recovery marker are ready
- do not promise same-console completion on Windows after the handoff message
- do not run follow-up `ha-nova` commands from the same shell immediately after the handoff
- if HA NOVA is still present after 10 seconds, open a new shell and run `ha-nova doctor`

Rules:
- Windows uses a single supported install path: `install.ps1`
- before downloading, the Windows installer requires Windows 10 / Server 2016 or later, PowerShell 5.1 or later, an amd64 process (including x64 emulation on ARM64), a writable per-user install root, and working TLS access to GitHub
- supported public Windows onboarding means one `irm .../install.ps1 | iex` command in a local PowerShell console or Windows Terminal session
- if at least one supported client is already runnable, the supported public Windows path must not end with `Next step: ha-nova setup`
- if at least one supported client is already runnable, the supported public Windows path must positively prove that setup started automatically in the same session
- if no supported client is ready yet, the same public installer path is still valid when it installs HA NOVA locally, explains the missing client prerequisite, and exits cleanly
- when an installer cannot start the interactive wizard, it must print `ha-nova setup` as the exact next command and explain that setup asks for the six-digit NOVA Home Base pairing code
- `scripts/dev/windows-desktop-setup.ps1` proves same-version update smoke plus standard/purge uninstall semantics; the cross-version background replace path is still covered by the manual RC/stable matrix above
- do not present any package-manager alternative as an equal public path
- keep the matrix small but explicit; do not replace the commands above with vague "relevant tests" wording
- when Linux setup or secure-storage behavior changes, the release-bound manual matrix must include the Linux real-machine onboarding lane above; macOS/Windows proofs are not a substitute for it

## macOS Public Onboarding Lane

This is the release-bound macOS host proof. It complements the private RC helpers; they do not replace it.

Prerequisites for this lane:
- use a local interactive macOS Terminal session
- if you want to prove the full guided-setup path, preinstall at least one supported client and verify it already runs from that exact shell

Supported public outcomes:
- if at least one supported client is already runnable, the public `install.sh` flow must start `ha-nova setup` in the same Terminal session
- if no supported client is ready yet, the same public `install.sh` flow is still valid when it installs HA NOVA locally, prints the missing client prerequisite guidance, and exits cleanly

Entry point:
- `npm run dev:validation:harness`
- then run the printed macOS install command for your host architecture from the same local Terminal session

Rules:
- use the real public `install.sh` flow; do not set `HA_NOVA_NO_SETUP=1`
- keep this lane manual and host-local; private helpers with `HA_NOVA_NO_SETUP=1` do not prove the same-session setup handoff
- keep the missing-client outcome explicit; do not treat it as a hard installer failure

## Windows Public Onboarding Lane

This is the release-bound Windows UX proof. It is separate from `npm run verify` and separate from the private RC helper scripts.

Prerequisites for this lane:
- on `clean` / fresh-profile runs, preinstall at least one supported client and verify it already runs from the same local shell
- native Windows Claude also needs Git for Windows / Git Bash
- native Windows Google Antigravity must provide either a standard per-user Desktop install (`%LOCALAPPDATA%\Programs\antigravity\Antigravity.exe` or `%LOCALAPPDATA%\Programs\Antigravity\Antigravity.exe`) or CLI (`agy`)
- for this native Windows lane, counted ready clients are Claude Code with Git Bash, Google Antigravity Desktop/CLI, Codex CLI, and OpenCode; Hermes does not count on native Windows

Additional supported public outcome:
- if no supported client is ready yet, the same helper may also be used to prove the graceful install-only path: HA NOVA installs locally, shows the missing client prerequisite guidance, and does not fail the installer
- optional Desktop-only proof: set `HA_NOVA_REQUIRE_ANTIGRAVITY_DESKTOP_ONLY=1` for the public Windows helper; that lane requires the standard Antigravity Desktop marker and fails if readiness only comes from `agy`

Supported matrix for this lane:
- Windows 10 + PowerShell 5.1 + standard user + fresh profile
- Windows 10 + PowerShell 7 + standard user + fresh profile
- Windows 11 + PowerShell 5.1 + standard user + fresh profile
- Windows 11 + PowerShell 7 + standard user + fresh profile
- stale uninstall marker
- stable-over-stable reinstall/update
- rerunning the same public one-liner after a successful install

Entry point:
- `npm run test:desktop:windows:public`
- or `powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev\windows-public-onboarding.ps1`

Rules:
- use the real public `install.ps1` flow; do not set `HA_NOVA_NO_SETUP=1`
- keep this lane release-bound; do not move it into host-safe PR CI
- RC validation uses the same public UX contract as stable, only with an RC bundle source

Evidence record for each run:
- `windows_version`
- `powershell_version`
- `host_form`
- `standard_user`
- `install_source`
- `start_state`
- `ready_clients`
- `expected_public_result`
- `installer_exit_code`
- `local_install_completed`
- `setup_auto_started`
- `second_terminal_command_needed`
- `manual_fallback_displayed`
- `client_prerequisite_guidance_displayed`
- `final_verdict`

Release defaults:
- RC before tag: local/private bundle proof is acceptable only as a pre-tag sanity check
- RC after publish: at least one real public Windows onboarding proof on Windows 11 + PowerShell 7 against the published prerelease installer
- Stable: full 4-host matrix plus `reinstall` and `stale-uninstall-marker` runs before final publish when installer/onboarding changed

## Desktop Validation Helpers

Private/manual validation entrypoints:

- `scripts/dev/start-local-validation-harness.sh`
- `scripts/dev/mock-ha-relay.py`
- `scripts/dev/macos-private-rc-suite.sh`
- `scripts/dev/macos-private-rc-smoke.sh`
- `scripts/dev/macos-private-rc-setup-all.sh`
- `scripts/dev/macos-private-rc-client.sh`
- `scripts/dev/windows-clean-test-state.ps1`
- `scripts/dev/windows-private-rc-install.ps1`
- `scripts/dev/windows-desktop-setup.ps1`
- `scripts/dev/windows-public-onboarding.ps1`
- npm wrappers:
  - `npm run dev:validation:harness`
  - `npm run test:desktop:macos`
  - `npm run test:desktop:windows:headless`
  - `npm run test:desktop:windows:rdp`
  - `npm run test:desktop:windows:public`

Rules:
- use the macOS/private Windows helpers only for private validation against local or RC bundles
- do not run them against `main` or a public stable release without intent
- `scripts/dev/macos-private-rc-suite.sh` is the canonical technical start for private macOS lanes because it rebuilds RC bundles and starts the local bundle server
- `scripts/dev/macos-private-rc-smoke.sh`, `scripts/dev/macos-private-rc-setup-all.sh`, and `scripts/dev/macos-private-rc-client.sh` are leaf lanes; run them only after the suite or an equivalent fresh bundle/server setup
- the private macOS helpers set `HA_NOVA_NO_SETUP=1`; they do not prove the public same-session `install.sh` setup handoff
- the harness serves `install.ps1` plus `dist/install-bundles/*`
- `scripts/dev/macos-private-rc-smoke.sh` proves private standard-remove plus purge cleanup against fresh temp homes
- `scripts/dev/macos-private-rc-setup-all.sh` proves private same-version setup/doctor/update/uninstall lifecycle plus standard config/token retention
- `scripts/dev/macos-private-rc-client.sh` proves client-specific artifact install/remove results; `setup-all` is not a substitute for those client assertions
- the Windows helper path is always the bundle installer path, not a package-manager path
- `scripts/dev/windows-desktop-setup.ps1` is a private mechanics/lifecycle lane, not the source of truth for enhanced setup UI fidelity
- `scripts/dev/windows-desktop-setup.ps1` must verify post-update version stability, standard uninstall semantics, purge uninstall semantics, and cleared Windows uninstall recovery markers
- Windows bundle uninstall is background-complete, not same-console-complete; private helpers must wait for recovery-marker clearance explicitly
- `scripts/dev/windows-desktop-setup.ps1` proves token keep/delete semantics through the file-based test keyring override for deterministic desktop validation; real Windows Credential Manager interop stays covered by runtime tests and `tests/onboarding/windows-keyring-interop-contract.test.ts`
- `scripts/dev/windows-private-rc-install.ps1` must wait for the background uninstall to finish and fail if `%LOCALAPPDATA%\ha-nova\uninstall-status.json` remains

Public Windows onboarding proof:
- `scripts/dev/windows-public-onboarding.ps1` is the only helper in this group that targets the public end-user contract
- it must continue to execute the real installer inline, without extra output piping or suppression around the installer run itself
- it exists to prove the supported `install.ps1` one-liner either starts setup automatically without a second terminal command or lands in the documented local-install-plus-missing-client-guidance path
- it writes a small evidence record for release signoff; keep the record structured, not screenshot-only

Emergency macOS cleanup if a desktop helper was interrupted:

```bash
pkill -f 'npm run dev:validation:harness|start-local-validation-harness\\.sh|http\\.server 8917|vitest|mock-ha-relay\\.py|ha-nova setup' || true
```
