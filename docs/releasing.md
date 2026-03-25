# Release Checklist

## Version Bump

```bash
npm run bump -- <version>
```

For the current train:

```bash
npm run bump -- 0.3.1
```

This updates all 5 version files atomically:
- `version.json` (source of truth)
- `package.json`
- `package-lock.json`
- `.claude-plugin/plugin.json`
- `.claude-plugin/marketplace.json`

To also bump `min_relay_version` (when skills require relay endpoints not present in older relay versions), edit `version.json` manually after the bump.

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
It covers release metadata sync, TypeScript, the safe Vitest suite, build/docs validation, and Go CLI verification.
The deterministic `#87` bulk contract is machine-checked here through the tracked `test:safe` suite on every RC/final workflow SHA.
It must not open browsers or touch real secure stores on the maintainer host.

Hard rule:
- final tag version, `version.json`, `package.json`, `package-lock.json`, `.claude-plugin/plugin.json`, and `.claude-plugin/marketplace.json` must match
- the Claude marketplace entry must keep `source: "./"` so installed bundles stay on the local plugin update path instead of drifting to a remote repo source
- Claude marketplace source parity must be regression-covered for all three shapes: plain GitHub URL, structured GitHub repo source, and structured GitHub repo source with pinned `ref`; a pinned `ref` must never compare equal to the floating default source

## Bulk Release Preflight

`npm run verify` stays the canonical automated repo gate.
It does not talk to a real Home Assistant instance or a live Codex/relay session.

For the `#87` bulk release train, maintainers must also run the local live bulk sign-off commands on the exact SHA intended for RC or final tag:

```bash
npm run test:bulk:release
npm run test:bulk:manual:area-review
```

Rules:
- `test:bulk:release` is the maintainer-host bulk gate: the deterministic bulk contract is already covered by `npm run verify` through `test:safe`; this command adds live stable inventory smoke, then reruns the full safe suite on the same host
- `test:bulk:manual:area-review` is the explicit aggregate bulk-review proof; keep the resulting transcript or summary artifact with the release notes / sign-off record
- do not move these live bulk checks into GitHub runner CI; they depend on a real local HA + relay + Codex environment
- the live bulk commands require `ha-nova` and `codex` on `PATH` plus a Python 3 runtime; the npm wrapper resolves `python3`, `python`, or Windows `py -3`, and the harness immediately verifies `ha-nova relay health` before starting
- if any relevant delta lands after the last successful bulk sign-off, rerun both commands on the new SHA
- `npm run verify` and the bulk preflight must agree on the same reviewed commit state before RC or tag work continues

## Release Preflight

Before every RC or final tag:

- audit open PRs first, especially Dependabot PRs plus anything touching workflows, GoReleaser, installer/update paths, or release metadata
- classify each relevant open PR as `release blocker now` vs `separate later`
- do not pull a red or unreviewed workflow/release PR into the release train at the last minute
- for the release PR itself, wait for the actual Codex bot result; `codex-review-gate` timeout alone is not enough
- review clearance is tied to the exact commit state that will be tagged, not to the branch/topic in general
- a clean bot result applies only to the exact commit SHA it reviewed; any later SHA is unreviewed until the cycle completes again
- if any relevant delta appears after the last bot-reviewed commit, stop release work and restart the full cycle: push -> `@codex` -> actual bot result -> checks green

Fast-path rule during iteration:
- do not wait for a full manual review pass before asking Codex
- for the initial PR SHA and after each relevant fix, run only targeted local verification, push immediately if needed, and immediately trigger `@codex`
- after the PR exists, do not add extra local review gates in between; Codex bot + CI are the review path
- only the final merge/tag-ready SHA may be treated as cleared when required checks are green and the current Codex bot result on that exact SHA is clean
- if Codex times out or never posts a real result on the current SHA, re-request `@codex` on that same SHA before treating the PR as review-clean

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
- Keep the headings fixed across releases.
- If a section does not apply, omit that section instead of inventing filler.
- Keep notes short and concrete.
- Prefer user-visible outcomes over implementation detail.
- Do not list every small fix.
- Only include bug fixes that are severe, user-facing, or likely to affect trust/support load.
- Only include `What To Watch` when there is a real behavior change, migration step, required action, compatibility note, or breaking change.
- If there is nothing users need to do, omit `What To Watch` entirely.
- If there are breaking changes, put them under `What To Watch` in plain language first. Technical detail can follow in one short bullet if needed.
- Call out client-specific behavior only when it matters to users. Example: only Claude currently has the extra automatic SessionStart update banner.
- For onboarding/lifecycle releases, explicitly call out uninstall mode changes, Windows path migrations, and whether `winget` is publicly available yet or only internal groundwork.
- Keep `.goreleaser.yml` and RC notes in `release-candidate.yml` aligned on the same lifecycle truth; do not let final and prerelease notes drift.

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
- `./scripts/release/build-winget-manifest.sh`
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
- the `winget` handoff manifest is regenerated from the same tagged Windows bundle payload

