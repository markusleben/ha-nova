# Release Runbook

Manual proofs (desktop lanes, Windows/Linux onboarding, private RC helpers)
live in `docs/release-validation.md`.

## 1. Bump and Release-Prep PR

```bash
npm run bump -- <version>
npm run verify:next-release-version -- v<version>
```

- never reuse a published stable version; never tag an RC/final whose base
  version is <= the latest published stable
- tag version, `version.json`, `package.json`, `package-lock.json`,
  `.claude-plugin/plugin.json`, and `.claude-plugin/marketplace.json` must
  match; the marketplace entry keeps `source: "./"`
- `README.md` is stable-release truth: unreleased claims collect in the
  version-free `docs/work/next-release-body.md`; the release-prep PR carries
  the bump, every release-bound `README.md` edit, and the `.goreleaser.yml`
  notes, then moves the consumed draft to `docs/archive/work/`
- docs parity sweep before the release-prep PR: skill count and list in
  `README.md` match `skills/` (`skills/ha-nova/SKILL.md` dispatch table is
  authoritative); shipped capabilities are never described as missing
- site sync: https://hanova.app (`markusleben/ha-nova-site`) mirrors the
  README; every release-bound README claim gets its site copy in a prepared
  site PR (desktop + mobile screenshots, maintainer go), merged right after
  the FINAL stable tag publishes (never for an RC) — a release is not done
  until the site matches. The hosted demo's landing page (`demo/` there)
  stays unlinked and `noindex` until the release that ships the demo, so it
  may merge before a tag; that release's site PR links it and drops
  `noindex`. A brand change to
  `assets/icon.svg` also updates `scripts/demo/hosted/site/icon.svg` (the
  demo's restarting page; a test keeps them equal)
- merging the release-prep PR starts the tag sequence immediately
- preflight: audit open PRs (Dependabot, workflows, installer/update,
  release metadata) as `blocker now` vs `separate later`; never pull a red or
  unreviewed workflow/release PR in right before publish
- release worthiness: release when shipped behavior, the live release path,
  published artifacts, the installer/update flow, or a user-facing bug
  changed; batch docs/tests/process work otherwise

## 2. Verify

```bash
npm run verify
```

The host-safe default gate: release metadata sync, production `npm audit` on
both lockfiles (`bash scripts/release/verify-npm-audit.sh`), blocked files,
TypeScript, docs contracts, `test:safe:core` (pinned by
`scripts/test/safe-core-files.json`), the onboarding slice (starting with
`npm run verify:installers`), build, Go CLI tests, and release contracts.

## 3. Release Candidate Gate

The **tag-first dress rehearsal** runs the exact stable pipeline against a
`vX.Y.Z-rcN` prerelease, so pipeline breakage surfaces on the RC tag.

**RC required** when the release changes delivery machinery since the last
green `release.yml` run: `install.sh` / `install.ps1` / `scripts/release/`,
`.goreleaser.yml` beyond notes text, release workflows or the
`release-tags-protection` ruleset, Go code, or the onboarding/update flow the
installed artifacts execute. **RC skippable** for skills/docs-only releases.
When in doubt, run the RC.

Workflows: `ci.yml` = PR/main gate; `release-candidate.yml` = manual
build + 3-runner bundle smoke, never publishes; `release.yml` = tagged
publish for both `-rcN` and final tags. The `release-tags-protection` ruleset
blocks the Actions token from creating `v*` tags, so a maintainer pushes every
tag. `GORELEASER_CURRENT_TAG` pins GoReleaser to the pushed tag, so an RC and
the final tag may share a commit.

## 4. Rehearsal and Tag Steps

Keep one immutable `<reviewed-merge-sha>`; no local-only delta enters a
deploy or tag.

1. Merge the reviewed PR state and record its remote merge commit. If the
   Relay version changed, wait for the `relay-image.yml` push run on that SHA:
   ```bash
   bash scripts/release/verify-relay-image.sh <reviewed-merge-sha> <relay-version>
   ```
   and for the `app-image.yml` push run on that SHA (`app-image-verify` green;
   see App Images below).
2. Every release needs one green `e2e-disposable-ha.yml` run (dispatched, not the weekly cron) on the commit being tagged.
   Dispatch it and select the run whose `headSha` equals `<reviewed-merge-sha>`:
   ```bash
   gh workflow run e2e-disposable-ha.yml --ref main
   gh run list --workflow e2e-disposable-ha.yml --event workflow_dispatch --commit <reviewed-merge-sha>
   gh run watch <exact-e2e-run-id> --exit-status
   ```
   A local `bash scripts/e2e/disposable-ha/run.sh` pass is useful but does not replace the dispatched run.
3. Push the RC tag
   (`git tag vX.Y.Z-rcN <reviewed-merge-sha> && git push origin vX.Y.Z-rcN`).
4. Wait for `release.yml` to finish green. Verify the published RC over the real
   public install path (section 7), including one real Windows 11 +
   PowerShell onboarding proof, and run the Cloud checklist (section 6).
5. Then cut the final tag on the same commit
   (`git tag vX.Y.Z <reviewed-merge-sha> && git push origin vX.Y.Z`) and
   run the post-publish verification (section 7). A skipped RC still needs
   steps 1–2 of the rehearsal completed.

## 5. What `release.yml` Does

1. `verify-release-metadata`: tag matches every version file.
2. `build-signed-darwin-release-assets` (macOS, `production` environment):
   refuses tags not on `main`, builds and signs Darwin binaries with the
   Developer ID identity, verifies Team ID and hardened runtime.
3. `release` (`production` environment): tag-on-main check, `npm run verify`,
   next-version check, GoReleaser (Linux/Windows, draft, `prerelease: auto`),
   uploads signed Darwin binaries plus checksums, then builds the install
   bundles with `scripts/release/build-install-bundle.sh`, which signs each
   bundle's Cloud provenance via `sign-cloud-release-evidence.mjs` using
   `HA_NOVA_CLOUD_RELEASE_SIGNING_KEY_PEM`.
4. `smoke-release-bundles` (Linux, macOS, Windows): downloads the exact draft
   bundles; listed platforms must pass `internal-cloud-release-check`,
   unlisted ones must fail it; macOS re-verifies the signature and compares
   the bundled binary byte-for-byte with the raw asset.
5. `publish-release`: verifies the complete asset set, publishes the draft,
   re-asserts Latest for stable.
6. `smoke-installers`: public install/update/uninstall on all three OSes.

## App Images

Supervisor stores track `main`: once `nova/config.yaml` carries
`image: "ghcr.io/markusleben/{arch}-ha-nova-relay"`, every App version on main
MUST already have public images for every `nova/build.yaml` arch, or every
install/update fails. Spec: `docs/work/2026-10-15-app-prebuilt-images-spec.md`.

- `app-image.yml` publishes from the release-prep PR (same-repo, into main,
  `image:` set) while the version never reached main; versions on main are
  frozen. Recover a failed publish by re-running the PR's workflow run.
- `app-image-verify` gates the merge: anonymous existence check, plus a
  build-input fingerprint match on the change that introduces the version or
  `image:`. For a PR that changes the App version: `gh pr update-branch`,
  wait for green on the new head, only then merge — also before `--admin`.
  A red main-push run on the merge SHA means: ship the next patch version.
- New GHCR packages start private: flip them to Public once (first release).
- Manual check: `bash scripts/release/verify-app-image.sh <app-version>`.
- Regression matrix (App image path changed or first `image:` release):
  `bash scripts/e2e/supervisor-app/run.sh`, then
  `E2E_IMAGE_SOURCE=ghcr bash scripts/e2e/supervisor-app/run.sh` once public.
- Rollback: the next version without `image:` builds locally again; never
  retag or delete a published version.
- The chat-app bridge inside the image (`ha-nova serve` + skills) is staged
  from the same tree (`scripts/release/stage-app-bridge.sh`, version =
  `skill_version`), so an App bump comes with or after the final
  `skill_version`, and a later bridge fix needs an App patch version. An
  App patch alone does not ship a `nova/integration/` fix: only turning chat
  apps on copies the integration, and HA loads a new copy only after a
  restart. Build that update path before the first integration fix.

### Chat-app lanes

Throwaway HAOS VM only (never a real home), results in the App bump PR.
Supervisor reads `main`, so an App version is live for every user the moment
its PR merges: run these BEFORE the merge, on the PR's published image (VM
repository `https://github.com/markusleben/ha-nova#<app-bump-branch>`); a
failure found after the merge needs the next patch version. Required for the
first chat-app release and after a change to the bridge, `nova/integration/`,
`nova/src/runtime/chat-apps.ts`, or the discovery path:

