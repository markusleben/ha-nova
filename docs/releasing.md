# Release Checklist

## Version Bump

```bash
npm run bump -- <version>
```

Example:

```bash
npm run bump -- 0.3.1
```

Before any RC or final release work, hard-check the next version against the already published GitHub releases:

```bash
npm run verify:next-release-version -- v0.3.1
```

Hard rule:
- never reuse an already published stable version
- never create an RC/final tag whose base version is less than or equal to the latest published stable release

## Verify

```bash
npm run verify
```

This is the host-safe default gate.
It covers release metadata sync, production `npm audit` on both the root and `nova/` lockfiles, TypeScript, the safe Vitest suite, build/docs validation, and Go CLI verification.
Internally, it uses `test:safe:core` so the docs, onboarding, and release contract slices do not run twice. `test:safe:core` is pinned by `scripts/test/safe-core-files.json`, and the onboarding slice starts with `npm run verify:installers`, which checks the committed public installers directly before the wider onboarding contract slice runs.

The canonical production dependency audit helper is:

```bash
bash scripts/release/verify-npm-audit.sh
```

Hard rule:
- final tag version, `version.json`, `package.json`, `package-lock.json`, `.claude-plugin/plugin.json`, and `.claude-plugin/marketplace.json` must match
- the Claude marketplace entry must keep `source: "./"` so installed bundles stay on the local plugin update path instead of drifting to a remote repo source

## Release Preflight

Before every RC or final tag:

- audit open PRs first, especially Dependabot PRs plus anything touching workflows, installer/update paths, or release metadata
- classify each relevant open PR as `release blocker now` vs `separate later`
- do not pull a red or unreviewed workflow/release PR into the release train at the last minute
- for the release PR itself, wait for the actual Codex bot result on the final SHA
- review clearance is tied to the exact commit state that will be tagged

Fast-path rule during iteration:
- for the initial PR SHA and after each relevant fix, run only targeted local verification, push immediately if needed, and immediately trigger `@codex`
- after the PR exists, do not add extra local review gates in between; Codex bot + CI are the review path

Manifest-label rule:
- if the PR changes `package.json`, `package-lock.json`, `nova/package.json`, or `nova/package-lock.json`, add `manifest-review:approved` immediately after `gh pr create`
- do that before `@codex` and before `gh pr checks --watch`
- otherwise `manifest-review-gate` will fail even when the manifest delta is intentional and already maintainer-reviewed

## Release Worthiness

Do not cut a new version just because `main` moved.

Default rule:
- release when the merged delta changes shipped behavior, installer/update flow, release/runtime compatibility, or fixes a user-facing bug people can actually hit
- batch docs-only, test-only, process-only, and internal maintenance into the next real user-facing release unless they fix the release path itself

## Dependabot Fast Lane

Safe auto-merge is intentionally narrow.

Allowed fast lane:
- dev-only npm minor/patch updates that touch only `package.json` / `package-lock.json` (root or `nova/`)

Explicit exclusions:
- safe lane excludes toolchain-risk dependencies such as `vitest`, `vite`, `typescript`, `tsx`, `rollup`, `rolldown`, and `esbuild`
- workflow, installer, runtime, release, security, and non-manifest changes stay manual

Required protection posture on `main`:
- require `dependency-review` on `main`
- require `manifest-review-gate` on `main`
- `codex-review-gate` is advisory on `main`

## Release Candidate Gate

Every public release goes through a **tag-first dress rehearsal** first. It is
mandatory: it runs the exact stable pipeline against a prerelease, so any
pipeline breakage surfaces on the `-rcN` tag and never on the stable publish.

GitHub automation:
- `ci.yml` = normal PR / main quality gate
- `release-candidate.yml` = manual build + 3-runner bundle smoke, **no publish** (a quick pre-tag sanity check that pollutes no tags)
- `release.yml` = the tagged publish, used for **both** the `-rcN` rehearsal and the final tag

**Why tag-first.** The `release-tags-protection` ruleset blocks the Actions
token from creating `v*` tags, so no workflow can self-publish a release (an
RC-publish workflow step would only fail with HTTP 422). A maintainer — who can
bypass the ruleset — pushes the tag, and `release.yml` does the rest. GoReleaser
is pinned to the pushed tag via `GORELEASER_CURRENT_TAG`, so an `-rcN` tag and
the final tag may safely point at the same commit.

