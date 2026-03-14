# Release Checklist

## Version Bump

```bash
npm run bump -- 0.2.0
```

This updates all 4 version files atomically:
- `version.json` (source of truth)
- `package.json`
- `.claude-plugin/plugin.json`
- `.claude-plugin/marketplace.json`

To also bump `min_relay_version` (when skills require relay endpoints not present in older relay versions), edit `version.json` manually after the bump.

## Verify

```bash
npm run verify
```

This covers TypeScript, Vitest, and Go CLI verification.

## Release Candidate Gate

Before creating a public release, run an RC pass.

GitHub automation:
- `ci.yml` = normal PR / main quality gate
- `release-candidate.yml` = manual RC build + bundle smoke, with optional prerelease bundle publish
- `release.yml` = final tagged publish

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
- the RC publish job accepts only commits on `main`

What the GitHub RC proves:
- artifact build works
- bundle packaging works
- the bundled binary starts on all three runner OSes

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
  - `ha-nova version`
  - `ha-nova setup <client>`
  - `ha-nova doctor`
  - `ha-nova relay version`
  - `ha-nova update --version <same-version>`
  - `ha-nova uninstall --yes`
- Windows clean VM / snapshot:
  - `install.ps1`
  - `ha-nova version`
  - `ha-nova setup <client>`
  - `ha-nova doctor`
  - `ha-nova relay version`
  - `ha-nova update --version <same-version>`
  - `ha-nova uninstall --yes`
- Linux:
  - run the same flow only if Secret Service is available
  - if not live-tested, do not call the release fully verified on Linux

GitHub RC smoke covers the built bundles directly.
The tagged `Release` workflow later covers the public installer path plus `check-update`, same-version `update`, and `uninstall` against published assets.
This manual matrix exists to cover real machines before public publish.

### Public RC Installer Test

After publishing an RC prerelease like `v0.1.13-rc1`, test the real installer path with a fresh `HOME`.

macOS / Linux:

```bash
HA_NOVA_VERSION=v0.1.13-rc1 curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

Windows:

```powershell
$env:HA_NOVA_VERSION = 'v0.1.13-rc1'
irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex
```

This is the first public-path check that proves the one-liner can actually fetch the published bundle assets.

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

Smoke at least once each for:
- `codex`
- `claude`
- `opencode`
- `gemini`

Per client:
- `ha-nova setup <client>`
- verify installed skill/plugin presence
- run one minimal real read-only command
- verify uninstall cleanup

### 6. Docs Gate

Check:
- `README.md`
- `.codex/INSTALL.md`
- `.claude/INSTALL.md`
- `.gemini/INSTALL.md`
- `.opencode/INSTALL.md`
- `CONTRIBUTING.md`

Must not contain active instructions for:
- `npm run onboarding:macos`
- `~/.config/ha-nova/relay`
- `~/.config/ha-nova/update`

## Final Publish

Only after RC is green:

Maintainer-only step:
- only maintainers with permission to create protected `v*` tags should run this
- if `required reviewers` is configured, final publish pauses at the `production` environment until a reviewer approves it
- approving `production` is the explicit checkpoint to confirm the latest RC passed; the workflow does not auto-check RC status for you

```bash
git add version.json package.json .claude-plugin/plugin.json .claude-plugin/marketplace.json
git commit -m "chore: bump version to X.Y.Z"
git tag -a vX.Y.Z -m "vX.Y.Z"
git push && git push --tags
```

The tagged `Release` workflow rebuilds fresh artifacts and publishes them.
Its installer smoke is post-publish confirmation, not the pre-publish gate.

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
- Claude Code users refresh via `ha-nova update` (which re-registers the local marketplace entry if needed)
- SessionStart hook will show `UPDATE AVAILABLE` to users still on the old version
- Legacy pre-Go installs are not updated in place; they must run the dedicated legacy cleanup script first, then reinstall with `install.sh` / `install.ps1`