What the GitHub RC does not prove:
- the public installer path
- real update/uninstall against published release assets
- manual client setup on real machines

Important gate rule:
- RC is the pre-publish gate
- `release.yml` smoke runs after publish and is only a safety net, not the release approval step

When `publish_release=true`, the RC workflow becomes the bridge to the public installer path by publishing the bundle assets as a prerelease.
It still does not auto-run the real public installer smoke; that final check remains manual on real machines by design.

### 2. Local Artifact Check

Optional local parity check:

Requires local `goreleaser` on `PATH`.
If not available, use the GitHub RC workflow instead.

```bash
npm run release:rc:local
```

## Public Winget Handoff

The generated `ha-nova-winget-manifest-<tag>.zip` is the handoff artifact for `microsoft/winget-pkgs`.
For the real public submission, stage it from the exact final stable GitHub release asset.
Local `dist/` output or RC artifact downloads are rehearsal-only.
`npm run release:winget:stage-submission` now defaults to `WINGET_STAGE_SOURCE=release_asset`.
Only use `WINGET_STAGE_SOURCE=local_dist` for private rehearsal or contract validation.

Stage the real public submission payload from the exact final stable release artifact:

```bash
npm run release:winget:stage-submission
```

Only use the explicit script form when you need to target a specific final stable tag:

```bash
bash scripts/release/prepare-winget-pkgs-submission.sh 0.3.0 markusleben/ha-nova v0.3.0
```

The helper refuses prerelease tags in `release_asset` mode.

That helper:
- unpacks the exact generated manifest ZIP into `dist/winget/submission/...`
- verifies the staged `InstallerUrl` points at the published GitHub release bundle, not a local harness override
- verifies the manifest `InstallerSha256` matches the actual Windows bundle bytes and any local `.sha256` sidecar
- writes a staged maintainer checklist, PR body, and upstream copy path next to the submission payload
- writes a bash + PowerShell PR command guide that stays usable even when Windows validation and PR creation happen on different hosts
- prints the exact next steps for warning-free `winget validate`, `winget-pkgs` PR creation, and the final fresh-VM public-source smoke

Generated helper artifacts:
- `dist/winget/submission/<package>/<version>/winget-pkgs-maintainer-checklist.md`
- `dist/winget/submission/<package>/<version>/winget-pkgs-pr-body.md`
- `dist/winget/submission/<package>/<version>/winget-pkgs-copy-path.txt`
- `dist/winget/submission/<package>/<version>/winget-pkgs-gh-commands.md`

Use them as the source of truth for the actual maintainer submission step instead of reconstructing the PR by hand.
The commands file should be treated as the source of truth for the real fork/branch/commit/push/PR step after Windows validation succeeds, with separate bash and PowerShell variants plus an explicit staged-root placeholder for cross-host handoffs.

Track the public package lane in `release/winget-publication-state.json`:
- set `publication_phase = "pr_open"` and `pending_version = "<tag version>"` when the `winget-pkgs` PR opens
- move `publication_phase` to `merged_waiting_visibility` after merge
- move `publication_phase` to `visible_waiting_install_proof` once `winget show --source winget` sees the version
- set `public_install_proven = true`, `publication_phase = "install_proven_waiting_upgrade_proof"`, and `current_public_version` after the fresh-VM install/check-update/uninstall proof passes
- set `public_upgrade_proven = true`, `publication_phase = "upgrade_proven"`, and keep `current_public_version` on the latest proved public version only after a later published-to-published `winget upgrade` proof passes
- keep `automation_enabled = false` until both proofs are true and the team explicitly enables automated update PRs
- never open a second public `winget` submission while `pending_version` is non-empty and `publication_phase` is not yet terminal

