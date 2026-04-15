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