**Rehearsal steps:**

1. On the fully reviewed, merged `main` commit, verify the pipeline contract is
   intact. Run this as a maintainer (admin `gh auth`) so the no-App-bypass guard
   is verified — strict mode fails closed if the token cannot read the ruleset's
   bypass actors:
   ```bash
   HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS=1 bash scripts/release/verify-release-pipeline.sh
   ```
2. Push the rehearsal tag on that exact commit (maintainer bypass):
   ```bash
   git tag vX.Y.Z-rcN <reviewed-merge-sha>
   git push origin vX.Y.Z-rcN
   ```
3. Wait for `release.yml` to finish green: it runs verify + GoReleaser
   (auto-marked prerelease via `prerelease: auto`) + install bundles + the
   three-runner public-install smoke.
4. Verify the published RC over the real public install path (see
   "Supported RC selection" below), including at least one real Windows 11 +
   PowerShell onboarding proof on a clean VM/snapshot.
5. Only after the rehearsal is clean, cut the final tag (see "Final Publish").

The weekly `release-pipeline-audit.yml` workflow runs the same contract check
between releases so a broken publish path is caught within a week, not at the
next release.

## Release Channels

Use exactly two release shapes:

- `stable` = final public release tag `vX.Y.Z`
- `rc` = prerelease tag `vX.Y.Z-rcN`

Rules:
- `stable` is the only normal public channel
- `rc` is a tester-only prerelease shape, not a persistent channel users subscribe to
- normal install and normal `ha-nova check-update` / `ha-nova update` always target stable
- explicit prerelease selection is still supported via exact version pinning only

Supported RC selection:

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/<rc-tag>/install.sh | HA_NOVA_VERSION=vX.Y.Z-rcN bash
```

Windows:

```powershell
$env:HA_NOVA_VERSION = 'vX.Y.Z-rcN'
irm https://raw.githubusercontent.com/markusleben/ha-nova/<rc-tag>/install.ps1 | iex
```

Supported stable selection:

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/<stable-tag>/install.sh | HA_NOVA_VERSION=vX.Y.Z bash
```

Windows:

```powershell
$env:HA_NOVA_VERSION = 'vX.Y.Z'
irm https://raw.githubusercontent.com/markusleben/ha-nova/<stable-tag>/install.ps1 | iex
```

Installed runtime:

```bash
ha-nova update --version vX.Y.Z-rcN
```

Return an RC install to stable:

```bash
ha-nova update
```

## RC / Stable Validation Matrix

Minimum automated gate:
- `npm run verify`
- `npm run verify:next-release-version -- vX.Y.Z-rcN` before an RC tag
- `npm run verify:next-release-version -- vX.Y.Z` before the final tag
- `HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS=1 bash scripts/release/verify-release-pipeline.sh` before any release tag/publish step

Minimum manual gate before calling an RC ready:

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
12. confirm purge removed runtime/config/state/cache and deleted the relay auth token

Linux real-machine onboarding:
Helper:
- use `scripts/smoke/linux-headless-setup-check.sh` as the executable assistant for the SSH/headless Linux lane; pass the host and install command via env, never hardcode host-specific details in the repo
- by default the helper runs `HA_NOVA_NO_BROWSER=1 ha-nova setup`; for Google Antigravity proof, use `npm run test:desktop:linux:antigravity` or set `HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup antigravity'`; for Hermes desktop-keyring proof, set `HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup hermes'`; for Hermes service/gateway proof, set `HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup --service hermes'`
- `HA_NOVA_LIVE_SKIP_INSTALL=1` is for repair/debug passes only; it does not satisfy the full release-bound fresh-install proof for this lane
1. use a real Linux host with a desktop user session; when validating the SSH/headless recovery path, use an SSH shell inside that same logged-in user session
2. fresh stable install via the public `install.sh` flow
3. confirm Home Assistant auto-discovery prefers a real reachable result over an unverified `.local` guess when Avahi/mDNS evidence exists
4. if secure storage is unavailable because no Secret Service provider is running, confirm setup fails with the explicit provider prerequisite message instead of raw `org.freedesktop.secrets` D-Bus text
5. if secure storage is present but the default collection is still locked or uninitialized and the active Secret Service owner is GNOME Keyring, confirm interactive `ha-nova setup` offers the built-in local secure-storage recovery step before host/token work
6. if the same locked/uninitialized state exists on a non-GNOME Secret Service backend, confirm setup stays fail-loud with the explicit prerequisite guidance and does not pretend inline recovery is available
7. confirm the locked-flow copy asks for the existing local Linux keyring password, while the uninitialized-flow copy asks the user to create and confirm a new local Linux keyring password
8. confirm a wrong local keyring password keeps the user on the locked recovery step with a clear local secure-storage error, and confirm a correct password unlocks the keyring and resumes setup
9. confirm the fresh-init recovery path can create the default GNOME Keyring collection headlessly over SSH and then resumes setup without sending the user to a desktop GUI
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
13. confirm purge removed runtime/config/state/cache, deleted the relay auth token, and cleared `%LOCALAPPDATA%\ha-nova\uninstall-status.json`