Required sequence before any public doc flip:
1. stage the manifest payload from the release ZIP
2. run `winget validate --manifest <dir>` on Windows and require a warning-free success result
3. open and merge the `winget-pkgs` PR
4. wait until `winget show --id markusleben.ha-nova --exact --source winget` shows the expected published version
5. run the initial fresh-VM published-source install/check-update/uninstall proof
6. only then switch public docs and release-note wording to `winget` as the primary Windows path

Do not switch public Windows install docs to `winget install` until the exact staged manifest has been submitted, merged, and proven on a fresh Windows machine with:
- `winget install --id markusleben.ha-nova --exact`
- `winget install --id markusleben.ha-nova --exact --source winget`
- `ha-nova check-update`
- `winget uninstall --id markusleben.ha-nova --exact --source winget`

Treat public `winget upgrade` as a second proof lane:
- validate it from a separate Windows snapshot that already has the previous published `markusleben.ha-nova` version installed
- if this is the first public `winget` publication and no older public version exists yet, record upgrade continuity as pending and re-run it on the next published `winget` release
- keep release-note/doc wording conservative about public `winget upgrade` until that continuity proof exists
- use `winget upgrade --id markusleben.ha-nova --exact --source winget` for that proof so the command cannot drift to another configured source

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
  - confirm this standard remove keeps local config/token state by design
  - confirm the CLI says Windows uninstall continues in the background and that it is safe to close the terminal
  - then poll `Test-Path "$env:LOCALAPPDATA\\Programs\\ha-nova"` for a few seconds until it flips to `False`; Windows uninstall finalizes via a short-lived helper
  - if you force a helper failure during validation, confirm `ha-nova doctor` blocks with the exact recovery command and rerunning that command clears the marker
  - on a separate fresh snapshot, also verify `ha-nova uninstall --yes --purge`
  - after purge, confirm `%APPDATA%\\ha-nova` and `%LOCALAPPDATA%\\ha-nova\\cache` are gone
- Linux:
  - run the same flow only if Secret Service is available
  - if not live-tested, do not call the release fully verified on Linux

GitHub RC smoke covers the built bundles directly.
The tagged `Release` workflow later covers the public installer path plus `check-update`, same-version `update`, warning-free Windows `winget validate`, and `uninstall` against published assets.
For Windows, treat that final workflow as complete only after it polls until `%LOCALAPPDATA%\\Programs\\ha-nova` is gone and `%LOCALAPPDATA%\\ha-nova\\uninstall-status.json` is gone; the detached helper can finish a few seconds after the foreground command returns.
This manual matrix exists to cover real machines before public publish.

Current Windows release reality:
- public Windows installer smoke is still `install.ps1`-based until the `winget` manifest is published and proven on a fresh Windows VM
- every tag now also builds a `ha-nova-winget-manifest-<tag>.zip` handoff artifact from the Windows installer bundle
- upload that manifest asset with the release and treat it as the source of truth for any later `winget-pkgs` submission
- do not advertise `winget install` / `winget upgrade` in release notes before publication plus fresh-VM proof exists
- if you are validating a private or future `winget` lane, treat it as an extra explicit source-aware test path, not as the current public contract
- if mixed bundle + `winget` installs are detected, verify that `ha-nova check-update` warns and `ha-nova update` fails loud instead of guessing the target channel
- current GitHub RC/final smoke still proves only the bundle lane; source-aware `winget` / mixed-channel behavior is a private Windows RC blocker until the public package is published and proven

Published-source proof rules:
- use a fresh Windows VM
- do not use the local harness
- do not use a local manifest path
- do not set `HA_NOVA_BUNDLE_URL`, `HA_NOVA_BUNDLE_SHA256_URL`, or `HA_NOVA_VERSION`
- do not enable or rely on `LocalManifestFiles` for this proof
- prove package visibility first with `winget show --id markusleben.ha-nova --exact --source winget`
- for the initial public-launch proof, run `winget install`, then `ha-nova check-update`, then `winget uninstall`
- for upgrade continuity, use a separate snapshot with the previous published version already installed before running `winget upgrade`

### Public RC Installer Test

