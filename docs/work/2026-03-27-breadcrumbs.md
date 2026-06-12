# Breadcrumbs

Current breadcrumb notes only.

Historical breadcrumb log:
- `docs/archive/breadcrumbs.md`

## 2026-03-27: Stable Install Contract

### Completed
- Re-reviewed the stable install path end-to-end and confirmed the current public contract was still mixed: `main` bootstrap plus latest-release payload
- Chose the fully release-pinned stable model as the next product contract
- Wrote a small work spec so the change stays focused on public docs, release-note generation, and onboarding contracts
- Promoted the new stable-install message from low-contrast prose into a short 3-step callout so the canonical user action is obvious in README and the per-client install overlays

### Next
- Remove public stable one-liners from `main` docs and replace them with a link to the latest GitHub release
- Update the stable release-note template to publish tag-pinned installer commands plus matching `HA_NOVA_VERSION`
- Tighten onboarding contract tests so stable public surfaces fail if they advertise `main/install.sh` or `main/install.ps1`

## 2026-04-08: Release-Note Comparison

### Completed
- Reworked the release-note comparison plan after a six-agent gap review
- Locked the compare algorithm to published GitHub releases plus exact SHAs instead of draft releases or release-branch names
- Classified the current `v0.3.2..ea8a708` delta by user-facing behavior slices instead of by the squash commit headline
- Prepared a release-note comparison artifact with an analysis matrix, exclusion audit, and a provisional draft

### Next
- Re-run the same comparison against the exact publish SHA once the next RC or stable release commit exists
- Fill the install block placeholder with the real release tag only during release-cut work

## 2026-04-09: README Positioning Refresh

### Completed
- Decided to separate the “why use HA NOVA” story from the lower-level implementation explanation
- Reframed the Relay as the product foundation for trust, local control, and future growth
- Reframed markdown skills as product leverage and community extensibility, not just a technical curiosity
- Prepared a dedicated next-release notes draft from the current `v0.3.2..ea8a708` comparison
- Ran a second README pass using multiple writing-focused subagents for positioning, accessibility, community pull, and MCP differentiation
- Moved user value and differentiation ahead of Quick Start, compressed install into a clearer later section, and made the MCP contrast more decisive without sounding hostile
- Simplified the README wording again so it uses easier English without sounding childish or vague
- Added the missing Relay story: not just the trust boundary today, but also the small base where future HA-specific helpers can live without turning HA NOVA into a big MCP server

### Next
- Re-check the README after the next visible feature train so the promise stays aligned with shipped capability

## 2026-04-17: Project Familiarization

### Completed
- Rebuilt the current project model with six parallel sub-analyses across docs, relay, CLI, skills, verification, and release/packaging.
- Confirmed the core architecture is still: Go CLI + TypeScript relay + markdown skill system, with the relay intentionally dumb and the skills intentionally smart.
- Confirmed the active relay surface is still Phase 1a only: `GET /health`, `POST /ws`, `POST /core`.
- Confirmed the skill system remains the main product surface: one context skill plus eleven sub-skills, with `write` as the only explicitly agentized mutation flow.
- Confirmed the strongest operational complexity sits in the CLI, especially Claude packaging/marketplace sync, install-source detection, and update-time client repair.
- Confirmed the verification model is contract-heavy and host-safe by default, with stronger real-world proof available through opt-in smoke/live harnesses rather than default CI.
- Ran focused Vitest verification successfully:
  - `npm run test:safe -- tests/bootstrap/runtime-start.test.ts tests/skills/ha-nova-contract.test.ts tests/onboarding/install-skills-per-client.test.ts`
- Ran `cd cli && go test ./...` and observed current workspace failures in Claude resume/setup tests caused by missing Claude release-snapshot payload expectations in the current worktree state.

### Next
- Use this familiarization model as the baseline for future work instead of re-discovering architecture per task.
- Treat current CLI test failures as branch-state context until a task explicitly targets Claude packaging/setup repair.

## 2026-03-27: Review Synthesis + R-18 Follow-Up

### Completed
- Extended standalone review docs to add `Questions to consider` plus ranked `Suggestions`
- Added the design-intent gate so unclear remove/simplify ideas downgrade into questions
- Kept post-write review compact while adding the manual trace-inspection next step for persisted `R-18`
- Added scenario coverage for question-vs-suggestion wording, suggestion ranking, and the stable empty questions section
- Extended the scenario harness schema with explicit `must_not_contain_text` assertions
- Added a small merged work spec and recorded the active defaults in `docs/choices.md`
- Ran live HA canaries with temporary scripts and automations, then cleaned them up again
- Observed that the current HA stack did not reproduce the reported `R-18` runtime failure for the tested script and automation canaries; traces showed successful evaluation of dependent variables
- Found a separate scenario-harness bug where missing final marker output crashed the harness with `final_line: unbound variable`; initialized the variable so the harness can fail cleanly instead
- Split live review validation into its own small review harness plus dedicated review scenarios instead of growing the generic scenario harness further
- Added a focused `R-18` repro runbook to keep the remaining uncertainty out of the product logic
- Confirmed the new review-live harness now fails fast with `duration_exceeded` when the external `codex exec` path stalls, instead of hanging indefinitely
- Hardened the review-live harness prompt to forbid Exa/Ref/web research and added an explicit `unexpected_external_research_detected` failure path