Windows uninstall contract:
- bundle uninstall completes through a short background handoff once the helper and recovery marker are ready
- do not promise same-console completion on Windows after the handoff message
- do not run follow-up `ha-nova` commands from the same shell immediately after the handoff
- if HA NOVA is still present after 10 seconds, open a new shell and run `ha-nova doctor`

Rules:
- Windows uses a single supported install path: `install.ps1`
- supported public Windows onboarding means one `irm .../install.ps1 | iex` command in a local PowerShell console or Windows Terminal session
- if at least one supported client is already runnable, the supported public Windows path must not end with `Next step: ha-nova setup`
- if at least one supported client is already runnable, the supported public Windows path must positively prove that setup started automatically in the same session
- if no supported client is ready yet, the same public installer path is still valid when it installs HA NOVA locally, explains the missing client prerequisite, and exits cleanly
- `scripts/dev/windows-desktop-setup.ps1` proves same-version update smoke plus standard/purge uninstall semantics; the cross-version background replace path is still covered by the manual RC/stable matrix above
- do not present any package-manager alternative as an equal public path
- keep the matrix small but explicit; do not replace the commands above with vague "relevant tests" wording
- when Linux setup or secure-storage behavior changes, the release-bound manual matrix must include the Linux real-machine onboarding lane above; macOS/Windows proofs are not a substitute for it

### macOS Public Onboarding Lane

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

### Windows Public Onboarding Lane

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

## Release Notes Style

Release notes are user-facing, not an internal changelog.

Default structure:
- `New Features`
- `What To Watch`
- `Bug Fixes`

Rules:
- keep the headings fixed across releases
- keep notes short and concrete
- prefer user-visible outcomes over implementation detail
- do not list every small fix
- call out Windows installer/update/uninstall changes when they affect real users
- stable release notes must publish tag-pinned install commands, never `main` bootstrap URLs
- keep the supported Windows command plain and release-pinned:
  - `$env:HA_NOVA_VERSION = 'vX.Y.Z'`
  - `irm https://raw.githubusercontent.com/markusleben/ha-nova/vX.Y.Z/install.ps1 | iex`

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

## Final Publish

For a final stable release (only after the tag-first rehearsal above is clean):

1. merge the reviewed PR state
2. as a maintainer, tag the exact reviewed remote merge commit — the same commit
   the `-rcN` rehearsal validated — and push it (`git push origin vX.Y.Z`); the
   ruleset blocks the Actions token, so the tag is maintainer-pushed
3. let `release.yml` publish the raw binaries and install bundles
4. verify the published stable commands:

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/<stable-tag>/install.sh | HA_NOVA_VERSION=vX.Y.Z bash
```

Windows:

```powershell
$env:HA_NOVA_VERSION = 'vX.Y.Z'
irm https://raw.githubusercontent.com/markusleben/ha-nova/<stable-tag>/install.ps1 | iex
```

5. smoke:
   - `ha-nova version`
   - `ha-nova check-update`
   - same-version `ha-nova update`
   - `ha-nova uninstall --yes`

## Notes

- Legacy pre-Go installs are not updated in place; they must run the dedicated legacy cleanup script first, then reinstall with `install.sh` / `install.ps1`
- Bundle assets are installer payloads, not the normal user entrypoint
- Windows release communication should stay honest: one supported path, guided Home Assistant onboarding, no implied zero-touch setup