After publishing an RC prerelease like `v0.1.13-rc1`, test the real installer path with a fresh `HOME`.
Do this from the exact commit SHA or immutable tag that published the prerelease, not from a moving branch ref and not from `main`.
Reason: a moving branch can fetch newer installer bootstrap code against older RC assets and produce a false green prerelease signal.

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/<rc-commit-or-tag>/install.sh | HA_NOVA_VERSION=v0.1.13-rc1 bash
```

Windows:

```powershell
$env:HA_NOVA_VERSION = 'v0.1.13-rc1'
$ProgressPreference = 'SilentlyContinue'
irm https://raw.githubusercontent.com/markusleben/ha-nova/<rc-commit-or-tag>/install.ps1 | iex
```

This is the first public-path check that proves the one-liner can actually fetch the published bundle assets.

### Private RC Installer Test

Safest path before any public release:
- do not merge to `main`
- do not create a public prerelease
- build local/private bundles and the matching local `winget` handoff manifest, then point the installers at the bundle explicitly

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
$ProgressPreference = 'SilentlyContinue'
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

For override-based tests, installer version comes from `bundle.json`. `HA_NOVA_VERSION` is optional and acts only as an extra expected-version assertion.

### Windows Private RC Checklist

Run this before any Windows installer/update release is called ready:

1. Reset the VM or snapshot to a clean state.
   Start with `scripts/dev/windows-clean-test-state.ps1`.
2. Run the bundle lane on a clean machine.
   Use `install.ps1`, complete setup, run `ha-nova version`, `ha-nova doctor`, `ha-nova check-update`, and `ha-nova update --version <same-version>`.
3. Prove clean update behavior with an older bundle.
   Install an older private/public RC bundle first, then update to the current candidate and verify client sync completes afterward.
4. Verify uninstall semantics.
   Run `ha-nova uninstall --yes`, confirm config/token retention by design, confirm the CLI says background uninstall is safe to close, then wait until `%LOCALAPPDATA%\\Programs\\ha-nova` plus `%LOCALAPPDATA%\\ha-nova\\uninstall-status.json` are both gone. On a separate snapshot run `ha-nova uninstall --yes --purge` and confirm `%APPDATA%\\ha-nova` plus `%LOCALAPPDATA%\\ha-nova\\cache` are gone.
   Also force one helper-failure path during validation and confirm `ha-nova doctor` blocks with the exact recovery command until rerunning that command clears the marker.
5. Rehearse the future `winget` lane deliberately, if available.
   Verify `install.ps1` refuses to create a second Windows channel on top of an existing `winget` install, and verify mixed-channel states warn on `check-update` and fail loud on `update`.
6. Rehearse both mixed-channel directions if available.
   Test bundle-active-plus-winget-present and winget-active-plus-bundle-present. For both, verify `check-update`, `update`, `uninstall --yes`, `uninstall --yes --purge`, and direct `winget uninstall` never guess silently which channel to mutate.
7. Run the Windows Claude cache regression explicitly.
   Seed both known stale Claude cache layouts, run install or update with `HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1`, then prove Claude is using the freshly staged payload rather than a cached older plugin copy.

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

That helper rebuilds fresh local bundles by default, rebuilds the local Windows manifest ZIP so it points at the live local bundle URL, serves the repo root on `:8917` so both `install.ps1`, `dist/install-bundles/*`, and `dist/winget/*.zip` are reachable, prints copy/paste install commands for macOS plus both Windows paths (`install.ps1` and local `winget --manifest`), and can also start the tiny HA + fake relay `/health` mock with `--with-mock`.
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
The removed legacy macOS shell onboarding scripts are no longer part of the safe validation lane.
For Windows release sign-off, treat the checklist above as the truth: the npm helper lanes are convenience wrappers, not the release decision by themselves.

Emergency macOS cleanup if a desktop helper was interrupted:

```bash
pkill -f 'npm run dev:validation:harness|start-local-validation-harness\\.sh|http\\.server 8917|vitest|mock-ha-relay\\.py|ha-nova setup' || true
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
  - `ha-nova uninstall` must fail loud until the channel conflict is resolved; it must not guess which current install to mutate
- release assets:
  - `install.sh` / `install.ps1` stay the user-facing entrypoints until the package is live
  - `ha-nova-winget-manifest-<tag>.zip` must point at the published Windows bundle asset, never at `install.ps1`

### 5. Client Matrix

Smoke the matrix you actually intend to claim for the target platform.

Baseline:
- macOS: `codex`, `claude`, `opencode`, `gemini`
- Windows: `claude`, `gemini`, plus any extra client lane you explicitly want to claim
- Linux: keep wording conservative unless a real Secret Service-backed machine was live-tested; CI smoke alone does not upgrade Linux to full real-machine validation

Adapter families to cover before release messaging claims cross-client update confidence:
- Claude / Claude Desktop Code tab: `plugin_marketplace`
- Codex and OpenCode: `skill_tree`
- Gemini: `skill_flat`

Release notes must keep platform support and client-lane validation separate:
- Windows platform support: installer + Go runtime + bundle packaging
- Windows validated client lane for this release: only what you actually smoke-tested on Windows
- Windows ARM64 caveat: `amd64` bundle only; use x64 emulation

Per client:
- verify the wizard-installed skill/plugin presence
- run one minimal real read-only command
- verify uninstall cleanup
- for Claude on Windows, also verify the refreshed marketplace/plugin payload is the new one after install/update so stale cache layouts cannot silently win

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

Also check:
- `docs/reference/skill-architecture.md`

That active reference doc must not imply the removed macOS shell onboarding family still exists as current product truth.

### 7. PR / Release Notes

For installer/runtime/platform releases, call out all of these explicitly:
- Windows platform support is now live through `install.ps1` + the Go runtime
- Windows currently ships an `amd64` bundle only; Windows ARM64 uses x64 emulation
- Default `npm run verify` is intentionally host-safe; desktop/private-RC validation stays separate
- Only describe Windows validation scope that was actually proven for this exact release; do not imply broader client parity than was tested
- Existing installs update through `ha-nova check-update` / `ha-nova update`; only Claude currently has the extra automatic SessionStart update banner
- Tell users not to download and run the release `ha-nova-installer-bundle-*.tar.gz` / `.zip` assets directly; those are installer payloads, not the supported end-user path
- If uninstall semantics changed, say plainly whether default `ha-nova uninstall` is standard remove or full purge
- If Windows paths changed, say plainly that `%APPDATA%\\ha-nova` / `%LOCALAPPDATA%\\ha-nova` are now canonical and legacy `~/.local` / `~/.config` data is migrated or cleaned up
- If the release ships only the generated `winget` manifest artifact but not a live `winget` community publication yet, say that clearly and keep `install.ps1` as the documented Windows install path
- Attach the generated `winget` manifest artifact to RC/final releases and submit that exact bundle to the public `winget-pkgs` flow before documenting `winget install` for users
- If mixed Windows install channels are now detected, say that HA NOVA warns/fails loud until users keep only one channel
- If `install.ps1` now refuses to install on top of an existing `winget` install, say plainly that one Windows install channel per machine is supported

Before tag/release:
- audit open PRs, especially Dependabot and workflow/release PRs, as `blocker now` vs `separate later`
- do not pull in red or unreviewed workflow/release changes at the last minute
- final release SHA must complete the current review cycle, including the current Codex bot hygiene for release-bound changes

## Final Publish

Only after RC is green:

Important:
- do not merge installer/runtime changes to `main` before the matching public release exists
- otherwise the public raw `install.sh` / `install.ps1` on `main` can outrun the latest published release assets

Maintainer-only step:
- only maintainers with permission to create protected `v*` tags should run this
- if `required reviewers` is configured, final publish pauses at the `production` environment until a reviewer approves it
- approving `production` is the explicit checkpoint to confirm the latest RC passed; the workflow does not auto-check RC status for you
- do not treat post-publish `release.yml` smoke as the reason to publish; it is confirmation after the fact

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
- Bundle/dev installs: users run `ha-nova update` (auto-detects installed clients)
- Do not switch public Windows docs to `winget` until the actual manifest/release publication is live and the initial fresh-VM published-source proof has passed
- Keep the release-uploaded `ha-nova-winget-manifest-<tag>.zip` in sync with the tagged Windows bundle before opening any `winget-pkgs` submission
- Claude Code users refresh via `ha-nova update` (which re-registers the local marketplace entry; private validation can still force the explicit local override)
- Claude SessionStart will show `UPDATE AVAILABLE` to users still on the old version
- Other clients use the same shared updater path, but do not currently inject an equivalent startup banner automatically
- Legacy pre-Go installs are not updated in place; they must run the dedicated legacy cleanup script first, then reinstall with `install.sh` / `install.ps1`