- [ ] `on`: NOVA page "Turn on chat apps" → consent → one restart → the
      `ha_nova` entry is loaded; the connector answers `401` unauthenticated
      and MCP `initialize` for the owner, also over Link
- [ ] `off`: "Turn off chat apps" → discovery message deleted, the entry goes
      (matched by uuid), `custom_components/ha_nova` removed, the bridge's
      device record revoked; off and on again → one entry, new secret
- [ ] `upgrade_off`: update from the previous App version without turning
      chat apps on → no `custom_components/ha_nova`, no discovery message, no
      `ha_nova` entry, no restart; paired computers keep working
- [ ] `app_update`: update from the previous chat-app App version while on →
      same entry, connector still works without turning on again (n/a for
      the first chat-app release: the previous App has no chat apps)
- [ ] `rollback`: an App build with the chat code but no staged bridge (the
      local-build rollback path) while on offers only [Turn off], and that
      finishes the cleanup
- [ ] `registry_reset`: NOVA page registry reset while on → chat apps work
      again, at the latest after an App restart (the bridge re-mints its
      credential before it starts)
- [ ] `clients` (maintainer, Cloud checklist): claude.ai web + phone app and
      ChatGPT web connect over Link, an unlock asks the linked phone (or a
      chat code), and turning off breaks both connectors

