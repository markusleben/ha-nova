# Preview channel for the NOVA App (2026-11-09)

Status: active

## Why

Today the App bump (`nova/config.yaml` `version`) lands on `main` with the
train landing PR, before the rc tag. Tags must be on `main`: `release.yml` and
`release-candidate.yml` refuse other commits. Supervisor stores read `main`.
So every Home Assistant user who added the repository is offered the new App
while only an rc is being tested. That happened with App 0.13.24 during
0.29.0-rc1. The same bump makes `relay-image.yml` move the standalone
`ha-nova-relay:latest` image. Goal: testing an rc never pushes an App update
on everyone.

## What Home Assistant offers

- An App's slug is the first 8 hex digits of the SHA-1 of the repository URL,
  then the folder slug. The official App is `2368fcfa_ha_nova_relay`. The
  documented beta pattern is a branch repository,
  `https://github.com/markusleben/ha-nova#beta` (developer docs → Apps →
  Presentation). Its slug is `817e1dcc_ha_nova_relay` (computed with the
  Supervisor rule), so it is a second App.
- `stage: experimental` only labels an App. The docs don't say it hides one.
- App data is per slug: the device registry and pairing, the TLS key that
  pairing pins, phone links, and the chat-app bridge credential. Two NOVA Apps
  can't run at once: both use host ports 8791 and 8792.
- The Supervisor offers an update for any installed version that differs from
  the store's, downgrades included (`need_update`: `version != latest_version`).
  It makes a backup before an update only when asked to.
- The Supervisor shallow-clones a repository and pulls it on its active
  branch (`git fetch` with the clone's refspec, then
  `reset --hard origin/<active branch>`). The VM lanes already switch the
  official repository's checkout to another branch (`docs/releasing.md` →
  Chat-app lanes). The App keeps its slug and its data, and the update
  installs in place.

## Options