### Next
- Keep `#128` closed on the verified SHA unless a future exact-SHA repro proves a real product gap
- Use the `R-18` repro runbook the next time real-HA verification is needed instead of adding more product-side runtime logic first

### 2026-03-31 Status
- Verified `#128` against `origin/main` SHA `7b9fd11782751e34e35a3e79fc0fe64d1bd5c1b1`
- Added exact-SHA evidence plus focused relay trace proof and closed the issue without a new product change
- Kept the runbook active as the ongoing repro source of truth instead of reopening product-side runtime hardening

## 2026-03-31: Conservative R-19 Follow-Up

### Completed
- Added `R-19` as a conservative branch-structure reliability warning for direct `trigger.id` checks inside a terminal bare `else` after entity-state `if` / `elif` guards
- Kept `R-19` out of trigger-intent inference; the warning is anchored only to the visible branch structure
- Extended standalone-review and review-live scenario suites with flagged and safe `R-19` cases
- Added a dedicated write-review live harness so Phase 2 pre-write warning behavior is proved without changing the generic CRUD harness

### Next
- Keep future `R-19` refinements anchored to explicit branch structure unless a separate issue justifies broader intent inference

## 2026-03-26: WinGet Removal

### Completed
- Decided to remove `winget` completely from HA NOVA instead of keeping it as a secondary or future Windows lane
- Chose `install.ps1` as the only supported Windows install path and aligned the public PowerShell command to a single one-liner
- Chose to rewrite the live `v0.3.1` release messaging and withdraw the still-open upstream `winget-pkgs` submission
- Rewrote the live `v0.3.1` GitHub release notes around the single supported Windows path and removed the two `ha-nova-winget-manifest-v0.3.1` assets
- Commented on and closed `microsoft/winget-pkgs#352236`, then deleted the submission branch `ha-nova-0.3.1`
- Folded the `install.ps1` contract assertions into `tests/onboarding/client-install-docs-contract.test.ts` because the standalone rewritten `windows-installer-contract` path hit a reproducible local `EPERM`
- Narrowed the remaining Windows package guard to private/test legacy remnants only and moved quiet-download progress suppression fully inside `install.ps1`
- Removed stale active `winget` guidance from `skills/ha-nova/update-guide.md` and trimmed superseded `winget` choices out of the active `docs/choices.md` file
- Added an explicit manifest-label guardrail to `AGENTS.md`, `docs/releasing.md`, and the onboarding contract so manifest-touching PRs add `manifest-review:approved` before `@codex`
- After Codex review on PR #138, restored cleanup of old private/test Windows package residue during bundle uninstall so reinstall is not blocked by stale `%LOCALAPPDATA%\\Microsoft\\WinGet\\...` leftovers

### Next
- Prepare the cleanup as a focused PR once the remaining unrelated worktree changes are separated

## 2026-03-26: Documentation Taxonomy Cleanup

### Completed
- Moved the long breadcrumb ledger into `docs/archive/breadcrumbs.md`
- Archived the old superpowers tree under `docs/archive/superpowers/`
- Added `docs/work/` as the canonical path for temporary active work docs
- Aligned the PR template to the canonical `npm run verify` gate

### Next
- Repair archive-internal links where they still point to pre-move paths
- Decide whether `PROJECT.md` should link to the governance map even more aggressively

## 2026-03-26: Claude Shipped Marketplace Direction

### Completed
- Decided that shipped Claude installs must use the release payload on disk, not the floating GitHub marketplace source
- Kept update discovery with HA NOVA itself (`check-update`, `doctor`, relay health, SessionStart)
- Chose `ha-nova update` + Claude restart as the canonical user action after update notices

### Next
- Flip the Claude runtime default for shipped installs to local staged marketplace roots
- Add atomic staging + validation coverage before marketplace cutover
- Align Claude install docs and release docs with the new release-pinned contract

## 2026-03-26: Verify Gate Security Audit

### Completed
- Added a canonical `verify-npm-audit.sh` helper for root + `nova/` production lockfile audits
- Folded that audit into `npm run verify`
- Added the same audit step to regular PR/main CI
- Updated release/contributor docs and contract tests to treat the audit as part of the standard host-safe gate

### Next
- Keep future dependency-policy changes aligned with the same root + `nova/` verify contract

## 2026-03-27: PR #142 Review Follow-Up

### Completed
- Rechecked PR #142 on the latest branch SHA instead of assuming the earlier Codex result still applied
- Confirmed the old R-18 scenario findings were already fixed locally and pushed on `8c1fe36`
- Hardened the review-live harness schema so `max_duration_sec` must be a positive integer and added contract coverage for that rule

### Next
- Run the targeted review-live contract suite
- If green, commit the follow-up fix, push, and trigger `@codex` again for the new SHA

