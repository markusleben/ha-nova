# Release Checklist

## Version Bump

```bash
npm run bump -- 0.2.0
```

This updates all 5 version files atomically:
- `version.json` (source of truth)
- `package.json`
- `package-lock.json`
- `.claude-plugin/plugin.json`
- `.claude-plugin/marketplace.json`

To also bump `min_relay_version` (when skills require relay endpoints not present in older relay versions), edit `version.json` manually after the bump.

## Verify

```bash
npm run verify
```

This is the host-safe default gate.
It covers release metadata sync, TypeScript, the safe Vitest suite, build/docs validation, and Go CLI verification.
It must not open browsers or touch real secure stores on the maintainer host.

Hard rule:
- final tag version, `version.json`, `package.json`, `package-lock.json`, `.claude-plugin/plugin.json`, and `.claude-plugin/marketplace.json` must match
- the Claude marketplace entry must keep `source: "./"` so installed bundles stay on the local plugin update path instead of drifting to a remote repo source
- Claude marketplace source parity must be regression-covered for all three shapes: plain GitHub URL, structured GitHub repo source, and structured GitHub repo source with pinned `ref`; a pinned `ref` must never compare equal to the floating default source

## Release Preflight

Before every RC or final tag:

- audit open PRs first, especially Dependabot PRs plus anything touching workflows, GoReleaser, installer/update paths, or release metadata
- classify each relevant open PR as `release blocker now` vs `separate later`
- do not pull a red or unreviewed workflow/release PR into the release train at the last minute
- for the release PR itself, wait for the actual Codex bot result; `codex-review-gate` timeout alone is not enough
- review clearance is tied to the exact commit state that will be tagged, not to the branch/topic in general
- a clean bot result applies only to the exact commit SHA it reviewed; any later SHA is unreviewed until the cycle completes again
- if any relevant delta appears after the last bot-reviewed commit, stop release work and restart the full cycle: push -> `@codex review` -> actual bot result -> checks green
- run at least two independent subagent review passes on the final release-bound delta with distinct focuses before relying on Codex review

## Release Worthiness

Do not cut a new version just because `main` moved.

Default rule:
- release when the merged delta changes shipped behavior, installer/update flow, release/runtime compatibility, or fixes a user-facing bug people can actually hit
- batch docs-only, test-only, process-only, and internal maintenance into the next real user-facing release unless they fix the release path itself

Examples that usually justify a version:
- new user-visible capability
- user-facing bug fix
- installer/update/uninstall behavior change
- client integration behavior change
- published artifact or release automation fix that affects real installs

Examples that usually do not justify an immediate standalone version:
- release-note wording only
- internal process/docs policy changes
- test-only hardening
- CI-only cleanup that does not affect shipped artifacts

## Dependabot Fast Lane

Keep Dependabot automation narrow:

- safe lane: dev-only npm minor/patch updates that touch only `package.json` / `package-lock.json`
- safe lane excludes toolchain-risk dependencies such as `vitest`, `vite`, `typescript`, `tsx`, `rollup`, `rolldown`, and `esbuild`; those stay manual even if they are dev-only manifest bumps
- manual lane: workflow files, release automation, installer/update paths, runtime/security-sensitive paths, and anything outside the narrow manifest lane
- require `dependency-review` on `main` so auto-merge cannot bypass dependency-risk screening
- require `manifest-review-gate` on `main` so non-safe manifest changes still need an explicit maintainer acknowledgement
- `codex-review-gate` is advisory on `main`; safe-lane auto-merge must not wait on Codex bot latency

Reason:
- this keeps low-risk maintenance fast without teaching the repo to auto-merge changes that alter the release runway or shipped behavior
- maintainers can verify the live GitHub setting drift with `bash scripts/release/verify-github-main-protection.sh`

## Codex Review Policy

