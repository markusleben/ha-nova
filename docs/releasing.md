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

## README and Release Notes

`README.md` is stable release truth and is guarded by the `readme-release-gate`
required check: a PR touching `README.md` passes only when `version.json`
changes in the same PR (this release-prep PR) or a maintainer applies the
`readme-stable:approved` label for corrections that describe the CURRENT
stable release. Concretely:

- unreleased feature/version claims collect in the active
  `docs/work/next-release-body.md` draft, never in `README.md` (version-free
  by design; the release-prep PR renames it once it picks the number)
- the release-prep PR carries ALL release-bound `README.md` edits plus the
  version bump and the `.goreleaser.yml` release-notes update, then moves the
  consumed release-body draft to `docs/archive/work/`
- before opening the release-prep PR, run a docs parity sweep: the skill
  count and full skill list in `README.md` match `skills/` (the dispatch
  table in `skills/ha-nova/SKILL.md` is authoritative), the release's
  user-facing features are reflected in `README.md` where sensible, and no
  shipped HA NOVA capability is still described as missing or
  competitor-only anywhere in `README.md` or the dated comparison page
  (v0.23.0 shipped the `hacs` skill; `README.md` kept saying "29 skills"
  and crediting HACS management to the competitor until after v0.24.0)
- merging the release-prep PR starts the RC/final tag sequence immediately
  (AGENTS.md: the main-ahead-of-stable window stays minutes, not days)

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

The review fast path, the round-five batch, and the manifest/README label
ordering are NOT restated here. They live in AGENTS.md -> "PR Merge / Release
Commit Gate" and apply to release PRs unchanged. Two copies of an ordering rule
drift, and this one already did: the copy here kept telling agents to push and
retrigger at the round where the batch is now due, and to label a manifest
before anything had inspected it.

## Release Worthiness

Do not cut a new version just because `main` moved.

Default rule:
- release when the merged delta changes shipped behavior, installer/update flow, release/runtime compatibility, or fixes a user-facing bug people can actually hit
- batch docs-only, test-only, process-only, and internal maintenance into the next real user-facing release unless they fix the release path itself

## Dependabot Fast Lane

Safe auto-merge is intentionally narrow.

Allowed fast lane:
- dev-only npm minor/patch updates that touch only `package.json` / `package-lock.json` (root or `nova/`)
- github-actions minor/patch bumps that change only `uses:` lines under `.github/workflows/` (diff-guarded by `dependabot-safe-lane-prepare.yml`)

Explicit exclusions:
- safe lane excludes toolchain-risk dependencies such as `vitest`, `vite`, `typescript`, `tsx`, `rollup`, `rolldown`, and `esbuild`
- action majors and any workflow change beyond `uses:` version bumps stay manual
- installer, runtime, release, security, and non-manifest changes stay manual

Required protection posture on `main`:
- require `dependency-review` on `main`
- require `manifest-review-gate` on `main`
- `codex-review-gate` is advisory on `main`

## Release Candidate Gate

The **tag-first dress rehearsal** runs the exact stable pipeline against a
prerelease, so any pipeline breakage surfaces on the `-rcN` tag and never on
the stable publish. It is a **conditional gate** (decision 2026-07-13): the
rehearsal pays off exactly when the delivery machinery itself changed, and
only clutters the releases page when it did not.

**RC required** when the release contains any delta, since the last green
`release.yml` run, to the delivery machinery:

- `install.sh` / `install.ps1` / `scripts/release/` bundle or verify scripts
- `.goreleaser.yml` beyond the release-notes body text
- `release.yml`, `release-candidate.yml`, other release workflows, or the
  `release-tags-protection` ruleset
- `census-worker/` request, storage, or deployment behavior
- Go code (CLI/relay — it ships in the release assets)
- onboarding/update flow that the installed artifacts execute

**RC skippable** for skills/docs-only releases (markdown + tests + notes
text). Every release — with or without RC — still runs the full remaining
gate: preflight PR audit, the strict pipeline audit and dispatched e2e below,
the final tag's own `release.yml` verify + 3-runner public-install smoke, and
a local public-install verification after publish. When in doubt, or if the
machinery delta is ambiguous, run the RC.

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

### Home Assistant Cloud publication gate

`version.json` is the release switch. While `cloud_remote_enabled` is `false`,
`cloud_remote_platforms` must be empty and no external Cloud evidence is
required. A release cannot advertise Cloud remote support merely because the
implementation exists in the tree.

Enabling the switch requires a non-empty, duplicate-free subset of `darwin`,
`linux`, and `windows`. The `production` GitHub environment must first be
protected by exactly two typed custom deployment refs: branch `main` and tag
`v*`; protected-branch mode and every other branch/tag are forbidden. Verify
the live state with:

```bash
bash scripts/release/verify-github-production-environment.sh
```

An unprotected or drifting environment is an activation and release blocker:
the trusted source gate and both release workflows run this verifier before
reading Cloud evidence or using production secrets. Both release workflows
also re-run the read-only live main-protection verifier before metadata and
Cloud evidence, so non-strict checks or an incorrect required-App binding
cannot publish a previously accepted source state. Fix GitHub policy drift,
then rerun the failed gate. Enabled publication mints a short-lived installation
token from the source-check App credentials scoped down to
`Administration: read`; it never grants Checks write to the publication gate.
Disabled publication exits before requiring those App credentials.