## 2026-03-27: PR #142 Review Follow-Up 2

### Completed
- Fixed two new Codex findings in the review-live harness by treating shell-based network fetch commands as forbidden external research
- Added an explicit transcript guard for onboarding `ready` / `doctor` / `quick` checks so the hard prompt rule is enforced in validation, not just wording
- Re-ran the focused review-live contract suite and shell syntax check successfully

### Next
- Commit the follow-up harness fix
- Push the new SHA
- Trigger `@codex` again and wait for a fresh clean result on that exact commit

## 2026-03-27: PR #142 Review Follow-Up 3

### Completed
- Fixed the new review-live finding by blocking Home Assistant read commands unless a scenario prompt explicitly opts in
- Fixed the generic scenario harness so `must_not_contain_text` validation cannot overwrite an earlier failure code
- Re-ran both affected e2e contract suites and both shell syntax checks successfully

### Next
- Commit the follow-up harness fixes
- Push the new SHA
- Trigger `@codex` again and wait for a clean current review on that exact commit

## 2026-03-27: Post-Merge Follow-Up for PR #142

### Completed
- Confirmed that a new Codex inline finding landed after the merge for PR #142 on SHA `c94e36f`
- Identified the process mistake: merge happened after `codex-review-gate` turned green but before the SHA-specific bot review objects had visibly settled in GitHub
- Patched the review-live harness to detect multiline Python network calls in `command_execution` payloads by checking interpreter and network-library tokens independently
- Added regression coverage so the contract now rejects the old same-line-only detector shape

### Next
- Run the targeted review-live contract suite
- Commit the post-merge follow-up fix on a new branch
- Open a small follow-up PR and wait for a fresh clean Codex result before merging

## 2026-04-14: Claude Release Snapshot Implementation

### Completed
- Replaced the Claude production default with an exact versioned local release snapshot under `~/.config/ha-nova/claude-marketplace/releases/vX.Y.Z`
- Kept the flat local marketplace root for dev / explicit override only
- Removed the bundle-path fallback to floating GitHub; missing or incomplete Claude payload now fails loudly
- Tightened Claude attach truth so healthy state requires:
  - marketplace record present
  - plugin record present
  - usable `installPath`
  - expected marketplace source match
- Added regression coverage for:
  - bundle install -> versioned local snapshot
  - legacy GitHub source migration -> versioned local snapshot
  - missing marketplace repair on same-version update
  - old flat local root no longer counting as healthy
  - bundle payload missing -> loud failure
- Proved the behavior on macOS both in:
  - an isolated temp-`HOME`
  - the real user profile after backup + deliberate marketplace removal

### Verification
- `cd cli && go test ./... -run 'TestInstallClaudePlugin|TestPostUpdateSync|TestClientAppearsInstalledForClaude|TestRunDoctor'`
- `npx vitest run tests/onboarding/install-skills-per-client.test.ts tests/onboarding/dev-sync-behavior.test.ts tests/onboarding/desktop-validation-contract.test.ts tests/onboarding/client-install-docs-contract.test.ts tests/onboarding/dev-sync-contract.test.ts`
- `git diff --check`
- Real macOS checks:
  - `go run . doctor`
  - `claude plugin list`
  - `claude plugin marketplace list`
  - deliberate marketplace removal -> same-version `update --version 0.4.1` repair back to the versioned local snapshot

### Next
- Open a focused PR for the Claude release-snapshot fix
- Wait for CI + Codex on the exact PR SHA
- Merge and ship as a small patch release so all users get the same production attach model

## 2026-04-15

### Release Prep
- Started isolated `v0.4.2` release prep in fresh clone `/tmp/ha-nova-v042-prep` because the main workspace is dirty and behind/ahead of `origin/main`.
- Confirmed release delta from `v0.4.1..main` is only the merged Claude release-snapshot fix in `#173`.
- Wrote minimal release prep spec:
  - `docs/work/2026-04-15-v0.4.2-release-prep-spec.md`
- Drafted release body:
  - `docs/work/2026-04-15-v0.4.2-release-body.md`
- Bumped release metadata to `0.4.2` in:
  - `version.json`
  - `package.json`
  - `package-lock.json`
  - `.claude-plugin/plugin.json`
  - `.claude-plugin/marketplace.json`
- Verified release prep locally:
  - `npm run verify:next-release-version -- v0.4.2`
  - `npm run verify:release-metadata`
  - `npm run verify:release-contracts`
  - `npx vitest run tests/onboarding/bump-version.test.ts tests/onboarding/release-contract.test.ts`
  - `cd cli && go test ./... -run 'TestInstallClaudePlugin|TestPostUpdateSync|TestClientAppearsInstalledForClaude|TestRunDoctor'`
  - `git diff --check`
## 2026-04-18: Linux Secure Storage Recovery