- `codex-review-gate` is advisory on `main`, not a required branch-protection check
- native GitHub gates stay hard on `main`: required reviews, CODEOWNERS-sensitive paths, `ci-gate`, `analyze`, `dependency-review`, and `manifest-review-gate`
- for release-bound or otherwise high-risk deltas, still wait for an actual Codex bot result on the final SHA before merge/tag/release
- use `bash scripts/release/verify-github-main-protection.sh` to confirm the live `main` branch protection still matches this policy

Reason:
- GitHub branch protection is most reliable when only deterministic native checks block merges; Codex remains a strong review layer without turning bot latency or timeout semantics into merge fragility

## Release Candidate Gate

Before creating a public release, run an RC pass.

GitHub automation:
- `ci.yml` = normal PR / main quality gate
- `release-candidate.yml` = manual RC build + bundle smoke, with optional prerelease bundle publish
- `release.yml` = final tagged publish

## Release Notes Structure

GitHub release notes still need the stable top-level release body sections:
- `Why This Release Exists`
- `What You Get`
- `Upgrade Notes`

Inside that frame, keep the changelog grouped into the user-facing categories below.

## Release Notes Style

Release notes are user-facing, not an internal changelog.

Prioritize exactly these questions:
- What is new?
- What should users watch out for?
- What important bug fixes landed?

Default structure:
- `New Features`
- `What To Watch`
- `Bug Fixes`

Rules:
- Keep notes short and concrete.
- Prefer user-visible outcomes over implementation detail.
- Do not list every small fix.
- Only include bug fixes that are severe, user-facing, or likely to affect trust/support load.
- Only include `What To Watch` when there is a real behavior change, migration step, required action, compatibility note, or breaking change.
- If there is nothing users need to do, omit `What To Watch` entirely.
- If there are breaking changes, put them under `What To Watch` in plain language first. Technical detail can follow in one short bullet if needed.
- Call out client-specific behavior only when it matters to users. Example: only Claude currently has the extra automatic SessionStart update banner.

Suggested template:

```md
## New Features

- ...
- ...

## What To Watch

- ...

## Bug Fixes

- ...
```

## GitHub Protection Setup

Before the first public release, configure GitHub so final publish stays maintainer-controlled and can later grow into an approval-gated flow.

- Create a `production` environment and attach the final `Release` job to it.
- Enable `required reviewers` on `production` once the repo has a second maintainer who can approve releases.
- Enable `prevent self-review` together with `required reviewers`.
- Store final release secrets only in `production`.
- Protect `v*` tags with a ruleset so only maintainers can create or update release tags.

Current repo reality:
- the repo currently has one direct admin collaborator
- the immediate hard release guard is therefore protected `v*` tags
- the reviewer gate becomes meaningful as soon as a second maintainer exists
- the active `v*` tag ruleset currently uses the verified repository-role bypass that GitHub accepts here; a direct `User` bypass was tested and did not work correctly
- `production` intentionally stays without a branch/tag policy; the `v*` restriction lives in the tag ruleset, which is clearer and less fragile here

`release-candidate.yml` is the rehearsal path.
`release.yml` is the protected publish path.

Maintainer-only step:
- final tagged release publish stays maintainer-driven
- if `required reviewers` is configured, wait for that approval before approving the protected `production` environment

### 1. GitHub RC Workflow

Run `Release Candidate` via `workflow_dispatch`.

It must complete:
- `npm run verify`
- `goreleaser build --snapshot --clean`
- `./scripts/release/build-install-bundle.sh`
- bundle smoke on `ubuntu-latest`, `macos-latest`, `windows-latest`

Optional public RC path:
- set `publish_release=true`
- set `version_tag=vX.Y.Z-rcN`
- the workflow will publish a GitHub prerelease with the install bundles after smoke passes
- use a commit you actually want external testers to install; RC publish does not enforce `main` for you