The required `cloud-source-gate` is emitted by the dedicated
`ha-nova-cloud-source-gate` GitHub App after a trusted
default-branch `workflow_run`. The broker runs after `CI` for pull requests and
merge groups, including Dependabot runs; it never reads upstream artifacts or
caches, checks out PR code, or executes a target path. An `in_progress`
delivery creates only an attempt-bound provisional pending App check; this
invalidates prior success during initial runs and reruns without resolving a
merge ref or executing policy code. A successful `completed` delivery creates
the exact target-bound pending check before retiring the provisional check.
Cancelled or failed CI retires its provisional check because CI itself remains
failed. Stale pull requests and GitHub races before a merge ref is materialized
exit without an exact check and remain fail-closed. The broker
resolves the current PR head and base, fetches GitHub's merge ref, verifies its
two parents, and materializes only its three release metadata files for
parsing. The first resolved merge SHA is passed to the verifier as an exact
target. The broker then re-reads the PR identity, immediately re-fetches the
same merge ref, and performs one final current-PR identity resolution before
reporting; a regenerated or moved ref and a no-longer-current PR fail.
Both PR snapshots must expose the same `merge_commit_sha` as the fetched merge
ref. Merge-queue runs apply the same final ref check and bind and report the
exact queue SHA. Workflow comparison is data-only through trusted Git commands
and the trusted default-branch helper.

While Cloud Remote is disabled and the dedicated App ID remains the explicit
unprovisioned value `0`, the trusted default-branch broker exits successfully
before reading any App credential or evidence secret. Enabled metadata with an
unprovisioned ID fails closed. After the App ID is provisioned, the broker runs
even while Cloud remains disabled so its required-check canary and lifecycle
can be verified before activation.

The App is installed only on this repository and has only Metadata read,
Administration read, and Checks write. Administration is read-only and exists
solely so every broker run can reject non-strict branch protection, a missing
required context, or a context not pinned to the exact App ID before reporting
success. Its private key and App ID are `production` environment secrets named
`HA_NOVA_CLOUD_SOURCE_CHECK_APP_PRIVATE_KEY` and
`HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID`. The ordinary Actions token cannot emit the
required context. Dependabot auto-merge re-evaluates when the App check
completes, but a read-only first job authenticates its exact name, App ID, and
App slug before any write-capable job can start.

The safe-lane preparation workflow also emits a repository dispatch only after
its automation-owned label and exact current-policy marker are recorded. The
dispatch runs the same current-default-branch resolver and direct merger; it
never checks out or executes pull-request code. This closes the ordering where
all required checks completed before a draft Dependabot pull request became
ready. An early dispatch with incomplete checks exits without merging and later
trusted check completions re-evaluate normally.

While the target enables Cloud, the evidence
always binds its own exact evidence commit and full source tree. It may cover a
different commit only when the complete Git tree is byte-identical, as with the
reviewed activation PR's squash commit. Otherwise, it may cover a newer target
only when that evidence commit is an ancestor and the complete evidence-to-target
tree delta consists exclusively of one permitted class. The first class is
the permitted existing non-sensitive `uses:` version changes: such a `uses:`
change must retain the action identity, move forward within the same major
release, use full commit SHAs, and have both SHAs resolve to their stated
canonical `vX.Y.Z` release tags through the GitHub API. The second class is
a non-sensitive source delta: regular non-executable Markdown files under
`docs/` or `skills/`, or root-level Markdown, none with an agent-policy
basename (stems `agent(s)`/`claude`/`gemini` with any suffix, any depth,
case-folded),
where no changed line touches a download or install command, a raw-script
or CDN script source, or a shell line
continuation — a best-effort guard over a forced textual diff
(`scripts/release/verify-cloud-nonsensitive-source.mjs`; the spec's `None`
invalidation row and its escape section). `tests/**` is deliberately not in
this class: privileged release workflows execute repository tests with
production-environment secrets. Mixed deltas and everything else fail
closed. Workflow additions,
deletions, renames, file-mode changes, changes to `cloud-source-gate.yml`,
`ci.yml`, `release.yml`, or `release-candidate.yml`, and every non-`uses:`
workflow change fail closed. This preserves the reviewed Dependabot GitHub
Actions minor/patch lane without allowing mutable action refs or arbitrary
commits under stale evidence. Disabled targets remain compatible with normal
reviewed workflow maintenance.

The broker and both release workflows require
`HA_NOVA_CLOUD_GATE_EVIDENCE_JSON` from the `production` environment. The
normal CI job reads the repository secret only for direct `main` pushes; pull
requests and merge groups are covered by the trusted broker, so Dependabot
never needs a repository secret. Disabled metadata exits before reading the
value.