### Completed
- Reproduced the real Linux SSH setup failure on `markus@192.168.1.8` and proved the root cause was not a plain unlock failure but a missing Secret Service `default` collection alias on a fresh GNOME Keyring state
- Proved live over SSH that GNOME Keyring's internal DBus methods can create the default collection headlessly (`CreateWithMasterPassword` + `SetAlias("default", ...)`) and can also unlock an existing locked collection (`UnlockWithMasterPassword`)
- Reworked the Linux CLI path to distinguish locked-vs-uninitialized secure storage, tightened GNOME owner trust checks, and moved recovery from shell exec to DBus
- Reworked setup UX copy so fresh-init asks the user to create a new local Linux keyring password, while locked recovery asks for the existing one
- Tightened Linux preflight to require a real write/read/delete probe instead of trusting alias state alone
- Added and updated Go coverage for the new Linux recovery actions, state classification, and setup/persistence UX

### Next
- Keep the Linux secure-storage slice in review until the remaining cleanup/test findings are closed

## 2026-04-09: README Positioning Tightening

### Completed
- Tightened the README opening so the Relay case lands once up top instead of repeating the same trust/growth message across multiple sections
- Kept the Relay explanation focused on three strong points: server-side trust, hard local work, and room for future HA-specific helpers
- Simplified the split explanation and reduced repeated “plain markdown skills” / “small Relay” language in later sections
- Recorded the new README defaults in the work spec and choices log
- Removed direct opening repetition where two back-to-back sentences both started with `HA NOVA`
- Replaced several remaining vague or awkward README lines with simpler, more direct wording across the opening, benefits, MCP comparison, and contribution close

### Next
- Re-read the full README once more for any remaining repetition outside the opening sections if more polishing is needed

## 2026-04-12: Claude Agent Port

### Completed
- Found the local Claude source agent at `/Users/markus/.claude/agents/doc-review-orchestrator.md`
- Read its companion memory file at `/Users/markus/.claude/agent-memory/doc-review-orchestrator/MEMORY.md`
- Ported the reusable review method into a local Codex skill at `/Users/markus/.agents/skills/doc-review-orchestrator/SKILL.md`
- Captured the port scope and defaults in a work spec

### Next
- Start a fresh Codex session when convenient so the new local skill can be discovered in the standard skill list

## 2026-04-12: Release Note Refresh

### Completed
- Refreshed the release-note comparison from `v0.3.2..ea8a708` to `v0.3.2..b5e94aa`
- Classified the three post-feature commits as docs-only follow-up work
- Confirmed they do not create new public release-note bullets
- Kept the next-release draft unchanged because the user-facing product delta is still the promoted skills plus Gemini auto-discovery

### Next
- Replace `<RELEASE_TAG>` in the draft once the actual release tag is chosen

## 2026-04-13: Dependabot Release Preflight

### Completed
- Audited all open Dependabot PRs with `gh`
- Confirmed current open set is `#166`, `#165`, `#164`, `#163`, `#156`
- Verified root production deps still fail `npm audit --omit=dev` on critical `axios` advisories
- Verified `nova/` production deps also fail `npm audit --omit=dev` on the same critical `axios` advisories
- Classified the two `axios` bumps as release-lane candidates and the workflow/dev-dependency bumps as separate-later maintenance

### Next
- Either merge reviewed `axios` fixes through the normal PR path or carry equivalent fixes in a reviewed release-prep PR before calling the next tag release-ready

## 2026-04-13: Combined Axios Release-Prep PR

### Completed
- Confirmed `#165` and `#164` could not pass CI independently because each PR left the other critical production `axios` audit finding in place
- Built a combined branch from `origin/main`: `build/release-prep-axios-1-15-0`
- Committed the bundled root + `nova` `axios` bumps at `ff2248e`
- Verified locally with `npm audit --omit=dev --json` in root and `nova/`, `npm run verify:security`, and `npm run test:safe:core`
- Opened PR `#167` with the combined fix, added `manifest-review:approved`, and triggered `@codex`
- Confirmed Codex bot already returned a `+1` reaction on `#167`
- Merged `#167` via squash as remote commit `1132ce7` after all required checks were green and the current PR SHA had a clean Codex result
- Closed superseded Dependabot PRs `#165` and `#164`

### Next
- Continue release prep from `origin/main` at `1132ce7`
- Re-evaluate which remaining open Dependabot PRs are intentionally deferred (`#166`, `#163`, `#156`)

## 2026-04-13: Post-Axios Release Refresh

### Completed
- Refreshed the release compare target from the older provisional `b5e94aa` snapshot to `origin/main` at `1132ce7`
- Reclassified the merged `axios` fix as a selective `Bug Fixes` candidate for the next release notes
- Updated the release-note comparison artifact and the next-release draft to include the `axios` security fix
- Rechecked the remaining open Dependabot PRs and kept `#166`, `#163`, and `#156` in the deferred lane

### Next
- Continue release prep from `1132ce7`
- Keep the remaining open Dependabot PRs out of the release lane unless a new live release-path problem appears

## 2026-04-13: Release Prep 0.4.0