## 6. Home Assistant Link (formerly Home Assistant Cloud)

`version.json` is the release switch: `cloud_remote_enabled` plus a
non-empty, duplicate-free subset of `darwin`, `linux`, `windows` in
`cloud_remote_platforms` (empty while disabled). Shrinking the list is a
user-facing feature removal and a maintainer decision.

Runtime guard: official builds use the `cloudremote_official` build tag and
enable Cloud only when `bundle.json` carries an Ed25519 signature over the
exact binary SHA-256, version, OS, architecture, binary name, platform list,
and release source-tree SHA. Ordinary `go build` output is compile-time
disabled. The public key is compiled in; the private key exists only as the
`production` environment secret `HA_NOVA_CLOUD_RELEASE_SIGNING_KEY_PEM`. Key
rotation is a reviewed source change: generate offline, commit only the public
half, install the private PEM, verify an RC on every platform, retire the old
key. While Cloud is enabled, `min_relay_version` equals the App version in
`nova/config.yaml`. Every such rise also outdates the hosted demo's relay (built from
the provisioned ref): re-provision it with the release's merge commit
(`docs/reference/hosted-demo.md`).

macOS signing secrets (`HA_NOVA_MACOS_CERTIFICATE_P12_BASE64`,
`HA_NOVA_MACOS_CERTIFICATE_PASSWORD`) live only in `production` and are
provisioned without touching the login Keychain:

```bash
bash scripts/release/provision-macos-signing-secrets.sh /absolute/path/developer-id.p12
```

**Per-release Cloud checklist** (every release with Cloud enabled; record
results in the release PR, no private Cloud URLs). Real-device rows repeat for
first support or after a change touching their path:

- [ ] `provenance`: `release.yml` bundle smokes green on every enabled OS
- [ ] `installed_relay_app`: the real App runs the prebuilt image for the
      `nova/config.yaml` version (Supervisor log: download, no build) — or,
      while `image:` is absent, Supervisor's local build of the release commit
      — and reports that version
- [ ] `health`: reference smoke below returns `ha_ws_connected: true` and the expected App version
- [ ] `parity`: `/health`, `/ws`, `/core`, `/files`, `/backups` through real Cloud (transport changes)
- [ ] `stress_10000`: `internal-cloud-stress` on one reference OS (transport or stress-harness changes)
- [ ] `keyrings`: real happy path and fail-closed no-UI on each enabled OS (adapter changes: affected OS)
- [ ] `domains_mfa`: one real canonical Nabu Casa OAuth flow
- [ ] `roles`: one real standard non-administrator binding
- [ ] `owner_no_code`: one real owner OAuth `cloud add` pairs with no code (Link owner pairing changes)
- [ ] `lifecycle`: `cloud add` → App restart/reinstall → CLI uninstall/reinstall → purge revokes the device
- [ ] `routing`: one real automatic fallback; `redirects_non_disclosure`: no credential in config, argv, logs, diagnostics, or AI-visible output
- [ ] `signing_and_update_matrix`: real install/update after installer, updater, signing, or native-authorization changes