What the GitHub RC proves:
- artifact build works
- bundle packaging works
- the bundled binary starts on all three runner OSes
- the release page keeps installers as the supported user path instead of suggesting direct bundle execution
- the release body keeps the fixed user-facing note structure instead of falling back to a flat commit dump
- release metadata stays version-synced; if `version_tag` is provided, its base version must match `version.json`

What the GitHub RC does not prove:
- the public installer path
- real update/uninstall against published release assets
- manual client setup on real machines

When `publish_release=true`, the RC workflow becomes the bridge to the public installer path by publishing the bundle assets as a prerelease.
It still does not auto-run the real public installer smoke; that final check remains manual on real machines by design.

### 2. Local Artifact Check

Optional local parity check:

Requires local `goreleaser` on `PATH`.
If not available, use the GitHub RC workflow instead.

```bash
npm run release:rc:local
```

### 3. Fresh Install Smoke Matrix

- macOS clean HOME:
  - `install.sh`
  - complete the installer-started setup wizard
  - `ha-nova version`
  - `ha-nova doctor`
  - `ha-nova relay version`
  - `ha-nova update --version <same-version>`
  - `ha-nova uninstall --yes`
- Windows clean VM / snapshot:
  - `install.ps1`
  - complete the installer-started setup wizard
  - `ha-nova version`
  - `ha-nova doctor`
  - `ha-nova relay version`
  - `ha-nova update --version <same-version>`
  - `ha-nova uninstall --yes`
  - then poll `Test-Path $HOME\.local\share\ha-nova` for a few seconds until it flips to `False`; Windows uninstall finalizes via a short-lived helper
- Linux:
  - run the same flow only if Secret Service is available
  - if not live-tested, do not call the release fully verified on Linux

GitHub RC smoke covers the built bundles directly.
The tagged `Release` workflow later covers the public installer path plus `check-update`, same-version `update`, and `uninstall` against published assets.
This manual matrix exists to cover real machines before public publish.

### Public RC Installer Test

After publishing an RC prerelease like `v0.1.13-rc1`, test the real installer path with a fresh `HOME`.
Do this from the RC branch/ref that published the prerelease, not from `main`.
Reason: `main` must not point at a new installer contract before the matching public release exists.

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/<rc-branch>/install.sh | HA_NOVA_VERSION=v0.1.13-rc1 bash
```

Windows:

```powershell
$env:HA_NOVA_VERSION = 'v0.1.13-rc1'
irm https://raw.githubusercontent.com/markusleben/ha-nova/<rc-branch>/install.ps1 | iex
```

This is the first public-path check that proves the one-liner can actually fetch the published bundle assets.

### Private RC Installer Test

Safest path before any public release:
- do not merge to `main`
- do not create a public prerelease
- build local/private bundles and point the installers at them explicitly

Example with a local bundle server from the repo checkout:

```bash
npm run dev:validation:harness
```

macOS / Linux:

```bash
HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1 \
HA_NOVA_BUNDLE_URL=http://127.0.0.1:8917/<bundle-name>.tar.gz \
HA_NOVA_BUNDLE_SHA256_URL=http://127.0.0.1:8917/<bundle-name>.tar.gz.sha256 \
HA_NOVA_NO_SETUP=1 \
bash ./install.sh
```

Use the matching bundle name for the machine under test:
- Intel macOS: `ha-nova-installer-bundle-macos-amd64`
- Apple Silicon macOS: `ha-nova-installer-bundle-macos-arm64`
- Linux amd64: `ha-nova-installer-bundle-linux-amd64`
- Linux arm64: `ha-nova-installer-bundle-linux-arm64`

Windows:

```powershell
$env:HA_NOVA_CLAUDE_MARKETPLACE_LOCAL = '1'
$env:HA_NOVA_BUNDLE_URL = 'http://<host>:8917/ha-nova-installer-bundle-windows-amd64.zip'
$env:HA_NOVA_BUNDLE_SHA256_URL = 'http://<host>:8917/ha-nova-installer-bundle-windows-amd64.zip.sha256'
$env:HA_NOVA_NO_SETUP = '1'
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