| | A. Second App on `main` | B. Branch repository `#beta` | C. App bump off `main` until final |
|---|---|---|---|
| What | a `nova-beta/` folder: slug `ha_nova_relay_beta`, "NOVA Relay (Beta)", `stage: experimental` | a `beta` branch carrying the rc App; testers add `…/ha-nova#beta` | the bump rides an "App promote" PR into `main` that merges at the final; its published image is the preview |
| Stable users | see a second NOVA App in their store, one click from installing it | see nothing | see nothing |
| Tester, switching to the preview | stop the stable App, install the beta, pair again with a six-digit code, turn chat apps on again, link the phone again | same as A, after adding a repository | a backup, then a store switch from a root shell on the host and a Supervisor restart, then Update; pairing, phones and chat apps stay |
| Tester, switching back | uninstall the beta, start the stable App, pair again (the CLI pins the beta's key) | same as A | `ha store repair` plus a Supervisor restart (no root shell needed); the final ships the same App. To go back early, also restore the App backup |
| CLI | must learn a second slug for Home Assistant Link routing (`/app/<slug>`), the guided update entity, and uninstall | same as A | no change |
| Tests the real update from the stable App | no: a fresh App | no: a fresh App | yes |
| New machinery | a second App version on `main`; `app-image.yml` and metadata checks for each folder | a `beta` publish path, -rcN App versions, a job that moves `beta` | none: `app-image.yml` already publishes from a PR into `main` |

A and B need C's core too. While the stable bump reaches `main` before the
rc, stable users are offered it whatever channel exists. Also rejected:
tagging rcs on `release/*`. That would give the signing secrets to a branch
with no protection and no CI.

## Recommendation: C, where the promote PR is the preview

Shaped by the KISS review: no beta slug, no beta branch, no -rcN App version,
and no CLI change. Outside testers (B) wait until there are any.

1. **Landing.** The train landing PR carries the CLI, the skills and the
   `nova/` source, but not the App version. `nova/config.yaml` and
   `min_relay_version` stay at the stable App X. Merge it and push the rc tag
   on that commit, as today.
2. **Promote PR.** Right after the landing, open one PR into `main`. It changes
   only the version lines: `nova/config.yaml` from X to X+1, and
   `min_relay_version` to X+1 in `version.json` and `nova/version.json`.
   `app-image.yml` publishes X+1 from it, because that version was never on
   `main`. Stable stores still read X.
3. **Preview.** The VMs, and the maintainer's home if he decides so, switch
   the official store checkout to the promote branch. That takes a root host
   shell and a Supervisor restart. Then they update the App in place
   (proven, commands below). The rc test bed already builds the Relay from
   the tag.
4. **Final.** After the rc lanes and the maintainer's preview, merge the
   promote PR under the merge steps. Dispatch `e2e-disposable-ha.yml` on its
   merge commit and push the final tag there. Keep the post-merge checks of
   §4 step 1: `verify-relay-image.sh` and the `app-image-verify` push run on
   the merge commit. Users get exactly one App update per train, and it is the
   image the testers ran (see Image identity).
5. **Back to stable.** Run `ha store repair 2368fcfa` and restart the
   Supervisor on every switched store (a runbook step, no root shell). The
   App stays at X+1, which `main` now offers as well.

A train with no App change has no promote PR, and the rc and the final share
the landing commit as today.

### min_relay_version

On every `main` commit, the floor equals the App that `main` offers. The
equality check in `verify-release-metadata.sh` stays. The rc CLI keeps floor
X. The floor is only a version comparison: it doesn't prove that the rc CLI's
new features work against X. So an rc of an App train is supported only with
the preview App, and the rc lanes enforce the preview's exact version. A
stable CLI is only tagged on a commit whose floor is that commit's own App,
so it can never require an App that only testers have.

One new gap: a final tag on a landing commit whose App code changed without a
bump would ship a CLI that expects the new Relay while its floor is still X.
Guard, for final tags only: `release.yml` refuses when any App source changed
between the commit that brought the current App version to `main` and the
tag. The App sources are `nova/Dockerfile`, `nova/build.yaml`, `nova/run`,
`nova/package.json`, `nova/package-lock.json`, `nova/tsconfig.json`,
`nova/src`, `nova/integration` and `scripts/release/stage-app-bridge.sh`.
Release metadata (`nova/version.json`, `version.json`) changes with every
release and stays out. So do `cli/` and `skills/`: the bridge inside the
App follows the App version, as today (App Images: "a later bridge fix needs
an App patch version"). The guard needs full history, and rc tags skip it.

### Image identity

The source fingerprint doesn't pin an image: a rerun of the promote PR's
publish job may overwrite the same preview version, and base images and
packages resolve fresh. The App proof therefore records the per-architecture
image digests of the preview in the promote PR. Right before the merge, the
registry's digests must equal the recorded ones. A changed digest voids the
proof and needs a new patch version. Every push to the promote PR, an
update-branch included, rebuilds its images. So the proof runs after the
last push. With `main` frozen (below), the PR stays up to date without one.

### Fixes during the rc

An App or bridge fix goes into the train and then into a new landing (rc2).
The promote PR takes the new `main` and moves to the next patch version
(X+2): Supervisor doesn't offer a re-pushed image under a version that is
already installed. Stable users jump from X to X+2, which is harmless.
`app-image.yml` never overwrites a version that reached `main` or that
another PR pushed.

While the promote PR is open, nothing merges into `main` except rc landings.
Otherwise its update-branch would rebuild the App from inputs the testers
never ran. Dependabot and docs PRs wait.

### Store switch (VMs; the maintainer's home only by his own hand)

**What Supervisor does** (`home-assistant/supervisor` 2026.10.5, the VM's version):

- `store/git.py` `GitRepo.pull` fetches with the clone's refspec, which
  covers `main` only. It then compares the *active* branch with
  `origin/<active branch>` and resets only when they differ. A switched
  checkout therefore stays where it is.
- `store/__init__.py` `StoreManager.reload` re-reads App metadata only for a
  repository whose pull changed something. So after a switch, `ha store
  reload` and Check for updates (`api/root.py` `refresh_updates` → `store.reload`)
  keep showing the old version. The periodic refresh (`misc/tasks.py`
  `_reload_store`, every 3 h) behaves the same.
- `StoreManager.load` runs at every Supervisor start (`core.py`), and with it
  every host reboot. It loads the existing clone without re-cloning and
  always re-reads the metadata. A restart is what makes the switch visible,
  in both directions.
- `GitRepo.load` never checks the branch. Only a repository reset re-clones
  the configured branch (`main`). That happens through `ha store repair`
  (`api/store.py` `repositories_repository_repair`) or through the
  automatic fix for a corrupt repository (`resolution/fixups/store_execute_reset.py`,
  `auto: True`). Either one returns the store to stable, which is the safe
  direction.

**Proof on the throwaway HAOS VM** (2026-11-09, App 0.13.23 installed, `main`
offering 0.13.24). The checkout was switched to tag v0.28.2 (App 0.13.20) as
a stand-in preview:

- right after the switch, a store reload still offered 0.13.24;
- after the Supervisor restart it offered 0.13.20, and a second reload plus
  `refresh_updates` still offered 0.13.20 with the checkout unchanged. Note
  that this offer was a downgrade: Supervisor offers any different version
  as an update;
- switching back with a plain `git checkout main` plus a reload kept
  0.13.20, and only the restart showed 0.13.24 again;
- with the checkout on `release/0.29` (App 0.13.24) and a restart, the
  update installed in place on the same slug. The log showed "Downloading
  docker image … 0.13.24" with no build. The device registry stayed the
  same (10 devices, same canonical SHA-256), and so did the TLS key that
  pairing pins (same SPKI hash on 8792);
- `ha store repair 2368fcfa` plus a restart put the store back on `main`,
  offering 0.13.24;
- at the end, the App backup was restored: 0.13.23, the same registry hash
  and the same key.

**Commands.** First make a backup of the NOVA App and check that it is listed.
Getting in needs a root shell on the host, because `git` only exists inside
the Supervisor container:

```bash
docker exec hassio_supervisor sh -c 'cd /data/apps/git/2368fcfa && git fetch --depth 1 origin +refs/heads/<promote-branch>:refs/remotes/origin/<promote-branch> && git checkout -B <promote-branch> origin/<promote-branch>'
ha supervisor restart     # required: a reload doesn't show the switch
# then Settings → Apps → NOVA Relay: check the offered version, Update
```

Older Supervisors use `/data/addons/git`. Getting back needs no root shell.
Any terminal with the `ha` command works, the official Terminal & SSH app
included:

```bash
ha store repair 2368fcfa
ha supervisor restart
```

**Root shell options.**

- The HAOS console: the machine's own screen and keyboard, or a VM console.
  Type `login` at the `ha >` prompt. No setting changes.
- The community Advanced SSH & Web Terminal app with protection mode off.
  It needs to stay off only for the switch, about five minutes per preview
  version, then go back on (an app restart). While it is off, whoever can
  open that terminal (any Home Assistant admin, or anyone with its SSH key
  or password) has root on the host and its Docker. So would a compromised
  build of that app. The official Terminal & SSH app has no Docker access.
- The HAOS debug SSH on port 22222. It needs a key imported over USB, and the
  root access then lasts until removed. Too heavy.

Neither the Supervisor API nor the NOVA App's `manager` role can change a
repository's branch: the branch is part of the repository's URL, and with
it the slug. **There is no way in without a root shell**, so the switch is
for the maintainer and the VMs only, never for a non-technical tester.
Outside testers need B.

Each new preview version (rc2 → X+2) needs the fetch, the checkout and the
restart again.

Going back early, before the final: `ha store repair`, then restart, then
restore the App backup. Don't take the store's offer of X: X has never read
X+1's data.

Known limit: a store left on the promote branch gets no more App updates.
Supervisor keeps comparing it with its own stale ref. The final checklist
repairs every switched store and checks that it offers the stable version. A
CLI below a later floor warns too. Agents run the switch only on the
throwaway VMs (AGENTS.md → Hard rules).

### Effect on `docs/releasing.md` and AGENTS.md

- §1 and AGENTS.md → Releases: the App bump moves from the release-prep PR to
  the promote PR, still once per train. The rule of at most one App update a
  day counts promote merges. Preview pushes only reach switched stores.
- App Images: the App proof moves from the landing PR to the promote PR's
  image. It checks the update from the stable App, an image download with no
  local build, `ha_ws_connected: true`, and a paired read. The chat-app lanes
  move with it. The line saying the App stays visible to users during the
  tagged RC is removed. So is AGENTS.md's note that `app-image-verify` fails
  on a train PR that bumps the App, since trains no longer bump it.
- §3 RC gate: a train that changes the App always runs an rc, because the rc
  lanes are where the preview App is tested.
- §4: step 1 waits for the App image on the promote PR, not on the landing.
  `vm-lane` runs on the preview App. Step 5 adds the promote merge, its own
  e2e dispatch and the switch back. The final tag goes on the promote merge
  commit, not on the rc commit. The two commits differ only in the version
  lines.
- §6: the rule that `min_relay_version` equals the App version now changes
  at the promote merge. The hosted demo is re-provisioned with that merge
  commit.

### Hosted demo and standalone image

The demo never reads a Supervisor store, so no channel reaches it. It builds
its own Relay and bridge images from a pinned merge commit. Re-provision it
from the final tag's commit, which is the promote merge. Never use an rc
landing: its Relay would run new code under the old version.
`relay-image.yml` publishes `ha-nova-relay:<version>` and `:latest` only on
a push that changes the App version, so standalone users now also get the
new image at the final.

## Build (each PR ≤ ~400 production lines)

1. **Docs and final-tag guard** (~80 production lines). Changes
   `docs/releasing.md` and AGENTS.md (Releases, Merge steps), and adds
   `scripts/release/verify-app-promoted.sh` to `release.yml`'s final-tag path,
   with tests. Proves: a final tag with changed App sources and no bump fails;
   a CLI-only, skills-only or metadata-only final passes; rc tags skip it. The
   runbook gains the digest comparison before the promote merge.
2. **vm-lane preview** (~150). `npm run vm-lane -- <tag> --app-branch <promote-branch>`
   switches the throwaway HAOS VM's store and updates the App in place after
   its existing backup. It requires the preview's exact version. It runs the
   App proof rows beside the existing lanes: image download, version,
   `ha_ws_connected`, a paired read, and the per-architecture digests for the
   promote PR. Cleanup keeps today's restore-and-verify of the backup, then
   runs `ha store repair` and a Supervisor restart, and checks the offered
   version. Proves on
   a VM: X updates to X+1 in place with pairing kept, the early way back
   restores X with an unchanged device registry, and the store follows `main`
   again.
3. **First train with an App change** (no code). Runs landing → rc → promote
   PR → lanes and preview → merge → e2e → final, which proves the whole chain.
   Known limits go into the PR.

Later, only once outside testers exist: B as its own PR stack. That is a
`beta` branch kept at the promote head, plus CLI support for
`817e1dcc_ha_nova_relay` (Link routing, guided update, uninstall), with the
cost of a fresh App spelled out for testers.

## Cross-check

A KISS review shaped the recommendation. It dropped A, the beta branch and
-rcN App versions, and it made the promote PR the preview.

An independent read-only review (separate model) agreed with changes. It
found C the simplest fit for today's testers, the maintainer and the VMs:
the slug and pairing stay, stable stores never see the bump, and tags stay
on `main`. It confirmed that `app-image.yml` publishes and re-publishes from
such a promote PR, and that a landing with no bump only gets the existence
check. Its findings, all folded in above:

- the final-tag guard needs an exact set of App sources: `nova/version.json`
  changes with every release;
- the image testers ran must be pinned by its digests, not by the source
  fingerprint;
- a low floor doesn't prove compatibility, so the rc requires the preview;
- going back early needs an explicit backup, because Supervisor offers
  downgrades as updates;
- every preview version needs the switch again;
- the post-merge image checks stay.

Then the store switch itself was traced in Supervisor's code and proven on
the throwaway VM (Store switch section). It is robust: it survives reloads,
the periodic refresh, Check for updates and restarts, and only a repository
reset returns the store to stable. But it shows only after a Supervisor
restart, and getting in needs a root host shell. That narrowed decision 2.

## Decisions for the maintainer

1. The order for trains that change the App: landing, then the rc on the
   landing commit, then the promote PR merged at the final, with the final
   tag on that merge commit. The rc and the final no longer share a commit.
   Recommended: yes.
2. Your home's preview, given that getting in needs a root host shell:
   - (a) the switch from the HAOS console (`login`), or from Advanced SSH &
     Web Terminal with protection mode off for about five minutes per preview
     version, with an App backup first. The way back is `ha store repair` and
     needs no root shell.
   - (b) B for your home, at the fresh-App cost: pair again, turn chat apps on
     again, link the phone again, each way.
   - (c) no preview at home: the VMs carry the App proof, and your home updates
     at the final like everyone else.

   Recommended: (a) when the console is reachable or the five-minute
   protection-mode window is acceptable, else (c). Outside testers never get
   (a).
3. An App fix during the rc gets the next patch version, so stable users may
   see version gaps (X → X+2). Recommended: yes.
4. `main` is frozen while the promote PR is open: Dependabot and docs PRs
   wait about one to three days. Recommended: yes.
5. Outside beta testers (B, with a CLI change): later, once someone asks.
   Recommended: later.
6. Preview pushes reach only switched stores, so they don't count toward the
   limit of one App update a day. Recommended: they don't count.