Reference smoke (macOS/Linux; Unix provenance needs the installed layout under
`$HOME/.local/share/ha-nova`). Never copy the production `~/.config/ha-nova`
into the smoke HOME and never use the server name `default`: both would rotate
or revoke the production authorization.

```bash
SMOKE_HOME="$(mktemp -d)/home"; mkdir -p "${SMOKE_HOME}/.local/share"
tar -xzf ha-nova-installer-bundle-<os>-<arch>.tar.gz -C "${SMOKE_HOME}/.local/share"
BIN="${SMOKE_HOME}/.local/share/ha-nova/ha-nova"
SERVER_NAME="smoke-$(openssl rand -hex 4)"
HOME="${SMOKE_HOME}" "${BIN}" internal-cloud-release-check
HOME="${SMOKE_HOME}" "${BIN}" cloud add --server "${SERVER_NAME}" --url "https://<cloud-host>"
HOME="${SMOKE_HOME}" "${BIN}" relay health --server "${SERVER_NAME}" --via cloud
HOME="${SMOKE_HOME}" "${BIN}" cloud remove --server "${SERVER_NAME}" --yes
```

On Windows the install root resolves from the executable directory. The
dedicated Windows test machine keeps one standing, pre-authorized `smoke`
profile (#584) that is exempt from `cloud remove`; every other profile is
throwaway. From 0.26.0 (not rc1-rc3) every profile except `default` pairs
under its own device identity, so a throwaway `smoke-…` profile no longer
disconnects `default` or `smoke` on the same Relay. Still unsafe until `smoke`
is paired again by a fixed binary: pairing `default`, or any profile with an
older binary, drops `smoke`. Pairing `smoke` again means `cloud remove --server
smoke` plus `cloud add` (a maintainer step). A throwaway run that fails before
`cloud remove` leaves its device listed; revoke it on the NOVA page.

## 7. Release Channels and Post-Publish Verification

- `stable` = `vX.Y.Z`, the only normal public channel; `rc` = `vX.Y.Z-rcN`,
  tester-only, selected by exact version pinning
- normal install, `ha-nova check-update`, and `ha-nova update` target stable

Supported RC selection:

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/<rc-tag>/install.sh | HA_NOVA_VERSION=vX.Y.Z-rcN bash
ha-nova update --version vX.Y.Z-rcN   # installed runtime; plain `ha-nova update` returns to stable
```

Supported stable selection:

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/<stable-tag>/install.sh | HA_NOVA_VERSION=vX.Y.Z bash
```

```powershell
$env:HA_NOVA_VERSION = 'vX.Y.Z'
irm https://raw.githubusercontent.com/markusleben/ha-nova/<stable-tag>/install.ps1 | iex
```

After a final stable publish, merge the prepared site PR (see section 1; RCs
skip it), then run the commands above for the new tag and smoke
`ha-nova version`, `ha-nova check-update`, same-version `ha-nova update`, and
`ha-nova uninstall --yes`.

## 8. Release Notes Style

Fixed headings: `New Features`, `What To Watch`, `Bug Fixes`.

- `What To Watch` is optional: only real behavior changes, breaking changes,
  or action-needed items
- short, concrete, user-visible outcomes; do not list every small fix
- bullet order is importance: the compact update notice shows only the FIRST
  bullets (one action-needed item plus two feature/fix items) from
  `Breaking Changes`, `What To Watch`, `Upgrade Notes`, `New Features`, and
  `Bug Fixes`
- call out Windows installer/update/uninstall changes that affect real users
- stable release notes must publish tag-pinned install commands, never `main` bootstrap URLs
- legacy pre-Go installs run the dedicated legacy cleanup script, then reinstall
