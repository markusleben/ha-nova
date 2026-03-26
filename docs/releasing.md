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

Before creating a public release, run an RC pass.

GitHub automation:
- `ci.yml` = normal PR / main quality gate
- `release-candidate.yml` = manual RC build + bundle smoke, with optional prerelease bundle publish
- `release.yml` = final tagged publish

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
- `bash scripts/check-docs.sh`
- `npx vitest run tests/onboarding/release-contract.test.ts`
- `npx vitest run tests/onboarding/client-install-docs-contract.test.ts tests/onboarding/desktop-validation-contract.test.ts tests/onboarding/self-update-contract.test.ts`
- `cd cli && go test ./...`

Minimum manual gate before calling an RC ready:

macOS self-managed:
1. fresh stable install
2. exact RC install by rerunning the installer with `HA_NOVA_VERSION=vX.Y.Z-rcN`
3. `ha-nova check-update`
4. plain `ha-nova update`
5. verify latest stable restored
6. `ha-nova doctor`
7. `ha-nova uninstall --yes`

Windows self-managed:
1. fresh stable install via `install.ps1`
2. exact RC install by rerunning `install.ps1` with `HA_NOVA_VERSION=vX.Y.Z-rcN`
3. `ha-nova check-update`
4. plain `ha-nova update`
5. verify latest stable restored
6. `ha-nova doctor`
7. `ha-nova uninstall --yes`

Rules:
- Windows uses a single supported install path: `install.ps1`
- do not present any package-manager alternative as an equal public path
- keep the matrix small but explicit; do not replace the commands above with vague "relevant tests" wording

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
- state the supported Windows command plainly:
  - `irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex`

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
- npm wrappers:
  - `npm run dev:validation:harness`
  - `npm run test:desktop:macos`
  - `npm run test:desktop:windows:headless`
  - `npm run test:desktop:windows:rdp`

Rules:
- use them only for private validation against local or RC bundles
- do not run them against `main` or a public stable release without intent
- the harness serves `install.ps1` plus `dist/install-bundles/*`
- the Windows helper path is always the bundle installer path, not a package-manager path

Emergency macOS cleanup if a desktop helper was interrupted:

```bash
pkill -f 'npm run dev:validation:harness|start-local-validation-harness\\.sh|http\\.server 8917|vitest|mock-ha-relay\\.py|ha-nova setup' || true
```

## Final Publish

For a final stable release:

1. merge the reviewed PR state
2. tag the exact reviewed remote merge commit
3. let `release.yml` publish the raw binaries and install bundles
4. verify the published stable commands:

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

Windows:

```powershell
irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex
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