Because the value lives in those two places, set BOTH — otherwise the pull
request goes green and `main` fails `ci-gate` seconds after the squash merge,
with the stale-evidence message, which reads like a tree mismatch and sends
you looking in the wrong place (observed on #559):

```bash
gh secret set HA_NOVA_CLOUD_GATE_EVIDENCE_JSON -R markusleben/ha-nova --body "$(cat envelope.json)"
gh secret set HA_NOVA_CLOUD_GATE_EVIDENCE_JSON --env production -R markusleben/ha-nova --body "$(cat envelope.json)"
gh secret list -R markusleben/ha-nova | grep EVIDENCE
gh api repos/markusleben/ha-nova/environments/production/secrets --jq '.secrets[] | select(.name|startswith("HA_NOVA_CLOUD_GATE")) | .updated_at'
```

Both timestamps must be from the current session.

Then, right after the squash merge, rewrite `commit_sha` and
`relay_app.source_commit` to the resulting `main` commit and store the
envelope again in both places. The tree is unchanged, so this attests
exactly the same content — but the name has to survive. A pull request's
synthetic merge commit is thrown away at merge time and is an ancestor of
nothing afterwards, and BOTH stale-evidence escapes require the evidence
commit to be an ancestor of their target. Leave the envelope pointing at
the merge commit and every later pull request fails the gate with the
stale-evidence message, even a docs-only one that the escape should carry
(observed on #560, the first pull request after the escape landed).

The mechanical steps of this flow — resolving the exact merge target, binding
and reusing the one candidate dispatch, verifying the artifact checksums,
running the per-platform provenance checks, writing the secret to BOTH
locations with verified timestamps, and the post-merge repoint — are
automated by `scripts/release/build-cloud-evidence.sh` (usage in its header).
The attestation never is: the script refuses to set any check boolean and
takes the maintainer's envelope file as input. The commands above stay the
canonical reference.

All paths run:

```bash
bash scripts/release/verify-cloud-release-gate.sh
```

Before enabling the required check, create the private GitHub App with the
exact slug `ha-nova-cloud-source-gate` and the permissions above,
install it only on this repository, and store its App ID and private key in the
two `production` environment secrets. Let the broker emit one expected failing
disabled canary so GitHub registers the App check. While the check is not yet
required, replace the intentionally unprovisioned `0` App ID in
`.github/policy/repo-policy.json` through a reviewed PR. After that policy is on
`main`, select the specific App as the expected source for
`cloud-source-gate`, enable strict up-to-date checks and stale-review
dismissal, and rerun CI on a disabled test PR. Exercise one disabled Dependabot
PR and run `bash scripts/release/verify-github-main-protection.sh`. The broker
and verifier both reject a non-strict policy, an unprovisioned ID, and every
different or unbound `app_id`. Do not open an activation PR until the live
source check and Dependabot reevaluation are proven.

The JSON is commit-specific and has this exact schema:

```json
{
  "schema": 2,
  "commit_sha": "<exact-40-character-evidence-commit>",
  "tree_sha": "<exact-40-character-full-source-tree>",
  "relay_app": {
    "version": "<nova/config.yaml version at the evidence commit>",
    "source_commit": "<same exact evidence commit>",
    "source_tree_sha": "<same exact full-source tree>"
  },
  "checks": {
    "parity": true,
    "stress_10000": true,
    "keyrings": {
      "linux": true
    },
    "roles": true,
    "domains_mfa": true,
    "lifecycle": true,
    "redirects_non_disclosure": true,
    "installed_relay_app": true,
    "routing": true,
    "signing_and_update_matrix": true
  }
}
```

The `keyrings` keys must exactly match `cloud_remote_platforms`. Each `true`
value attests the exact-target checks plus every still-applicable risk-scoped
qualification. Before carrying a qualification forward, inspect the complete
qualification-to-target diff. In the activation or release pull request,
record each carried check and keyring OS with its qualified commit/tree,
non-secret evidence reference, inspected target, and change-class decision.
Missing or uncertain ledger data means rerun.

Every target still runs CI, candidate signature/provenance checks on all
enabled platforms, and the exact installed Relay App. A target whose delta
matches an invalidation-map row with real-platform scope also runs one real
Cloud health smoke on a reference platform; maintenance deltas skip it. The
smoke uses the downloaded candidate binary, `--via cloud`, the exact installed
App, and `HA_NOVA_NO_CENSUS=1`; its JSON must report the expected App version
and `ha_ws_connected: true`.

- `parity`: `/health`, `/ws`, `/core`, `/files`, and `/backups` through real
  Home Assistant Cloud on one reference platform after a transport change;
- `stress_10000`: one bounded Ingress-session stress run with 10,000 commands
  after a Cloud or Relay transport change or stress-harness change, not once
  per operating system;
- `keyrings`: real happy-path and fail-closed no-UI behavior on each enabled
  platform for first support; shared orchestration changes repeat one reference
  OS and adapter changes repeat only their affected OS; deterministic tests
  cover cancellation and timeout branches;
- `roles`: one real standard non-administrator binding plus exact-target user
  and Relay identity-binding tests; Owner and administrator add no separate
  transport path;
- `domains_mfa`: one real canonical Nabu Casa OAuth flow plus deterministic
  custom-origin, inactive-subscription, disabled-remote, and
  authorization-abort branches; Home Assistant owns its MFA challenge before
  returning the same OAuth callback;
- `lifecycle`: one isolated Cloud-authorized profile first proves Relay App
  restart and reinstall recovery, then HA NOVA CLI standard
  uninstall/reinstall with retained authorization, then full purge last; the
  purge revokes and verifies the active remote authorization and device before
  local cleanup; deterministic durable-boundary and concurrency recovery
  covers the other branches, with real update or instance-mismatch runs after
  those paths change;
- `redirects_non_disclosure`: redirect rejection plus credential absence from
  config, argv, logs, diagnostics, and AI-visible output, with one retained
  real artifact scan after a relevant transport, config, argv, log,
  diagnostics, or AI-output change;
- `installed_relay_app`: the App installed for the real-device proof was built
  by Supervisor from the exact reviewed source commit, reports the exact
  `nova/config.yaml` version, and contains the reviewed Cloud endpoints;
- `routing`: one real automatic fallback after a routing change plus
  exact-target fail-closed selection tests; and
- `signing_and_update_matrix`: exact candidate signature and provenance on all
  enabled platforms, with real install/update repetition only after installer,
  updater, signing-identity, or native-authorization changes.

No mock replaces a required real positive path. Exact-target CI, candidate
provenance, and `installed_relay_app` are never carried forward. The Cloud
health smoke repeats only when the delta matches an invalidation-map row with
real-platform scope; deltas matching only the map's `None` and
release-machinery rows refresh the envelope and provenance without it. The
map, not any summary, decides. See
`docs/work/2026-07-30-cloud-release-evidence-risk-scope-spec.md`.
Carry-forward applies only to the qualification behind a check boolean: create
a new exact-target JSON envelope and never copy an older commit/tree identity.
The complete bounded change-class map is in that spec. A test or harness change
invalidates every qualification that relies on it as deterministic or real
evidence.

For the first evidence on an activation pull request, dispatch the
non-publishing candidate builder exactly once from current `main`.

The pull request must be ready, same-repository, current with `main`, and green
on every protected/advisory check except the expected failing
`cloud-source-gate`. It must also have a real clean Codex bot result for the
current head; an advisory gate timeout does not qualify. The workflow resolves
GitHub's exact synthetic merge commit, uses only trusted build/signing scripts
from `main`, smokes the exact raw binaries natively on all three platforms,
then revalidates and creates the hash-bound signed bundles. It verifies the
binary identity and signed platform/architecture provenance in all five
archives with the public release key. Raw transport artifacts lack Cloud
provenance, fail closed, and expire after one day. The final
`cloud-candidate-install-bundles` artifact is retained for seven days.
The workflow never creates a deployment, tag, or release. The normal RC and
release workflows remain evidence-first. In this solo-maintainer repository,
dispatch by the exact maintainer account is the candidate approval when GitHub
reports `REVIEW_REQUIRED`; requested changes and unresolved threads still
block the run.

Dispatch, capture, and monitor that single run:

```bash
PR_NUMBER=469 # replace
VERSION_TAG=v0.22.0-rc1 # replace
REQUEST_ID="$(date -u +%Y%m%dT%H%M%SZ)-$$"
# Runs cannot be found by name or request id: the workflow's `run-name:`
# loses its interpolations to YAML (the unquoted `#` starts a comment), so
# every run is titled just "Cloud candidate PR" — and `gh workflow run`
# prints no run id either. Fence on the highest run id seen BEFORE the
# dispatch; the id fence plus the bundle-identity checks below are the real
# binding.
MAX_BEFORE="$(gh run list --workflow cloud-candidate-bundle.yml --limit 30 \
  --json databaseId --jq '[.[].databaseId] | max // 0')"
gh workflow run cloud-candidate-bundle.yml --ref main \
  -f pull_request="${PR_NUMBER}" -f version_tag="${VERSION_TAG}" \
  -f request_id="${REQUEST_ID}"
RUN_ID=""
for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do
  sleep 5
  RUN_ID="$(gh run list --workflow cloud-candidate-bundle.yml \
    --event workflow_dispatch --limit 30 --json databaseId \
    --jq "map(.databaseId | select(. > ${MAX_BEFORE})) | min // empty")"
  [[ -n "${RUN_ID}" ]] && break
done
[[ "${RUN_ID}" =~ ^[0-9]+$ ]] || {
  echo "Dispatched run not found; do not dispatch again." >&2
  exit 1
}
if ! gh run watch "${RUN_ID}" --exit-status; then
  gh run view "${RUN_ID}" --log
  exit 1
fi
gh run view "${RUN_ID}" --json url,headSha,status,conclusion
gh run download "${RUN_ID}" -n cloud-candidate-install-bundles \
  -D cloud-candidate-install-bundles
(
  cd cloud-candidate-install-bundles
  for checksum in *.sha256; do shasum -a 256 -c "${checksum}"; done
)
```

Record the run URL, candidate commit, and tree from the run summary. Download
the artifact promptly. If the run fails, inspect its logs and fix the cause in
a reviewed pull request; workflow reruns are rejected, so use one new explicit
dispatch only after the fix is reviewed.

Never call a `ha-nova` found through `PATH`: extract each matching platform
archive, verify its bundle identity against the run summary, and invoke that
contained binary by its explicit path. On every enabled platform run the
provenance check. For macOS or Linux:

```bash
EXPECTED_TREE=0123456789abcdef0123456789abcdef01234567 # replace
VERSION_TAG=v0.22.0-rc1 # replace
CANDIDATE_OS=macos
CANDIDATE_ARCH=arm64
# A unique name also isolates the OS-keyring device-credential slot, which is
# keyed by server name; "default" would reuse (and `cloud remove` would
# revoke) the production device credential. Node is required anyway and fails
# loudly, unlike optional generators whose absence a pipeline would swallow.
SERVER_NAME="smoke-$(node -e 'console.log(require("node:crypto").randomBytes(4).toString("hex"))')"
EXPECTED_RELAY_APP_VERSION=0.7.1 # replace from exact-target nova/config.yaml
# Unix builds resolve their install root from HOME, so the provenance check
# passes only from an installed layout (see be0c5e2); calling the extracted
# binary in place fails with "official Cloud release provenance is not
# enabled".
CANDIDATE_DIR="$(mktemp -d)"
SMOKE_HOME="${CANDIDATE_DIR}/home"
mkdir -p "${SMOKE_HOME}/.local/share"
tar -xzf "cloud-candidate-install-bundles/ha-nova-installer-bundle-${CANDIDATE_OS}-${CANDIDATE_ARCH}.tar.gz" \
  -C "${SMOKE_HOME}/.local/share"
CANDIDATE_BIN="${SMOKE_HOME}/.local/share/ha-nova/ha-nova"
node - "${SMOKE_HOME}/.local/share/ha-nova/bundle.json" "${VERSION_TAG#v}" "${EXPECTED_TREE}" <<'NODE'
const fs = require("node:fs");
const [path, version, tree] = process.argv.slice(2);
const bundle = JSON.parse(fs.readFileSync(path, "utf8"));
if (bundle.version !== version || bundle.cloud_release?.source_tree_sha !== tree) {
  throw new Error("candidate bundle identity does not match the recorded run");
}
NODE
HOME="${SMOKE_HOME}" HA_NOVA_NO_CENSUS=1 "${CANDIDATE_BIN}" internal-cloud-release-check
```

On the Windows Proxmox test machine, use PowerShell and the contained
executable explicitly:

```powershell
$ExpectedTree = "0123456789abcdef0123456789abcdef01234567" # replace
$Version = "0.22.0-rc1" # replace
# Unique name: the device-credential slot is keyed by server name; "default"
# would reuse or revoke the production device credential.
$ServerName = "smoke-" + [guid]::NewGuid().ToString("N").Substring(0, 8)
$ExpectedRelayAppVersion = "0.7.1" # replace from exact-target nova/config.yaml
$CandidateDir = Join-Path $env:TEMP ("ha-nova-cloud-candidate-" + [guid]::NewGuid())
Expand-Archive `
  -LiteralPath "cloud-candidate-install-bundles\ha-nova-installer-bundle-windows-amd64.zip" `
  -DestinationPath $CandidateDir
$CandidateRoot = Join-Path $CandidateDir "ha-nova"
$Bundle = Get-Content (Join-Path $CandidateRoot "bundle.json") -Raw |
  ConvertFrom-Json
if (
  $Bundle.version -ne $Version -or
  $Bundle.cloud_release.source_tree_sha -ne $ExpectedTree
) {
  throw "candidate bundle identity does not match the recorded run"
}
$CandidateBin = Join-Path $CandidateRoot "ha-nova.exe"
$env:HA_NOVA_NO_CENSUS = "1"
& $CandidateBin internal-cloud-release-check
if ($LASTEXITCODE -ne 0) { throw "candidate provenance check failed" }
```

Choose exactly one reference platform for the exact-target Cloud health smoke.
The smoke is required only when the delta matches an invalidation-map row with
real-platform scope; deltas matching only the map's `None` and
release-machinery rows refresh the envelope and provenance without it. Consult
the map in the evidence risk-scope spec rather than any shorthand.

The smoke needs its own Cloud authorization inside the smoke HOME. NEVER copy
the production `~/.config/ha-nova` there: the copied profile keeps the
production ProfileID, which is the keychain account under the fixed
`ha-nova.oauth.home-assistant-cloud.*` services — `cloud add` on top of it
rotates the PRODUCTION Cloud authorization. A fresh profile in an empty smoke
HOME gets a new ProfileID and is collision-free by design. `<cloud-host>` is
the Nabu Casa remote origin (or a custom domain on it), not the LAN host. Set
it up once per smoke (browser OAuth opens interactively) and remove it
afterwards:

```bash
HOME="${SMOKE_HOME}" HA_NOVA_NO_CENSUS=1 "${CANDIDATE_BIN}" cloud add \
  --server "${SERVER_NAME}" --url "https://<cloud-host>"
```

Run the health smoke below, plus the `internal-cloud-stress` proof when a
transport or stress-harness delta requires it. The profile removal command is
in the cleanup step at the end of this section, after the last Cloud command.

On Windows the install root resolves from the executable directory
(be0c5e2), so no HOME override exists or is needed; create the same isolated
authorization with the extracted binary on the dedicated test machine (which
carries no production profile) and remove it after the last Cloud command:

```powershell
$env:HA_NOVA_NO_CENSUS = "1"
& $CandidateBin cloud add --server $ServerName --url "https://<cloud-host>"
```

Use the same explicit downloaded candidate binary and exact installed App:

```bash
HEALTH_JSON="$(
  HOME="${SMOKE_HOME}" HA_NOVA_NO_CENSUS=1 "${CANDIDATE_BIN}" relay health \
    --server "${SERVER_NAME}" --via cloud
)"
node -e '
  const fs = require("node:fs");
  const payload = JSON.parse(fs.readFileSync(0, "utf8"));
  if (
    payload.ok !== true ||
    payload.data?.status !== "ok" ||
    payload.data?.ha_ws_connected !== true ||
    payload.data?.version !== process.argv[1]
  ) {
    throw new Error("candidate Cloud health payload is not release-ready");
  }
' "${EXPECTED_RELAY_APP_VERSION}" <<<"${HEALTH_JSON}"
```

```powershell
$env:HA_NOVA_NO_CENSUS = "1"
$HealthJson = (& $CandidateBin relay health --server $ServerName --via cloud |
  Out-String)
if ($LASTEXITCODE -ne 0) { throw "candidate Cloud health smoke failed" }
$Health = $HealthJson | ConvertFrom-Json
if (
  $Health.ok -ne $true -or
  $Health.data.status -ne "ok" -or
  $Health.data.ha_ws_connected -ne $true -or
  $Health.data.version -ne $ExpectedRelayAppVersion
) {
  throw "candidate Cloud health payload is not release-ready"
}
```

Only after a Cloud or Relay transport change or a change to
`internal-cloud-stress` or its evidence collection/validation harness, run one
stress proof on that reference platform and profile:

```bash
HOME="${SMOKE_HOME}" HA_NOVA_NO_CENSUS=1 "${CANDIDATE_BIN}" internal-cloud-stress \
  --server "${SERVER_NAME}"
```

```powershell
& $CandidateBin internal-cloud-stress --server $ServerName
if ($LASTEXITCODE -ne 0) { throw "candidate Cloud stress proof failed" }
```

Cleanup: after the last Cloud command — including after a failed smoke —
remove the smoke profile with `--yes` (without it the command asks for
confirmation, defaults to No, and exits before revoking anything when no TTY
is attached). This revokes the device authorization and deletes the keyring
entries:

```bash
HOME="${SMOKE_HOME}" HA_NOVA_NO_CENSUS=1 "${CANDIDATE_BIN}" cloud remove \
  --server "${SERVER_NAME}" --yes
```

```powershell
& $CandidateBin cloud remove --server $ServerName --yes
if ($LASTEXITCODE -ne 0) { throw "smoke profile cleanup failed" }
```

`internal-cloud-stress` resolves Cloud once and sends exactly 10,000 read-only
authenticated `/health` requests through that process-local Ingress session.
It is bounded to 20 seconds per request and one hour overall, fails on the first
invalid response, and prints neither response bodies nor credentials. It does
not run Census or update checks. A pass from a developer build, a different
commit, or a command restarted after failure is not release evidence.

The checked-out `HEAD` must equal `GITHUB_SHA`; `commit_sha` and `tree_sha` must
exactly identify the evidence commit and its full Git tree. The evidence commit
is fetched by exact SHA when it is no longer in the local clone. Evidence may
differ from the target commit only when the full target tree is identical or
through the ancestor-bound safe `uses:` normalization or the ancestor-bound
non-sensitive source delta described above. Every other earlier evidence is
rejected, including for an
apparently metadata-only activation: `nova/version.json` is copied into the App
and directly controls its Cloud runtime, so evidence from the disabled source
cannot attest the enabled runtime. The activation PR must reach a stable head,
but in `pull_request` CI that checked-out head is GitHub's synthetic
`refs/pull/<number>/merge` commit, not the PR branch head. Product, release
metadata, or sensitive workflow changes require a new exact-target evidence
envelope and every qualification row invalidated by that delta, followed by a
repository-secret update and a CI rerun without changing the PR or its base.
If the merge commit changes, rebuild the exact-target envelope; repeat only
the qualification rows invalidated by the new delta. A target containing only
the narrowly verified existing non-sensitive `uses:` version changes, or
only the non-sensitive source delta defined above, may reuse evidence from
its exact ancestor instead. If a merge queue is used,
`merge_group` creates another synthetic checkout commit and follows the same
rule.

After squash merge, the resulting `main` commit has a different SHA but may
reuse the reviewed PR evidence only while its complete Git tree is identical
to the tested synthetic merge tree. This exact-tree bridge is the sole
commit-only rewrite exception. Every tree delta still requires fresh evidence,
except for the narrowly verified ancestor-bound safe `uses:` version changes
and the ancestor-bound non-sensitive source delta defined above.
This also closes direct-to-main App-source and unknown-path bypasses.

`relay_app.source_commit` and `relay_app.source_tree_sha` must repeat the
top-level identity, and the evidence App version must equal its
`nova/config.yaml`. The current App has no
prebuilt `image` in `nova/config.yaml`, so Supervisor builds it locally from
source and there is no published App artifact hash to attest. The verifier
validates the JSON without printing it.

While Cloud remote is enabled, `min_relay_version` must equal the App version
in `nova/config.yaml`, and that version must be newer than the pre-Cloud
`0.7.1` App. Both release workflows build Darwin binaries only on macOS and
sign them with the publisher's Developer ID Application identity. The
protected `production` environment provides
`HA_NOVA_MACOS_CERTIFICATE_P12_BASE64` and
`HA_NOVA_MACOS_CERTIFICATE_PASSWORD`; neither value may appear in source,
artifacts, logs, or a non-signing job. The signing script removes both
variables before compilation, uses an ephemeral keychain, and verifies the
fixed Team ID, executable identifier, and hardened-runtime flags before upload.
GoReleaser builds only Linux and Windows, so it cannot replace the signed
Darwin artifacts. macOS bundle smoke verifies the signature again and compares
the bundled executable byte-for-byte with its raw release asset.

Provision both protected secrets from the existing encrypted Developer ID
`.p12` without importing or exporting anything through the login Keychain:

```bash
bash scripts/release/provision-macos-signing-secrets.sh /absolute/path/developer-id.p12
```

The command verifies the active GitHub account, protected environment, private
key, fixed Developer ID identity, and Team ID before upload. It requests the
`.p12` password once through concealed standard input, never writes a decoded
key or password file, and sends both values directly to GitHub's encrypted
environment-secret API. Never run it with shell tracing enabled.

At runtime, release Cloud support also requires an exact installed bundle. The
linker and `bundle.json` versions must match exactly as `X.Y.Z` or
`X.Y.Z-rcN`; `version.json.skill_version` must match their `X.Y.Z` base.
Snapshots and every other suffix fail closed. Ordinary `go build` output is
compile-time disabled. Official release builds require the
`cloudremote_official` build tag and a bundle evidence signature that binds the
exact binary SHA-256, version, OS, architecture, binary name, enabled platform
list, and the exact current release source-tree SHA. This bundle provenance is
never normalized to an ancestor. Either guard missing or mismatched disables
Cloud Remote.

The Ed25519 public key is compiled into the client; the private key must exist
only as the protected production-environment secret
`HA_NOVA_CLOUD_RELEASE_SIGNING_KEY_PEM`. Key provisioning or rotation is a
security-sensitive reviewed source change: generate the key offline, store
only its public half in source, install the private PEM in the production
environment, build a non-public RC, verify every platform, then retire the old
private key. Never commit or log a private key. The provisioned public key
matches the protected production secret through a committed non-secret
verification signature. Merely flipping mutable metadata still cannot activate
production Cloud support: exact source evidence, official build identity,
signed bundle provenance, and every platform gate remain mandatory.

The manual RC workflow requires an exact `vX.Y.Z-rcN` input and builds that
exact linker version with the official tag before bundling. It never uses a
GoReleaser snapshot. Each bundle smoke derives its own platform from the
runner. A platform listed in `cloud_remote_platforms` must pass
`internal-cloud-release-check`; every disabled or unlisted platform must fail
that check. Because enabled metadata requires a non-empty platform list and
the matrix covers every supported platform, at least one enabled-platform
provenance check must pass.

The tagged release workflow repeats this check against the exact downloadable
draft bundle asset on Linux, macOS, and Windows after upload and before the
draft can be published. Publication depends on all three jobs: listed platforms
must pass provenance and unlisted platforms must remain fail-closed.

**Rehearsal steps.** Keep one immutable `release_sha`: the reviewed merge SHA
from `main`. No local-only delta may enter any deploy or tag. Steps 1–2 precede
the RC; steps 3–4 are the RC itself when required; step 5 applies when the
release changes the census Worker.

1. Merge the reviewed PR state and record its exact remote merge commit as
   `<reviewed-merge-sha>`. If the release bumps the Relay version, first wait
   for the `relay-image.yml` **push** run on that exact SHA, then prove that the
   `latest`, immutable version, and commit tags resolve to the same published
   manifest:
   ```bash
   bash scripts/release/verify-relay-image.sh <reviewed-merge-sha> <relay-version>
   ```
   This gate must pass before an RC tag. A green workflow on another commit, or
   the existence of only some of the three GHCR tags, is not release evidence.
2. On that same fully reviewed merge commit, verify the pipeline contract is
   intact. Run this as a maintainer (admin `gh auth`) so the no-App-bypass guard
   is verified — strict mode fails closed if the token cannot read the ruleset's
   bypass actors:
   ```bash
   HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS=1 bash scripts/release/verify-release-pipeline.sh
   ```
   Then dispatch the live disposable-HA e2e from `main`, select a dispatched run
   whose `headSha` equals `<reviewed-merge-sha>`, and wait for that run to turn
   green. Re-dispatch if `main` moved before GitHub accepted the run. The weekly
   schedule alone is NOT release evidence (the v0.14.0 release shipped before
   the workflow had ever fired):
   ```bash
   gh workflow run e2e-disposable-ha.yml --ref main
   gh run list --workflow e2e-disposable-ha.yml --event workflow_dispatch --commit <reviewed-merge-sha>
   gh run watch <exact-e2e-run-id> --exit-status
   gh run view <exact-e2e-run-id> --json headSha,conclusion
   ```
   A local `bash scripts/e2e/disposable-ha/run.sh` pass is useful additional
   evidence, but does not replace the dispatched run.
   It boots a real Home Assistant plus the relay built from the commit and
   asserts the live guarantees (auth, version report, health semantics, real
   REST/WS data, bounded event windows, `/files` off by default AND the
   readwrite roundtrip on a mounted config).
3. While the production census Worker is still the old reviewed deployment,
   push the rehearsal tag on that exact commit (maintainer bypass). Do not
   deploy a release-bound census Worker before the RC has exercised the old
   endpoint:
   ```bash
   git tag vX.Y.Z-rcN <reviewed-merge-sha>
   git push origin vX.Y.Z-rcN
   ```
4. Wait for `release.yml` to finish green: it runs verify + GoReleaser
   (auto-marked prerelease via `prerelease: auto`) + install bundles + the
   three-runner public-install smoke. Verify the published RC over the real
   public install path (see "Supported RC selection" below), including at least
   one real Windows 11 + PowerShell onboarding proof on a clean VM/snapshot.
5. Only after the RC is clean, deploy a release-bound Census Worker from a
   clean checkout of the exact reviewed merge SHA. Before deployment,
   Cloudflare Access must already protect `/stats*`, the Worker must have
   `ACCESS_TEAM_DOMAIN` and `ACCESS_AUD`, and the release shell must provide
   the Access service-token credentials documented in `census-worker/README.md`.
   Treat the Census deploy as a serialized single-writer operation: do not
   deploy the Worker from another shell, workflow, or the Cloudflare dashboard
   while this wrapper runs. When Wrangler identifies this run's deployed
   version, cleanup refuses to overwrite a different active version. Cleanup
   also waits through a bounded settlement window before treating a failed
   deploy as unchanged.
   A maintainer must also complete a fresh browser login to `/stats` and only
   then set `HA_NOVA_CENSUS_BROWSER_ACCESS_VERIFIED=1` in that release shell.
   The fail-closed wrapper requires Node.js 22 or newer, a clean exact-SHA
   checkout, and `gh` authenticated to `github.com`; it proves the SHA is in
   the hard-pinned `markusleben/ha-nova` main history, exercises real
   Worker/SQLite deduplication and withdrawal locally, pins Wrangler 4.113.0
   plus the production account/config/name, and attests the deployed
   Cloudflare version READ-ONLY. A post-deploy verification failure
   automatically restores the previously active 100-percent Worker version:
   ```bash
   bash scripts/release/deploy-census-worker.sh <reviewed-merge-sha>
   ```
   The production verifier reads private `/stats/api` through Cloudflare
   Access and checks the required SHA/version headers and the schema-2 stats
   contract. Production isolation (#446): it never sends a production ping or
   withdrawal — production statistics represent voluntary real participants
   only, enforced statically by
   `scripts/test/check-census-production-isolation.mjs`. The functional
   ping/deduplication/withdrawal proof runs against the ISOLATED test Worker
   and runs BEFORE production promotion — if a mutation route broke, the
   test Worker must catch it while production still has its previous version
   and armed rollback: first
   `(cd census-worker && npx wrangler@4.113.0 deploy --env test --tag <reviewed-sha>)`, then
   `bash scripts/release/verify-census-functional.sh <reviewed-sha> <test-version-id>`
   (it attests the test deployment's SHA/version headers, so a stale test
   Worker can never green-light broken mutation routes), and only after both
   succeed run `deploy-census-worker.sh`. One-time prerequisite:
   a Cloudflare Access application for the test hostname with the same
   service-token policy, plus `ACCESS_TEAM_DOMAIN`/`ACCESS_AUD` secrets set
   via `wrangler secret put … --env test` (secrets are per-worker; without
   them the fail-closed Worker answers 403).

   Machine prerequisite, easy to miss on a fresh setup: the deploy scripts set
   `CLOUDFLARE_ACCOUNT_ID` but never `CLOUDFLARE_API_TOKEN`. Unless the shell
   already exports that token (it takes precedence), Wrangler falls back to its
   own stored OAuth session — so on a machine that has never run it, do
   `npx wrangler@4.113.0 login` first. Without either, the run fails early at
   the `secret list` step rather than at the deploy, which makes the cause look
   unrelated. Confirm with `npx wrangler@4.113.0 whoami` before starting.
6. Only after the rehearsal and every applicable external gate are clean — or
   the RC was skipped per the conditional gate above (skills/docs-only delta) —
   cut the final tag (see "Final Publish"). A skipped RC never skips the
   reviewed merge-SHA lock in step 1, step 2, or the post-publish public-install
   verification.

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
- one green `e2e-disposable-ha.yml` run (dispatched, not the weekly cron) on the commit being tagged

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
- bullet order expresses importance: the compact update notice (check-update,
  relay nudges, session start) surfaces only the FIRST bullets — one
  action-needed item plus two feature/fix items — so lead every section with
  what users must see
- only recognized sections feed the compact update notice: `Breaking Changes`,
  `What To Watch`, `Upgrade Notes` (action needed), `New Features`, and
  `Bug Fixes`; bullets under any other heading never surface there
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

For a final stable release — after the tag-first rehearsal above is clean, or
after the RC was skipped per the conditional gate (skills/docs-only delta;
steps 1–2 of the rehearsal completed, including every applicable external
gate):

1. confirm the exact reviewed remote merge commit is still `release_sha` and
   every applicable Relay/census external gate above passed
2. as a maintainer, tag that exact reviewed remote merge commit — the same
   commit the `-rcN` rehearsal validated, or, on the no-RC path, the commit
   the strict audit + dispatched e2e ran against — and push it
   (`git push origin vX.Y.Z`); the ruleset blocks the Actions token, so the
   tag is maintainer-pushed
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