For override-based tests, installer version comes from `bundle.json`. `HA_NOVA_VERSION` is optional and acts only as an extra expected-version assertion.

### Desktop Validation Helpers

Use the repo-local helpers for repeatable private-RC validation.
All of them assume the same precondition:

```bash
npm run verify
npm run release:rc:local
```

Do not run them against `main` or a public release.

macOS:
- `npm run test:desktop:macos`
- `scripts/dev/macos-private-rc-suite.sh`
- `scripts/dev/macos-private-rc-smoke.sh`
- `scripts/dev/macos-private-rc-setup-all.sh`
- `scripts/dev/macos-private-rc-client.sh <claude|codex|opencode|gemini>`
- `scripts/dev/start-local-validation-harness.sh`
- `scripts/dev/mock-ha-relay.py`

`scripts/dev/mock-ha-relay.py` only fakes Home Assistant plus the relay `/health` endpoint.
Its reported version is the private test line from `version.json`, not the real Home Assistant App version from `nova/config.yaml`.

For manual real-machine validation, prefer:

```bash
npm run dev:validation:harness
```

That helper rebuilds fresh local bundles by default, serves the repo root on `:8917` so both `install.ps1` and `dist/install-bundles/*` are reachable, prints copy/paste install commands for macOS and Windows, and can also start the tiny HA + fake relay `/health` mock with `--with-mock`.
The printed local commands also set `HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1` so Claude validation uses the freshly built local payload instead of the GitHub marketplace source.
Do not start an extra manual `http.server` on `:8917` next to these helpers; they either start their own server or the harness does it for you.

Windows:
- `scripts/dev/windows-clean-test-state.ps1`
- `npm run test:desktop:windows:headless`
- `npm run test:desktop:windows:rdp`
- `scripts/dev/windows-private-rc-install.ps1`
- `scripts/dev/windows-desktop-setup.ps1 -Client <client> -BundleUrl <url> -BundleSha256Url <url>`

Use `windows-private-rc-install.ps1` only for the headless installer lane.
Use `windows-desktop-setup.ps1` only inside an interactive RDP desktop session.
Both npm wrappers require `HA_NOVA_BUNDLE_URL` and `HA_NOVA_BUNDLE_SHA256_URL`.
`test:desktop:windows:rdp` also accepts `HA_NOVA_CLIENT`, defaulting to `claude`.
`test:desktop:macos` rebuilds fresh private RC bundles and serves them locally before running the helper lanes.
The desktop runner now treats failed `setup`, failed `doctor`, failed same-version `update`, missing client artifacts, and incomplete uninstall cleanup as hard failures.
Default `npm run verify` intentionally does not execute any of these desktop lanes.
The helper runners isolate token storage with `HA_NOVA_TEST_KEYRING_FILE` on macOS and `HA_NOVA_KEYRING_SERVICE` on Windows.
The file override is intentionally guarded behind `HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1` so a leaked path env var alone cannot silently downgrade real secure storage.
Older `dev:legacy:onboarding:macos*` npm scripts remain only for historical shell debugging and are not part of the safe validation lane.

Emergency macOS cleanup if a desktop helper was interrupted:

```bash
pkill -f 'npm run dev:validation:harness|start-local-validation-harness\\.sh|http\\.server 8917|vitest|macos-setup\\.sh|mock-ha-relay\\.py|ha-nova setup' || true
```

### 4. Recovery Matrix

- current Go install:
  - install -> update -> doctor -> uninstall
- legacy-only install:
  - installer must abort with legacy cleanup guidance
  - `legacy-uninstall.sh` / `legacy-uninstall.ps1`
  - fresh reinstall succeeds afterward
- mixed machine:
  - `legacy-uninstall.*` must not remove a valid current Go install
  - `ha-nova uninstall` must remove only the current Go install