### Completed
- Chose `0.4.0` as the next stable version
- Verified `v0.4.0` is newer than the latest published stable release
- Created a clean release-prep branch from `origin/main`: `build/release-prep-0-4-0`
- Bumped release metadata to `0.4.0` in `version.json`, `package.json`, `package-lock.json`, `.claude-plugin/plugin.json`, and `.claude-plugin/marketplace.json`
- Verified locally with `npm run verify:release-metadata`, `npm run verify:release-contracts`, targeted bump/release Vitest suites, and the full pre-push hook
- Opened PR `#168`, added `manifest-review:approved`, and triggered `@codex`
- Confirmed `#168` received a clean Codex `+1` and green CI on SHA `07d503c`
- Merged `#168` via squash as remote commit `d1de3f1`
- Tagged the reviewed remote merge commit as `v0.4.0`
- Let `release.yml` publish the release successfully
- Replaced the generated GitHub release text with the final user-facing body from `docs/work/2026-04-13-v0.4.0-release-body.md`

### Next
- If needed, do a short post-release sanity pass on the published installers and update path

## 2026-04-13: Post-Release Sanity

### Completed
- Confirmed GitHub release `v0.4.0` is published with the intended final user-facing release notes
- Confirmed release workflow `24336286371` finished successfully
- Confirmed the release job plus all three post-publish smoke installer jobs (`ubuntu`, `macOS`, `Windows`) completed successfully
- Confirmed the public raw installer scripts at tag `v0.4.0` resolve the latest release path by default and support explicit `HA_NOVA_VERSION` pinning
- Confirmed the tagged `version.json` reports `0.4.0`
- Confirmed the release assets and selected bundle checksum sidecars are present on GitHub
- Confirmed `checksums.txt` covers the raw CLI binaries while the installer bundles ship separate `.sha256` sidecars

### Next
- Remaining optional work is follow-up housekeeping only; no immediate release issue was found

## 2026-04-13: Claude Availability Troubleshooting

### Completed
- Confirmed the local Mac still has HA NOVA `0.3.2` installed
- Confirmed the local installed HA NOVA skill tree does not include the new promoted skills from `v0.4.0`
- Confirmed HA NOVA's own local state still says Claude is installed in plugin mode
- Confirmed Claude itself does not currently list the `ha-nova` marketplace or the `ha-nova@ha-nova` plugin in `~/.claude/plugins/*` or `claude plugin list`
- Confirmed the staged local Claude marketplace files still exist under `~/.config/ha-nova/claude-marketplace`
- Determined the immediate issue is plugin-state drift: Claude is not loading the old HA NOVA install, so it cannot show the expected in-product update notice

### Next
- Explain the drift clearly to the user
- If wanted, repair the local Claude registration and update the install to `v0.4.0`

## 2026-04-13: Claude Drift Fix

### Completed
- Ran six review lanes across install/setup, doctor/status, update sync, uninstall/reset, regression coverage, and user-impact surface
- Confirmed the strongest repo-controlled weakness was overly loose Claude success verification after install
- Confirmed Claude plugin detection incorrectly treated blank `installPath` values as installed
- Tightened the Go Claude install path so success now requires both marketplace registration and plugin presence after sync
- Tightened Claude plugin detection so blank `installPath` no longer counts as installed
- Added targeted regressions for marketplace disappearance after install and blank `installPath`
- Verified with targeted Go tests covering doctor hints, update sync, retry behavior, stale-state handling, and the new Claude regressions

### Next
- If needed, repair the local Mac Claude registration with `ha-nova setup claude` and then update to `v0.4.0`

## 2026-04-13: Claude Drift KISS Refinement

### Completed
- Ran a second six-lane refinement pass focused on minimality, detach semantics, likely trigger ranking, and regression scope
- Kept `update` non-pruning after review convergence; no hidden state-clearing behavior was added
- Hardened the Claude fallback `update -> install` branch so verifier failures now flow through rollback
- Tightened Claude attachment truth again so unparseable plugin registry data no longer counts as installed
- Added regressions for unparseable Claude registry data and fallback-install marketplace disappearance
- Re-ran targeted Claude doctor/install/update regression tests successfully

### Next
- If this still detaches in the wild after the stricter verifier, the next step should be evidence capture around the real trigger, not a bigger architecture layer

## 2026-04-13: Claude Real-Machine Repro

### Completed
- Snapshotted live local state before mutation under `/tmp/ha-nova-claude-snapshot-LIU4bi`
- Built a local test binary from merged `origin/main` instead of trusting the installed `0.3.2` binary
- Reproduced a controlled detached-Claude state by removing only the `ha-nova@ha-nova` plugin record from `~/.claude/plugins/installed_plugins.json`
- Confirmed the stricter attach fix behaves correctly on the real Mac:
  - `doctor` warned that Claude was configured but not attached
  - `setup claude` repaired the state cleanly
- Reproduced a second drift case by removing only the `ha-nova` marketplace entry from `~/.claude/plugins/known_marketplaces.json`
- Confirmed a new real-machine gap: `doctor` stayed green in the marketplace-missing case even though `setup claude` still had to repair Claude

