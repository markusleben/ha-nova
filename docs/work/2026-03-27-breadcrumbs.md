# Breadcrumbs

Current breadcrumb notes only.

Historical breadcrumb log:
- `docs/archive/breadcrumbs.md`

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
- Re-run the focused skill/scenario contract suites and tighten any remaining wording mismatches
- Run the new review-live harness against the 3 core standalone review scenarios and tune prompt wording only if the harness exposes output instability
- Use the `R-18` repro runbook the next time real-HA verification is needed instead of adding more product-side runtime logic first

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