### 5. Client Matrix

Smoke the matrix you actually intend to claim for the target platform.

Baseline:
- macOS: `codex`, `claude`, `opencode`, `gemini`
- Windows: `claude`, `gemini`, plus any extra client lane you explicitly want to claim

Release notes must keep platform support and client-lane validation separate:
- Windows platform support: installer + Go runtime + bundle packaging
- Windows validated client lane for this release: only what you actually smoke-tested on Windows
- Windows ARM64 caveat: `amd64` bundle only; use x64 emulation

Per client:
- verify the wizard-installed skill/plugin presence
- run one minimal real read-only command
- verify uninstall cleanup

If the install smoke intentionally skipped client setup or you are repairing a lane manually, use `ha-nova setup <client>` as the explicit recovery path.

### 6. Docs Gate

Check:
- `README.md`
- `PROJECT.md`
- `.codex/INSTALL.md`
- `.claude/INSTALL.md`
- `.gemini/INSTALL.md`
- `.opencode/INSTALL.md`
- `CONTRIBUTING.md`

Must not contain active instructions for:
- `npm run onboarding:macos`
- `~/.config/ha-nova/relay`
- `~/.config/ha-nova/update`

### 7. PR / Release Notes

For installer/runtime/platform releases, call out all of these explicitly:
- Windows platform support is now live through `install.ps1` + the Go runtime
- Windows currently ships an `amd64` bundle only; Windows ARM64 uses x64 emulation
- Current Windows client validation matrix for this exact release
- Default `npm run verify` is intentionally host-safe; desktop/private-RC validation stays separate
- Any client lanes that remain experimental on Windows
- Existing installs update through `ha-nova check-update` / `ha-nova update`; only Claude currently has the extra automatic SessionStart update banner
- Tell users not to download and run the release `ha-nova-installer-bundle-*.tar.gz` / `.zip` assets directly; those are installer payloads, not the supported end-user path

## Final Publish

Only after RC is green:

Important:
- do not merge installer/runtime changes to `main` before the matching public release exists
- otherwise the public raw `install.sh` / `install.ps1` on `main` can outrun the latest published release assets

Maintainer-only step:
- only maintainers with permission to create protected `v*` tags should run this
- if `required reviewers` is configured, final publish pauses at the `production` environment until a reviewer approves it
- approving `production` is the explicit checkpoint to confirm the latest RC passed; the workflow does not auto-check RC status for you

```bash
git add version.json package.json package-lock.json .claude-plugin/plugin.json .claude-plugin/marketplace.json
git commit -m "chore: bump version to X.Y.Z"
git tag -a vX.Y.Z -m "vX.Y.Z"
git push && git push --tags
```

The tagged `Release` workflow rebuilds fresh artifacts and publishes them.
Its installer smoke is post-publish confirmation, not the pre-publish gate.
It also hard-fails if the pushed tag does not match the checked-in release metadata.

## Relay Version Bump (independent from skill version)

Relay version lives in `nova/config.yaml` (`version:` field). Update manually:
```bash
# Edit nova/config.yaml version field
git add nova/config.yaml
git commit -m "chore: bump relay version to X.Y.Z"
```

Relay is rebuilt via Docker on the HA host — no npm publish. Users update by pulling the new image or rebuilding the app.

## Post-Release

- `git tag -l 'v*'` — verify tag exists
- All clients: users run `ha-nova update` (auto-detects installed clients)
- Claude Code users refresh via `ha-nova update` (which re-registers the local marketplace entry; private validation can still force the explicit local override)
- Claude SessionStart will show `UPDATE AVAILABLE` to users still on the old version
- Other clients use the same shared updater path, but do not currently inject an equivalent startup banner automatically
- Legacy pre-Go installs are not updated in place; they must run the dedicated legacy cleanup script first, then reinstall with `install.sh` / `install.ps1`