### Next
- Patch `doctor`/status to require both the Claude plugin and the Claude marketplace record for a healthy attach state

## 2026-04-13: Claude Doctor Marketplace Fix

### Completed
- Created a clean clone from `origin/main` after detecting the first test worktree was polluted by unrelated local deltas
- Patched Claude client status so `doctor` now treats Claude as attached only when both the plugin and marketplace record exist
- Added a regression test for the missing-marketplace drift case
- Verified targeted Go regressions on the clean clone
- Re-ran the marketplace-missing real-machine test with the clean-clone binary:
  - `doctor` now correctly warns on marketplace-only drift
  - `setup claude` repairs the state
  - final `doctor` returns to healthy
- Opened PR `#170` (`fix: detect missing Claude marketplace in doctor`) and triggered `@codex`

### Next
- Wait for PR `#170` CI + Codex on the exact pushed SHA, then merge if clean

## 2026-04-14: Claude Drift Evidence Capture

### Completed
- Merged PR `#170` first so the doctor/attach truth fix stayed isolated from follow-up forensics work
- Created a separate follow-up branch in the clean clone for Claude drift evidence capture
- Ran six focused review lanes across evidence quality, JSON/file-format risk, watcher KISS, test gaps, debugging value, and docs/scope
- Built a compact `snapshot-home` / `write-watch-event` extension into `scripts/dev/claude-plugin-state.mjs`
- Added a new local watcher wrapper at `scripts/dev/watch-claude-state.sh`
- Hardened the watcher/helper after review:
  - BOM-prefixed JSON parses cleanly
  - invalid Claude JSON records parseability instead of killing the watcher
  - foreign plugins / foreign marketplaces no longer false-positive as HA NOVA
  - `attached` now requires a usable HA NOVA install path plus marketplace source
  - watcher output includes a small per-event `changes` diff block
  - launchd-safe command lookup now falls back to Homebrew paths for `node` / `fswatch`
- Added targeted helper regressions for:
  - full attached snapshot
  - watch-event output
  - foreign plugin false-positive prevention
  - blank install path
  - BOM on installed plugins
  - BOM on known marketplaces
  - invalid known marketplaces JSON
- Verified on the clean clone:
  - `npx vitest run tests/onboarding/claude-plugin-state-helper.test.ts`
  - `go test -run 'TestDoctor|Test.*Claude.*' -count=1` in `cli/`
  - `git diff --check`
- Confirmed the live watcher runs on the Mac as a `launchd` submitted job:
  - label `com.markusleben.ha-nova.claude-audit`
  - stdout `/tmp/ha-nova-claude-audit.log`
  - stderr `/tmp/ha-nova-claude-audit.err`
  - audit output `~/Library/Caches/ha-nova/claude-drift-audit/{events.jsonl,latest.json}`
- Confirmed the current live Claude state at watcher startup is attached again on the Mac:
  - `known_marketplaces.json` includes `ha-nova`
  - `settings.json` enables `ha-nova@ha-nova`
  - `installed_plugins.json` contains `ha-nova@ha-nova` with install path `.../0.3.2`

### Next
- Commit and open the small follow-up PR for the evidence-capture tooling
- Leave the watcher running until the next real detach or drift event appears
- When detach happens again, inspect `events.jsonl` to identify the first changed Claude registry file and the exact field-level diff

## 2026-04-14: Manifest Label Timing Miss

### Completed
- Opened PR `#171` for the Claude drift evidence-capture tooling
- Caught a repeated `manifest-review-gate` failure after a force-push rebased the PR onto real `github/main`
- Confirmed the root cause was label timing, not missing label presence:
  - `manifest-review:approved` was present in GitHub UI
  - the workflow invalidated the earlier label event because a newer relevant SHA landed afterward
- Re-applied the manifest label on the current PR state
- Wrote the explicit relabel rule into `AGENTS.md` and `docs/work/2026-03-27-choices.md`

### Next
- Keep treating manifest-changing PRs as `label on create` plus `relabel after any later relevant SHA`

## 2026-04-14: Claude Detach Root Cause + GitHub Marketplace Fix

### Completed
- Reproduced the detached real-machine state again on the Mac and confirmed Claude no longer had `ha-nova` in:
  - `~/.claude/plugins/known_marketplaces.json`
  - `~/.claude/plugins/installed_plugins.json`
  - `~/.claude/settings.json`
- Pulled the live drift-audit evidence and confirmed the important order:
  - `known_marketplaces.json` lost `ha-nova` first
  - then `installed_plugins.json` / `settings.json` followed
  - Claude wrote `.orphaned_at` in the HA NOVA plugin cache
- Confirmed the old attached state right before detach was using the staged local marketplace path:
  - `/Users/markus/.config/ha-nova/claude-marketplace`
- Chose the KISS product fix:
  - bundle installs now use the GitHub Claude marketplace source by default
  - local staged marketplace stays dev-only / explicit override only
