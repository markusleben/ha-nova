# Prebuilt App Images — Spec (2026-10-15)

Status: active

## Problem

`nova/config.yaml` has no `image:`, so Supervisor builds `nova/Dockerfile` on
the user's device (apk nodejs/npm, `npm ci`, `tsc`) on install and on EVERY
update: 3–10 min on a Raspberry Pi, and it can fail on low RAM.

## Goal

Supervisor pulls `ghcr.io/markusleben/{arch}-ha-nova-relay:<version>` instead.
It pulls whenever the store config has `image:`
(`supervisor/docker/app.py`: `need_build=self.app.latest_need_build`;
`apps/model.py`: `need_build = ATTR_IMAGE not in self.data`). Without `image:`
it builds locally again (rollback path).

## Flow

1. `.github/workflows/app-image.yml` → `publish`: same-repo PRs into main
   whose `nova/config.yaml` has `image:` and whose version never reached main.
   Builds `nova/Dockerfile` once per `nova/build.yaml` arch on that arch's
   `build_from` base, then pushes one single-platform image per arch.
2. `app-image-verify` (every PR, every main push, dispatch):
   `scripts/release/verify-app-image.sh` checks anonymously that every arch
   image exists with the right platform and labels. If the change introduces
   the version or the `image:` line, every image must also carry this tree's
   build-input fingerprint (`io.ha-nova.inputs`, `app_image_inputs` in
   `scripts/release/app-image-lib.sh`). Otherwise existence only. No `image:`
   → nothing to verify.
3. The main-push run re-checks the fingerprint on the merge commit.

## Invariants

- **Image before config:** stores track main. A main commit with `image:` +
  version V requires public images for every arch first, or every
  install/update fails with "Could not pull". Enforced by
  `app-image-verify` as a required check.
- **Frozen once on main:** publish refuses any version that is not newer
  than main's or ever appeared in main's `nova/config.yaml` history (backup
  restores pull old tags). Before main, re-pushes track the PR head; a tag
  pushed by another PR (`io.ha-nova.pr` label) is never overwritten.
  Publishers are serialized (global concurrency group) and re-check main
  right before the cached push.
- **Image == merged code:** fingerprint match on the introducing change.
- **Multi-arch = `nova/build.yaml`:** `config.yaml` `arch:` must equal the
  `build_from` keys (verify script + `tests/app/config-contract.test.ts`).
- **Labels Supervisor reads:** `io.hass.version` (installed version,
  `docker/interface.py`) and `io.hass.arch`; plus `io.hass.type=app` like a
  Supervisor build, and `org.opencontainers.image.source` (links the package
  to the repo; overrides the base image's label).
- **Public:** verify uses an anonymous token, so a private package fails CI
  instead of every user's device.
- **Least privilege:** only `publish` holds `packages: write`; fork and
  stacked PRs never run it. Same-repo PR authors already control the workflow
  file, so the token in that job is no escalation.
- Dev deploys (`scripts/deploy/ha-app-deploy.sh`) strip `image:` so a local
  App still builds the local code.

## Known limits

- The fingerprint covers source files, not the floating `*-base:3.21` tag or
  `apk add nodejs npm`: two publishes of one tree can differ in Node patch.
- A merge inside the seconds between the pre-push re-check and the push still
  races; the main-push verify run flags it (then ship the next patch).
- Required checks go stale when main moves. With `--admin` the up-to-date
  rule is skipped, so the release rule below is mandatory.

## Rollout (sequencing)

1. **This PR** (no `image:`): workflow, lib, verify script, docs, e2e
   harness. Safe on main: no `image:` → publish skips, verify passes.
2. Maintainer: add `app-image-verify` to main's required checks (new name).
3. **Release PR #672** (0.9.0 → 0.9.1): update it with main, then commit
   `image: "ghcr.io/markusleben/{arch}-ha-nova-relay"` directly below
   `version:`. `publish` pushes `{amd64,aarch64}-ha-nova-relay:0.9.1`;
   `app-image-verify` fails until step 4.
4. Maintainer, once: GHCR → both new packages → Package settings → Change
   visibility → Public (check "Manage Actions access" lists the repo). Re-run
   the failed `app-image-verify` job → green.
5. Run the regression matrix below on the #672 head.
6. Merge rule for every PR that changes the App version: `gh pr update-branch`,
   wait for green `app-image-verify` on the new head, only then merge (also
   before any `--admin`). Confirm the main-push `app-image-verify` run on the
   merge SHA is green before tagging; red = ship the next patch version.

Never add `image:` to a version without published images (e.g. 0.9.0 on main
today): that version is frozen and can never be published.

## Rollback

Ship the next version WITHOUT `image:` → Supervisor builds locally again;
publish skips and verify passes, so a GHCR outage cannot block the rollback.
Never retag or delete a published version.

## Regression matrix (RC gate when the App image path changes)

`bash scripts/e2e/supervisor-app/run.sh` runs (a)–(c) against a throwaway
Supervisor in Docker (HA `devcontainer:6-apps`, privileged, no published
ports, anonymous volumes, 4 GB cap, removed on exit) — never a real Home
Assistant. OLD = `origin/main`'s App (local build), NEW = this checkout.

| # | Case | Pass criteria |
|---|------|---------------|
| a | Fresh install with `image:` | Supervisor log shows the download and no build; container runs `<image>:<V>`; 401 unauthenticated |
| b | Update locally built OLD → NEW via pull | `/data` (device registry, TLS cert + key, instance id) byte-identical; a credential migrated into the registry before the update still gets 200; reports NEW |
| c | Rollback: NEW+1 without `image:` | Supervisor builds locally again; `/data` byte-identical; credential works; reports NEW+1 |
| d | Arch coverage | `verify-app-image.sh <V>` OK for every `build.yaml` arch; e2e covers the host arch, CI builds the rest |
| e | Real GHCR pull | `E2E_IMAGE_SOURCE=ghcr bash scripts/e2e/supervisor-app/run.sh` once V is public |
| f | Client | a CLI paired before the update keeps working (`ha-nova relay health`) — manual, see below |

Manual on a throwaway HAOS VM (amd64 or aarch64) before the first `image:`
release: add the repo as `https://github.com/markusleben/ha-nova#<release-branch>`
only on a VM that first had 0.9.0 installed from main, pair the CLI, update
to 0.9.1 (Supervisor log: `Downloading docker image ghcr.io/markusleben/…`,
no `Starting build`), confirm `ha-nova relay health` still works without
re-pairing, then take and restore a partial backup of the App.