- Tightened Claude attachment truth further:
  - Claude attachment now requires both plugin record and marketplace record
  - blank `installPath` no longer counts as installed
  - post-sync verification now fails if the plugin exists but the marketplace registration is gone
  - failed GitHub-source installs now roll the marketplace back instead of leaving a half-attached state behind
- Updated tests for the new production/default behavior and added regression coverage for:
  - bundle install defaulting to GitHub source
  - stale local marketplace migration to GitHub source
  - blank `installPath`
  - missing marketplace record
- Updated Claude reference docs to reflect the new production/default model
- Used the new code path on the real Mac with `go run . internal-sync-clients` from `cli/` and repaired Claude successfully:
  - `ha-nova@ha-nova` restored
  - version now `0.4.1`
  - marketplace source now `https://github.com/markusleben/ha-nova.git`
  - `ha-nova doctor` returned healthy
  - 15-second follow-up check stayed attached

### Verification
- `cd cli && go test -run 'TestInstallClaudePlugin|TestRemoveInstalledClients|TestRunDoctor|TestPostUpdateSync|TestClientAppearsInstalled' -count=1`
- `npx vitest run tests/onboarding/client-install-docs-contract.test.ts`
- `git diff --check`
- Real-machine Claude checks:
  - `claude plugin list`
  - `claude plugin marketplace list`
  - watcher audit under `~/Library/Caches/ha-nova/claude-drift-audit/`

### Next
- Open a focused PR for the GitHub-marketplace production fix
- Keep the drift watcher running to see whether the GitHub-backed source stays stable longer-term on the real Mac

## 2026-04-14

### Claude release-snapshot implementation
- Revisited the Claude production-source decision after confirming the user still needs an exact shipped-release attach, not just a more stable attach.
- Confirmed in code that bundle and repair paths still used:
  - floating/legacy GitHub migration logic in Go tests
  - the flat local root `~/.config/ha-nova/claude-marketplace` for local staging
- Implemented a new production default for bundle installs:
  - exact versioned local release snapshots under `~/.config/ha-nova/claude-marketplace/releases/vX.Y.Z`
- Kept the flat local root for repo/dev and explicit override only.
- Tightened Claude attachment truth further:
  - expected marketplace source must match
  - plugin record must exist
  - `installPath` must be usable
- Changed bundle behavior so missing/incomplete Claude payload is now a hard failure instead of a GitHub fallback.
- Updated helper parsing so dev/shell rollback can still read structured GitHub and git+ref marketplace entries.
- Added/updated regressions for:
  - bundle default -> versioned local snapshot
  - legacy GitHub migration -> versioned local snapshot
  - configured Claude repair when marketplace record is missing
  - old flat local root no longer counting as healthy
  - bundle payload missing -> loud failure
- Updated Claude docs/spec notes to reflect:
  - bundle installs use versioned local release snapshots
  - flat root stays dev-only

### Verification
- `npx vitest run tests/onboarding/install-skills-per-client.test.ts tests/onboarding/dev-sync-behavior.test.ts tests/onboarding/desktop-validation-contract.test.ts`
- `cd cli && go test ./... -run 'TestInstallClaudePluginUsesVersionedLocalMarketplaceByDefaultForInstalledBundle|TestInstallClaudePluginMigrates|TestInstallClaudePluginFailsWithout|TestPostUpdateSyncRefreshesDetectedInstalledClientsWithoutState|TestClientAppearsInstalledForClaude'`
- `cd cli && go test ./... -run 'TestInstallClaudePluginUpdatesExistingPlugin|TestRunDoctor|TestPostUpdateSyncRefreshesAllDetectedClients'`

### Real-machine proof
- Ran an isolated temp-`HOME` Claude smoke on macOS using the current repo code plus the installed bundle payload.
- Confirmed fresh `setup claude --non-interactive` attached Claude to:
  - `/tmp/.../.config/ha-nova/claude-marketplace/releases/v0.4.1`
- Confirmed on-disk proof in temp `HOME`:
  - `known_marketplaces.json` pointed to the versioned local directory
  - `installed_plugins.json` resolved to cache path `.../ha-nova/0.4.1`
- Deliberately removed the `ha-nova` marketplace entry in temp `HOME`; `go run . doctor` failed correctly and same-version `go run . update --version 0.4.1` repaired it back to the same versioned local directory.
- Backed up the real profile before mutation under:
  - `~/Library/Caches/ha-nova/manual-claude-test-20260414-191656`
- Confirmed real-profile pre-state:
  - `doctor` reported `Claude Code is not attached correctly`
  - `claude plugin list` had no `ha-nova`
  - `claude plugin marketplace list` had no `ha-nova`
- Repaired the real profile with:
  - `cd cli && go run . setup claude --non-interactive`
- Confirmed real-profile post-repair:
  - `doctor` passed
  - `claude plugin list` showed `ha-nova@ha-nova Version: 0.4.1`
  - `claude plugin marketplace list` showed `Source: Directory (/Users/markus/.config/ha-nova/claude-marketplace/releases/v0.4.1)`
- Deliberately removed the real `ha-nova` marketplace entry again; `doctor` failed correctly.
- Repaired the real profile with same-version update:
  - `cd cli && go run . update --version 0.4.1`
- Confirmed final real-profile source returned to:
  - `/Users/markus/.config/ha-nova/claude-marketplace/releases/v0.4.1`
- Opened focused PR `#173` for the Claude release-snapshot fix.
- Fixed two real Codex findings on the PR:
  - foreign plugin arrays could falsely count as HA NOVA attached
  - one architecture doc line still described the old GitHub-backed production path
- Current PR head:
  - `a1fadf56db8b74f74f23d6abfa9bfa540f6910de`
- Current GitHub state:
  - all CI checks green
  - both old Codex review threads resolved
  - latest explicit `@codex review` comments only have `eyes`, no fresh final bot result yet for `a1fadf5`
- Remaining blocker is external:
  - `mergeStateStatus: BLOCKED`
  - `reviewDecision: REVIEW_REQUIRED`
  - by repo rule, the high-risk PR is not "done" until a real current Codex result exists for the latest SHA
- Received fresh current Codex result for the latest PR head via issue comment:
  - `https://github.com/markusleben/ha-nova/pull/173#issuecomment-4250794033`
  - message: `Codex Review: Didn't find any major issues.`
- Confirmed the clean Codex result arrived after the final explicit review request for head `a1fadf56db8b74f74f23d6abfa9bfa540f6910de`.
- Merged PR `#173` with squash + admin fallback after:
  - all CI checks green
  - real current Codex result present
  - no new findings
  - all review threads resolved
- Final merge result:
  - PR state: `MERGED`
  - merge commit: `6b187d5cd3b9731f8d853f3757ae0b0389bc8e94`
  - merged at: `2026-04-15T09:25:47Z`

## 2026-04-18

- Added the setup-only Linux secure-storage recovery stage and kept it intentionally GNOME-only by checking the active Secret Service owner before offering the built-in DBus recovery path.
- Split recovery attempt tracking into initial-gate vs save-time retry so backing out of the first recovery page no longer bypasses the gate and a later persistence failure can still offer one retry.
- Tightened recovery retry semantics so fatal backend/session failures stop immediately, while password rejection stays retryable with explicit local-password wording.
- Added regressions for recovery back-navigation, saved-token read recovery, fatal-vs-retryable loop behavior, Linux owner detection/method support, Linux DBus recovery classification, and the release-contract Linux lane.
- Re-ran `cd cli && go test -count=1 -timeout 180s ./...` plus `npx vitest run tests/onboarding/release-contract.test.ts` after the fixes.

## 2026-04-21

- Reproduced the Hermes skill-loading mismatch from user evidence at the contract level: `skills_list(category="ha-nova")` exposed HA NOVA namespaced sub-skills, but our Hermes installer still wrote them into bare directories like `history/` and `review/`.
- Traced the mismatch to both Hermes installers:
  - Go installer: `cli/client_hermes.go`
  - shell installer: `scripts/onboarding/install-local-skills.sh`
- Recorded the fix direction in `docs/archive/plans/2026-04-21-hermes-skill-view-mismatch-spec.md`: Hermes sub-skill directory names must equal their rewritten `name:` values.
2026-06-10 (train port)
- Ported the local main worktree's uncommitted train (service credentials + token file storage, Claude snapshot/auto-repair hardening, atomic state writes, Gemini/Hermes rewriter absolutization, purge keyring cleanup, issue #200 fail-loud token storage) onto origin/main as a clean branch via three-way delta application.
- Deliberately dropped during the port: the local-only April 11 README conversion rewrite (superseded by origin's README evolution and the active Hermes release-claim gating), stale 0.4.0 version/manifest fields, and local variants of files where origin/main had newer reviewed versions (setup discovery tests, preflight session-bus dedup, claude-plugin-state.mjs, recovery test variants).
- Preserved origin's Hermes release-claim gating: README stays release-conservative; the .hermes/INSTALL.md release-status note is kept; the public Hermes claim flip stays reserved for the final release PR.
2026-06-12
- Release-verification pass for 0.5.0: automated minimum gate green on final main; private macOS RC suite (temp homes, local bundle server) green incl. bundle build; Linux real-machine lane executed live on the Ubuntu host over SSH with the desktop keyring genuinely locked — fresh 0.5.0 bundle install via the maintainer HA_NOVA_BUNDLE_URL override, doctor through the service token file against the real HA/Relay, desktop-return correctly fail-fast on the locked keyring without stranding service state, non-interactive `setup --service hermes` reusing the existing file token, authenticated `relay health` from a fresh shell (the original issue #200 scenario, now green), purge removing the token file with the best-effort credential-store note, and full host restore afterwards.
- Hermes README claim stays gated for 0.5.0 (user decision); release notes frame Hermes as early support/preview pointing at the validation matrix.
- Still open before final tag: Windows real-machine lane (awaiting SSH access from the user) and the RC build/prerelease publish (workflow dispatch reserved for the user; agent classifier correctly blocks public release triggers).
