# Breadcrumbs

## 2026-03-18: Release-Day Finalization

### Completed
- Re-verified the full host-safe release gate with `npm run verify` after the last late test hardening change
- Confirmed the current branch already maps to PR `#97`
- Confirmed `v0.1.12` is already the latest published GitHub release, so today's release needs a new version/tag
- Reviewed open issues for safe auto-close linkage and identified `#91` as the clear match for this branch
- Fixed CI-only onboarding flake: fresh runners were timing out inside targeted `go build` / `go test` contract tests while modules were still downloading

### Current Release Default
- Use version `0.2.0` for the public release
- Keep the release pipeline on the simpler checksum-only lane; no extra signing work before today's ship
- Link PR `#97` to `Closes #91` and keep unrelated backlog issues open

### Next
- Bump version, commit, push, rerun PR checks, merge, tag, and watch the publish workflow

## 2026-03-14: Full-Project Familiarization + Recent-Change Pass

### Completed
- Re-read product and architecture anchors: `PROJECT.md`, `README.md`, `docs/reference/bridge-architecture.md`, `docs/reference/skill-architecture.md`, `docs/reference/ha-api-matrix.md`
- Mapped active runtime surfaces: `nova/src/` relay app, `cli/` Go relay client, `skills/` context + sub-skills, `hooks/` SessionStart bootstrap, onboarding/deploy/update scripts
- Reviewed recent mainline changes through the last 12 commits, with special focus on `2fa861f`, `51e2abc`, and `88e4435`
- Verified the current repo health after the recent CLI/release changes:
  - `npm test` → 46/46 files, 293/293 tests passed
  - `cd cli && go test ./...` → passed

### Key Understanding
- The current architecture is now a 3-part system:
  - HA App/Relay in `nova/` = thin authenticated transport (`/health`, `/ws`, `/core`)
  - Go client in `cli/` = local UX/auth wrapper around relay access (`health|ws|core|jq|version`)
  - Markdown skills in `skills/` = business logic, safety, review, and workflow
- The biggest recent shift is the replacement of the old shell relay wrapper with the cross-platform Go binary plus automated GitHub release publishing
- Skill governance and safety were tightened in parallel: namespaced skill IDs, `ha-nova-fallback` replacing `guide`, stronger review catalog/contracts, stricter onboarding/error paths

### Hotspots To Revisit First
- `cli/` for any future auth/proxy/`jq` behavior changes
- `scripts/onboarding/install-local-skills.sh`, `scripts/update.sh`, `scripts/dev-sync.sh` for client install/update drift
- `skills/ha-nova/SKILL.md` and `skills/ha-nova-fallback/SKILL.md` for cross-skill routing/safety changes
- `.goreleaser.yml` + `.github/workflows/release.yml` for anything touching release assets or installer expectations

## 2026-03-06: Helper CRUD Skill + Structural Fixes (PR #48)

### Completed
- New `ha-nova:helper` skill — CRUD for 9 storage-based helper types via WS commands (inline, no agents)
- New `skills/ha-nova/helper-schemas.md` — payload reference for all 9 types
- New `build.yaml` — multi-arch HA App builds (amd64 + aarch64)
- Review-agent rewritten: references `review/SKILL.md` as the pre-split catalog home instead of duplicating checks
- `skill-architecture.md`: agent vs inline decision rule, skill section template, new-skill checklist, post-write review standard, pre-split review check SSOT
- Dynamic Gemini skill discovery in `install-local-skills.sh` (replaces hardcoded list)
- Version 0.1.4 synced across all files; bump script now includes `config.yaml`
- Inverse scope notes on read/write skills; H-01..H-08 helper review checks; helper service patterns
- `helper` added to onboarding sub-skills array + uninstall flat-skills array
- CI `check-docs.sh` updated for 9 skill directories (guide skill added)
- Portable `sed` in bump script (Codex bot review finding)

### Architecture Decisions
- Helpers use inline execution (2-4 relay calls, flat configs) — agents only justified for write skill (5+ calls, normalization, reload)
- Historical note: before the 2026-03-12 split, review checks lived inline in `review/SKILL.md` Step 1; keep this as history, not current guidance
- HA Supervisor discovers add-ons via recursive `config.yaml` search — subdirectory layout NOT required

### Skill count: 1 context + 8 sub-skills = 9 total

## 2026-03-06: Full-Project Understanding Pass

### Completed
- Mapped repo purpose from `README.md`, `PROJECT.md`, and mandatory reference docs in `docs/reference/`
- Read relay runtime and HTTP surface in `src/` (`/health`, `/ws`, `/core`, auth, token resolution, REST/WS clients)
- Verified skill architecture: 1 auto-loaded context skill + 7 operational sub-skills under `skills/`
- Verified product packaging surfaces: Home Assistant App metadata, Claude plugin, SessionStart hook, onboarding/install scripts, CI workflows
- Verified current health locally: `npm run typecheck`, `npm test`, and `npm run build` all passed

### Key Understanding
- Current shipped relay is MVP transport: authenticated HTTP wrapper around HA WebSocket and REST, intentionally thin
- Strategic complexity lives in onboarding flows, skill contracts, and contract-heavy tests rather than in relay runtime code
- Filesystem/backups/subscriptions are designed and documented, but not implemented in the current `src/` runtime yet
- Auth model in current runtime is env/app-config LLAT + separate relay token; README/skills position client-side use around local relay CLI + Keychain-backed token storage

## 2026-03-05: Test Scenarios + Installer + CLI Flags

### Completed
- 11 test scenarios (S-1 to S-11) all automated with fixture-based mocks
- Shared test helpers: `tests/onboarding/_helpers.ts` (createMockHome, createMockBinaries, mockEnv)
- 6 fixture files in `tests/fixtures/`
- `--host` + `--token` CLI flags for non-interactive setup
- `ha-nova update` subcommand
- `install.sh` curl|bash installer
- README updated with one-liner install
- Installer contract test
- Code review: fixed shift crash (C-3), rm -rf guard (C-1), curl mock -sS flag (I-2)

### Test count: 141 → 190 (+49 tests, +8 test files)

### PR structure as planned
- All changes in single PR (combined) — original plan had 4 PRs but the work is cohesive enough for one.

## 2026-03-06: Legacy Reference Cleanup

### Completed
- Removed active legacy references from `PROJECT.md`
- Removed legacy user note from `AGENTS.md`
- Deleted `docs/reference/old-project-inventory.md`
- Deleted `docs/reference/manager-dependency-matrix.md`
- Re-checked repo for the concrete legacy terms after cleanup

### Notes
- Cleanup intentionally limited to current working tree; no history rewrite

## 2026-03-11: Review Taxonomy + Threshold Guard Checks

### Completed
- Added explicit review check taxonomy docs: code family letter + running number + separate severity
- Added helper checks H-09/H-10 for weakened thresholds and off-grid helper values
- Clarified live helper evidence fetch for H-09/H-10 via `/api/states/{helper_entity_id}`
- Updated shared references from H-01..H-08 to H-01..H-10 across write/helper/review-agent/architecture docs
- Added contributor-facing pointer so the taxonomy is discoverable from `CONTRIBUTING.md` without duplicating the active catalog
- Added focused contract coverage for the taxonomy and new helper threshold rules

### Notes
- HIGH escalation for H-09 is intentionally conservative: requires concrete loop evidence (`repeat:` or already-matched R-10/R-12), not plain `choose:` alone
- Off-grid step detection is relative to `min`, not absolute zero

## 2026-03-12: Review Catalog Refactor Started

### Completed
- Locked the refactor target to `skills/review/SKILL.md` as facade + `skills/review/checks.md` as full catalog
- Updated contracts first so the repo will fail until facade/catalog split, Gemini companion-copy, and self-update support are implemented
- Documented the default that Gemini flat copies must carry companion Markdown files, not just `SKILL.md`
- Replaced the old inline-SSOT wording with the split model: entrypoint in `skills/review/SKILL.md`, detailed rules in `skills/review/checks.md`

### Next
- Move the inline review catalog into `skills/review/checks.md`
- Repoint review/write/helper/architecture docs to the split model
- Extend Gemini installer/update flows, then run multi-pass reviews and live E2E

## 2026-03-12: Review Catalog Refactor Completed

### Completed
- Split `ha-nova:review` into facade `skills/review/SKILL.md` + catalog `skills/review/checks.md`
- Repointed `review-agent`, `write`, `helper`, architecture docs, and contributor docs to the split model
- Generalized Gemini flat-copy handling so per-skill companion markdown files are copied and same-skill references resolve locally
- Matched `scripts/update.sh` to the same Gemini companion-file behavior
- Verified fresh green runs for `npm test -- --run tests/skills` and `npm test -- --run tests/onboarding`

### Live Verification
- The dedicated Codex live harness still hangs in this environment because `codex exec` sessions do not terminate cleanly; transcript inspection showed real Relay `/core` reads and writes reaching the split-skill flow before the hang
- Independent direct live Relay verification succeeded for `health`, automation `create`, first `read`, and `update`; the subsequent `automation/reload` path timed out on this machine and the test automation then read back as absent, which points to an environment/runtime issue outside the markdown-refactor scope

## 2026-03-12: Uninstall Completeness Fix

### Completed
- Reproduced that `scripts/onboarding/uninstall.sh` left the installer clone `~/.local/share/ha-nova` and CLI link `~/.local/bin/ha-nova` behind
- Added regression coverage for uninstall completeness in `tests/onboarding/macos-onboarding-script-contract.test.ts`
- Updated the uninstall script so it now removes the installer-managed clone and CLI link alongside skills/config/cache

### Notes
- This was a real uninstall gap in the script, not just leftover local state

## 2026-03-12: Global CLI UX Fix

### Completed
- Reproduced that setup/docs still suggested `npx ha-nova ...` even though the installer provisions `~/.local/bin/ha-nova`
- Chose `ha-nova ...` as the canonical post-install command and kept repo-local invocation separate
- Added red tests for installer PATH persistence and installer-first client docs
- Updated installer/docs/onboarding copy to the global `ha-nova ...` path
- Persisted `~/.local/bin` into the detected shell startup file and added a current-shell fallback hint after install/update
- Verified the full onboarding suite after the change

### Next
- Keep `npx ha-nova ...` only in explicit repo-local/dev references and historical notes

## 2026-03-12: Gemini Skill Root Isolation

### Completed
- Verified online that Gemini supports its own user skill scope and that shared discovery roots with duplicate installs are the wrong direction
- Isolated Gemini flat copies to `~/.gemini/skills`
- Updated installer, self-update, dev-sync, uninstall, and readiness detection to the new Gemini root
- Verified the full onboarding suite after the migration

### Next
- Keep watching for any remaining references that assume Gemini lives in `~/.agents/skills`

## 2026-03-12: dev-sync Simplified

### Completed
- Reduced `scripts/dev-sync.sh` to the KISS model: file-based clients delegate back to `install-local-skills.sh`
- Kept Claude as the only special-case cache sync path
- Updated CLI help and architecture guidance to reflect the narrower `dev-sync` scope

### Next
- Prefer direct `install-local-skills.sh <client>` in contributor docs whenever no Claude cache sync is needed

## 2026-03-12: HA NOVA Skill Namespacing

### Completed
- Renamed the HA NOVA source sub-skill directories to `skills/ha-nova-*`
- Matched every sub-skill frontmatter `name` to its namespaced folder name
- Updated the context skill dispatch table, review catalog references, installer/update flows, and active contracts to the namespaced IDs

### Next
- Sweep remaining historical plan/spec docs only if they start causing contributor confusion; runtime/docs/tests are already on the namespaced model

## 2026-03-12: Relay CLI Missing-Setup Error

### Completed
- Reproduced that `~/.config/ha-nova/relay health` failed with the raw file-path error `missing ~/.config/ha-nova/onboarding.env`
- Added a focused regression test for the relay CLI missing-setup path
- Changed the relay CLI to guide the user to `ha-nova setup` instead of exposing the missing config path

### Next
- Keep the missing-Keychain-token path as-is for now; only the missing-setup case was in scope

## 2026-03-12: CI Path-Contract Follow-Up

### Completed
- Reproduced that PR #75 failed only because the Gemini flat-copy contract expected macOS-specific `/Users/...` absolute paths
- Tightened the contract to require absolute rewritten repo paths across platforms instead of matching a macOS home prefix
- Added a minimal spec note for the CI-only follow-up
- Hermeticized installer/update contracts so they no longer hit a real local `claude` CLI and timeout based on machine state

### Next
- Re-run PR #75 checks and only touch runtime code if a real non-test regression appears

## 2026-03-12: Final PR #75 Codex Follow-Up

### Completed
- Switched installer Bash PATH persistence from `.bashrc` to login-shell targets (`.bash_profile` / `.profile`)
- Restored legacy Gemini marker support in `dev-sync` so local refresh matches updater migration behavior
- Added contract coverage for the installer Bash PATH targets and new `dev-sync` legacy Gemini detection

### Next
- Re-run Codex review gate and merge PR #75 once GitHub is fully clean

## 2026-03-12: Post-Merge Codex Findings Cleanup

## 2026-03-15: Windows Uninstall Truth + Step-4 Progress

### Completed
- Reproduced three real regressions with direct red tests:
  - Claude uninstall still called the CLI when the plugin was already absent
  - Claude uninstall swallowed real removal failures as warnings
  - Wizard Step 4 had no visible progress feedback
- Tightened Claude uninstall semantics in `cli/clients.go`
  - skip quietly when `installed_plugins.json` does not contain `ha-nova@ha-nova`
  - treat `not found in installed plugins` as already removed
  - fail loud on real removal failures
- Added Step-4 progress feedback in `cli/setup_progress.go` and wired it into `cli/setup_interactive.go`
- Added final `HA NOVA removed` output from the Windows helper path in `cli/commands.go`
- Added regression coverage in `cli/clients_test.go`, `cli/setup_interactive_test.go`, and `cli/uninstall_helper_test.go`

### Verification
- `cd cli && go test ./... -run 'TestRemoveInstalledClients|TestInteractiveSetupFreshInstallShowsWizardAndInstallsGeminiSkills|TestRunInternalUninstallPrintsFinalSuccess'` → passed
- `cd cli && go test ./...` → passed
- `npm run verify` → passed (`50/50` files, `282/282` tests)

### Next
- Windows real-flow recheck for final UX wording/output

## 2026-03-15: Relay Token Reuse Wizard

### Completed
- Identified the missing cross-device onboarding case: second machine, existing Relay already configured, no local token on the new device
- Wrote a minimal wizard design at `docs/superpowers/specs/2026-03-15-relay-token-reuse-wizard-design.md`
- Wrote the implementation plan at `docs/superpowers/plans/2026-03-15-relay-token-reuse-wizard.md`

### Proposed UX
- If no local token exists: choose between `Paste existing token` and `Generate new token`
- If a local token exists: choose between `Keep saved token`, `Paste existing token`, and `Generate new token`
- Only the `Generate new token` path opens the Home Assistant Relay config page
- Wizard navigation should also support `back` and `exit`, so users can correct host/token choices without restarting the whole flow

### Completed
- Reproduced the two still-active Codex review findings on top of merged PR #75
- Added a focused spec at `docs/superpowers/specs/2026-03-12-post-merge-codex-findings-design.md`
- Tightened `scripts/dev-sync.sh` so Codex/OpenCode require symlink markers and Gemini still honors current plus legacy flat-copy markers
- Tightened `scripts/onboarding/macos-lib.sh` so Gemini readiness validates companion markdown files copied by the flat installer
- Added/updated focused regression tests in `tests/onboarding/dev-sync-contract.test.ts` and `tests/onboarding/setup-resume.test.ts`

### Next
- Run the full onboarding suite
- Self-review the final diff
- Open a small follow-up PR and explicitly re-run Codex review before merge

## 2026-03-12: Installer `curl | bash` Hang Follow-Up

### Completed
- Reproduced the root cause from the installer source: global `exec < /dev/tty` during piped execution
- Added a focused spec at `docs/superpowers/specs/2026-03-12-installer-tty-handoff-design.md`
- Changed `install.sh` to keep piped stdin untouched and use `/dev/tty` only for the reinstall menu read plus setup handoff
- Added installer contract coverage to guard against reintroducing a global stdin swap

### Next
- Run focused installer/onboarding tests
- Reinstall once with the one-line GitHub command to verify the shell returns cleanly without `Ctrl+C`

## 2026-03-12: Full Understanding Refresh

### Completed
- Re-read product framing in `README.md`, `PROJECT.md`, `nova/README.md`, `nova/DOCS.md`
- Re-read mandatory reference docs in `docs/reference/` and aligned them with current runtime code
- Mapped relay runtime in `nova/src/` from bootstrap to `/health`, `/ws`, `/core`, including auth, path validation, REST/WS adapters, and HA token flow
- Mapped skill system, shared references, SessionStart hook, installer/update/dev-sync/uninstall flows, and client-specific install strategies
- Verified the current safety net live: `npm run typecheck` passed; `npm test` passed with 44 files / 243 tests

### Current Understanding
- Product center of gravity is not the relay runtime; it is the contract between markdown skills, onboarding UX, and strict install/update diagnostics
- Relay runtime is intentionally narrow: authenticated pass-through for HA REST + WS with thin validation and no domain logic
- Local client integration is first-class product surface: Codex/OpenCode use symlinks, Claude uses plugin install + SessionStart context injection, Gemini uses rewritten flat copies
- The repo currently has local uncommitted work in onboarding/test files; analysis was read-only and did not touch those changes

## 2026-03-12: Codex Review Summary Signal

### Completed
- Reproduced that the clean Codex result on PR #77 arrived as a PR discussion comment, not only as a reaction or inline review comment
- Added a focused spec at `docs/superpowers/specs/2026-03-12-codex-review-signal-flow-design.md`
- Updated `AGENTS.md` so the mandatory PR merge checklist now reads Codex review state from reactions, inline review comments, and PR issue/discussion comments

### Next
- Follow the expanded checklist on the next PR so the summary-comment channel is exercised in normal flow

## 2026-03-12: Setup WS-Degraded UX Follow-Up

### Completed
- Reproduced the real degraded state from the installed relay: `/health` returned `ha_ws_connected=false` and `/ws` probe returned `502` with `UPSTREAM_WS_CONNECT_ERROR`
- Verified that current setup intentionally allows a false-green finish and that doctor overstates `ha_llat` in the generic degraded path
- Added a focused spec at `docs/superpowers/specs/2026-03-12-setup-ws-degraded-flow-design.md`
- Added an implementation plan at `docs/superpowers/plans/2026-03-12-setup-ws-degraded-flow.md`

### Next
- Add failing onboarding tests for degraded setup completion and softer doctor wording
- Tighten setup verification so it retries degraded WS in-flow and ends with `Setup incomplete` when still unhealthy

## 2026-03-12: HALMark Curated Adoption Design

### Completed
- Reviewed HALMark repository structure, README, and `spec/halmark.md`
- Verified that HALMark is currently strongest as a stewardship-spec source, not as a runnable harness
- Stress-tested the adoption idea with three parallel review agents across architecture fit, UX value, and maintainability/attribution
- Narrowed the recommended first-pass adoption scope to `FG-18`, `FG-24`, `FG-08`, `FG-15`, and `FG-17`
- Wrote the design spec at `docs/superpowers/specs/2026-03-12-halmark-curated-adoption-design.md`
- Wrote the implementation plan at `docs/superpowers/plans/2026-03-12-halmark-curated-adoption.md`

### Current Understanding
- HALMark should influence HA NOVA as a small set of skill/check/test improvements, not as an imported framework
- The strongest adoption targets are policy and review behavior, not relay/runtime code
- Attribution should be visible but lightweight so HA NOVA remains the only active source of truth

### Next
- If implementation is approved, update skills/checks/tests for the selected subset
- Add lightweight acknowledgment for Nathan Curtis / HALMark during implementation

## 2026-03-12: HALMark Curated Adoption Execution

### Completed
- Re-reviewed the approved HALMark scope with three parallel review agents before execution
- Added/updated focused contract coverage for `FG-18`, `FG-24`, `FG-08`, `FG-15`, and `FG-17`
- Tightened `skills/ha-nova-write/SKILL.md` for invalid-premise correction, one-question ambiguity reuse, narrow diffs, delete preview blast-radius visibility, and destructive verification-before-success
- Tightened `skills/ha-nova/safe-refactoring.md` for direct-consumer scope limits and explicit post-delete verification requirements
- Added `R-16` to `skills/ha-nova-review/checks.md` and linked the trap in `skills/ha-nova/template-guidelines.md`
- Added a lightweight HALMark/Nathan Curtis acknowledgment in `README.md`

### Next
- Run full repo verification (`npm test`, `npm run typecheck`, `bash scripts/check-docs.sh`)
- Self-review final diff for wording drift or duplicate-truth issues before closing the task

## 2026-03-12: HALMark Scenario Smoke Verification

### Completed
- Added a narrow scenario-based smoke contract at `tests/skills/halmark-scenario-smoke-contract.test.ts`
- Added the matching scenario catalog at `tests/fixtures/halmark-scenarios.json`
- Covered exactly the HALMark-derived additions from this task: invalid-premise correction, room-scope ambiguity, narrow-diff updates, destructive delete verification, and templated event-name review

### Current Understanding
- These smoke tests verify scenario-to-contract coverage for the behaviors we added
- They intentionally do not claim live model-behavior proof; they strengthen regression detection on the skill/docs layer only

### Next
- Keep full-suite verification green after the new smoke layer

## 2026-03-12: HALMark Live Codex E2E

### Completed
- Added a dedicated live Codex E2E contract at `tests/e2e/codex-halmark-scenarios-contract.test.ts`
- Added the live HALMark scenario catalog at `tests/fixtures/codex-ha-nova-halmark-scenarios.json`
- Added `npm run e2e:skill:codex:halmark` as a thin wrapper over the shared scenario harness
- Ran the real HALMark Codex E2E suite and got `5/5` passing scenarios
- Fixed one false-positive harness issue by moving the scenario fixture out of `scripts/e2e/` into `tests/fixtures/`

### Current Understanding
- The live E2E layer now covers exactly the five HALMark-derived behaviors added in this task
- The suite is read-only/policy-only on purpose; it verifies end-to-end skill-guided responses without touching HA state

## 2026-03-13: Windows Support Research (No Code)

### Completed
- Researched GitHub Actions `windows-latest` runner capabilities (Windows Server 2025 image, Git 2.53, PowerShell 7.4.13, Node 22, jq 1.8.1 pre-installed)
- Researched Git Bash specifics: `uname -s` returns `MINGW64_NT-*`, `$HOME` maps to `USERPROFILE`, GNU sed (not BSD), `/tmp/` mapped to `C:\Users\<user>\AppData\Local\Temp`
- Researched DPAPI credential storage via PowerShell from Git Bash: works in non-interactive CI, tied to user+machine
- Researched WSL in GitHub Actions: not pre-installed, requires `wsl --install` + reboot (WSL2 impossible in single CI job), overhead vs Git Bash not justified
- Researched mocking strategies for local macOS testing: `fake-uname`, PATH-based mocking, ShellSpec/BATS frameworks
- Analyzed current codebase: platform abstraction already exists in `scripts/onboarding/platform/macos.sh`, `sed -i ''` is macOS-only syntax, `dns-sd` is macOS-only
- Key finding: Git Bash ships GNU sed (uses `sed -i` not `sed -i ''`), which is the OPPOSITE of macOS BSD sed

### Architecture Direction
- Create `scripts/onboarding/platform/windows.sh` parallel to existing `macos.sh`
- Platform dispatch via `uname -s`: `Darwin` -> macos.sh, `MINGW64*|MSYS*` -> windows.sh
- Credential storage: macOS Keychain -> Windows DPAPI via `powershell.exe -Command` from bash
- Browser launch: `open "$url"` -> `cmd.exe /c start "" "$url"`
- mDNS discovery: `dns-sd` -> fallback to manual IP entry (no reliable Windows CLI equivalent)
- sed portability: replace `sed -i ''` with platform-aware wrapper or use `sed -i.bak && rm *.bak`
- Temp files: use `$TMPDIR` or `mktemp -d` (works cross-platform), avoid hardcoded `/tmp/`
- CI testing: `shell: bash` in GitHub Actions invokes Git Bash on windows-latest; set `git config core.autocrlf false` before checkout

## 2026-03-14: Windows Bundle Installer + Unified Release Distribution

### Completed
- Added end-user bundle-first install flow for macOS in `install.sh` and a Windows PowerShell bootstrap in `install.ps1`
- Added release-bundle packaging + upload path via `scripts/release/build-install-bundle.sh`, `.github/workflows/release.yml`, and `.goreleaser.yml`
- Extended self-update to support both git-backed installs and bundle-backed installs in `scripts/update.sh`
- Extended version discovery to bundle installs in `scripts/version-check.sh`
- Made onboarding platform-dispatched with dynamic platform loading in `scripts/onboarding/macos-lib.sh`
- Added `scripts/onboarding/platform/windows.sh` with PowerShell-backed secure storage, browser launch, and clipboard support
- Replaced direct clipboard assumptions with platform abstractions in `scripts/onboarding/lib/ui.sh` and platform modules
- Made file-based client install Windows-safe in `scripts/onboarding/install-local-skills.sh`:
  - copy fallback for Codex/OpenCode installs
  - bundled-relay preference
  - `relay.exe` + shim install on Windows
- Pointed `ha-nova update` at the installed self-updater first in `scripts/onboarding/bin/ha-nova`
- Added/updated focused onboarding tests for installer contracts, platform dispatch, and Windows file-client behavior

### Verification
- `npm test` → 48/48 files, 300/300 tests passed
- `bash -n install.sh scripts/update.sh scripts/version-check.sh scripts/release/build-install-bundle.sh scripts/onboarding/install-local-skills.sh scripts/onboarding/bin/ha-nova scripts/onboarding/macos-lib.sh scripts/onboarding/platform/macos.sh scripts/onboarding/platform/windows.sh scripts/onboarding/lib/ui.sh` → passed
- Dry-run bundle packaging with fake relay artifacts produced:
  - `ha-nova-macos-amd64.tar.gz`
  - `ha-nova-macos-arm64.tar.gz`
  - `ha-nova-windows-amd64.zip`

### Current Shape
- End-user install path is now release-bundle-based on both macOS and Windows
- Windows UX target is now `PowerShell one-liner -> native bundle bootstrap -> ha-nova setup`
- Repo/dev git flows still exist; end-user bundle flows no longer depend on local Node/npm

## 2026-03-14: Go-First Runtime Cutover

### Completed
- Replaced `install.sh` with a thin macOS/Linux bootstrap that resolves GitHub Releases `latest`, downloads the correct bundle, installs `ha-nova` into `~/.local/bin`, and starts `ha-nova setup` only when interactive
- Replaced `install.ps1` with a native Windows bootstrap that downloads `ha-nova-windows-amd64.zip`, validates `bundle.json`, adds the install root to user `PATH`, and starts setup through the installed Go binary
- Switched `.goreleaser.yml` from `relay` artifacts to `ha-nova` artifacts and updated release notes to the new public commands
- Rebuilt `scripts/release/build-install-bundle.sh` to produce macOS, Linux, and Windows bundles with `bundle.json` and without bundling the old product shell runtime
- Reduced `scripts/update.sh`, `scripts/version-check.sh`, and `scripts/onboarding/bin/ha-nova` to legacy delegate shims into the Go CLI
- Rewrote the installer/release/self-update contract tests around the Go-first public surface

### Current Understanding
- New end-user installs now flow through native bootstrap scripts into the Go runtime instead of into the old shell onboarding/update path
- Shell scripts remain only as migration shims and repo/dev helpers; release bundles no longer need them for steady-state runtime behavior

### Next
- Run focused contract tests and `go test ./...`
- Fix any remaining contract drift in older shell-era tests that still assume `relay`-named artifacts or shell-first update behavior

## 2026-03-14: Cross-Platform Review Fix Sweep

### Completed
- Hardened self-update extraction in `cli/commands.go`:
  - reject tar/zip traversal and absolute-path entries
  - require a valid `ha-nova/` bundle root with `bundle.json`
  - replace install roots through staged swap + backup restore instead of delete-then-copy
- Made installer bootstraps write initial `state.json` ownership metadata so uninstall can clean managed PATH entries intentionally
- Simplified Windows uninstall cleanup back to the single installed `ha-nova.exe` after removing the extra launcher layer
- Extended compatibility cleanup so Windows keeps both `relay.exe` and an extensionless `relay` shim during the migration window
- Updated Go uninstall client cleanup to remove stale legacy skill trees, including `.claude/skills/ha-nova*`, even when state tracking is incomplete
- Replaced the legacy shell uninstall logic with a thin delegate into the Go runtime
- Added release workflow smoke jobs for `install.sh` and `install.ps1` on macOS, Linux, and Windows
- Added regression coverage for update extraction safety and refreshed onboarding/release contract tests
- Marked older shell-era plans/specs as historical where they still intentionally mention superseded `relay`-path or Git-Bash designs

### Verification
- `cd cli && go test ./...` → passed
- `npm run typecheck` → passed
- `npm test` → 49/49 files, 291/291 tests passed
- `bash -n install.sh scripts/release/build-install-bundle.sh scripts/onboarding/bin/ha-nova scripts/update.sh scripts/version-check.sh scripts/onboarding/uninstall.sh` → passed

### Current Shape
- Update/install/uninstall paths now match the Go-first product contract more closely across Windows, macOS, and Linux
- Historical docs still contain old command examples in some planning records, but those files are now explicitly labeled as historical rather than current truth

## 2026-03-14: Hard Cut Legacy Compatibility

### Completed
- Removed normal legacy compatibility from the Go runtime:
  - no `argv0=relay` dispatch
  - no `onboarding.env` fallback in config loading
  - no legacy shim generation in setup/update/uninstall
  - no Windows DPAPI import bridge
- Switched the Windows public entrypoint to the install-root itself on `PATH`, with no separate `ha-nova.cmd` launcher
- Confirmed on the Windows 11 VM that uninstall finalization needs a short-lived helper process; the command returns first, then the helper removes the install root a moment later
- Confirmed that `build-install-bundle.sh` alone can reuse stale `dist/` binaries during private RC testing; the correct local RC order is fresh GoReleaser build first, then bundle build
- Re-ran private RC installs against fresh local bundles: macOS private installer path passed; Windows 11 VM private installer path passed after polling for helper-based uninstall finalization
- Added explicit legacy detection to `install.sh` and `install.ps1`; both now abort with a dedicated legacy cleanup one-liner instead of attempting migration
- Added standalone `scripts/legacy-uninstall.sh` and `scripts/legacy-uninstall.ps1` for pre-Go cleanup only
- Added sidecar SHA-256 generation for install bundles and wired checksum verification into Unix bootstrap, Windows bootstrap, and Go self-update
- Tightened bundle validation to require matching `bundle.json` metadata for OS, arch, and binary name
- Removed the old `cli/compat_shims.go` runtime surface entirely

### Verification
- `cd cli && go test ./...` → passed
- focused onboarding/release/legacy tests → passed
- full `npm test` after the hard-cut runtime changes → green except for the obsolete relay-shim contract, then green after removing that expectation

### Current Shape
- Fresh installs are Go-only and JSON-config-only
- Pre-Go installs are cleanup-and-reinstall only
- Bundle replacement now has checksum + metadata gates before touching a working install

## 2026-03-14: Contributor Runtime Contract Cleanup

### Completed
- Added a contributor-wide `npm run verify` path and explicit `npm run test:cli` coverage for the Go runtime
- Moved shell-era npm aliases behind `dev:*` names so package scripts no longer present them as normal product entrypoints
- Updated contributor docs and client install docs to mention the Go-first runtime, common lifecycle commands, and legacy cleanup
- Switched smoke/e2e support scripts from `macos-onboarding.sh` / `onboarding.env` usage to `ha-nova doctor` plus `config.json`
- Updated the Claude session-start hook to use `ha-nova relay health` and `ha-nova doctor` instead of the old relay path + npm onboarding warning

### Verification
- targeted Vitest contract suite for contributor/dev-surface changes
- full `npm test`
- `npm run typecheck`
- `npm run test:cli`
- `bash -n` on touched shell scripts

### Current Shape
- Contributors now have one explicit verify command for TS + Go
- Shell-era helpers still exist, but only as repo-dev/test support surfaces

## 2026-03-14: RC Release Gate

### Completed
- Raised `docs/releasing.md` from a simple version-bump checklist to an RC runbook with:
  - `npm run verify`
  - artifact build step
  - fresh-install matrix
  - recovery matrix
  - client matrix
  - docs gate
- Added `cli/` to the README project structure so contributor-facing repo shape matches the Go-first runtime
- Narrowed the SessionStart update fallback to an explicit repo-dev-only message instead of a general user update path
- Split release automation into:
  - `ci.yml` for normal quality gates
  - `release-candidate.yml` for manual build + bundle smoke without publish
  - `release.yml` for final tagged publish
- Simplified final release verification step to `npm run verify`
- Fixed the bundle builder so it resolves real GoReleaser nested `dist/` artifacts during RC and release packaging

## 2026-03-14: Final Release Approval Gate

### Completed
- Attached the final tagged `Release` workflow job to a GitHub `production` environment
- Documented the required GitHub protection setup: `required reviewers`, `prevent self-review`, and `v*` tag protection
- Added a canonical local RC parity command: `npm run release:rc:local`
- Clarified the split between GitHub RC bundle smoke, final installer smoke on published assets, and the remaining manual real-machine checks
- Documented that `npm run release:rc:local` requires local `goreleaser`
- Renamed final GitHub smoke as post-publish confirmation to match what it actually proves
- Verified current GitHub state: the public repo currently has one direct admin collaborator and no `production` environment configured yet
- Created the GitHub `production` environment for the final release job
- Created the active `release-tags-protection` tag ruleset for `refs/tags/v*`
- Tested a direct `User` bypass, observed `current_user_can_bypass: never`, and reverted to the working repository-role bypass that reports `current_user_can_bypass: always`
- Tested environment branch/tag policy tightening, then intentionally reset `production` to a neutral state and kept the actual `v*` restriction in the dedicated tag ruleset

### Current Shape
- RC remains easy to rehearse through `release-candidate.yml`
- The repo enforces the `production` environment gate in workflow; maintainer-only release control is complete once GitHub reviewers and tag protection are configured there
- Until a second maintainer exists, protected `v*` tags are the immediate hard release gate; reviewer approval becomes the next layer later

## 2026-03-14: macOS Fresh-Home Smoke

### Completed
- Built fresh local RC artifacts via `npm run release:rc:local`
- Ran a real macOS fresh-home lifecycle from the local install bundle in a temporary `HOME`
- Exercised `version`, `setup all`, `doctor`, `relay version`, `check-update --quiet`, same-version `update`, and `uninstall --yes`
- Used tiny local HTTP servers on `127.0.0.1:8123` and `127.0.0.1:8791` to stand in for Home Assistant and Relay during the smoke

### Findings
- The macOS lifecycle smoke passed end-to-end with all four client installs present and uninstall cleanup succeeding
- CLI flag parsing follows Go `flag` rules: `ha-nova setup all --host ... --non-interactive` falls through to interactive parsing, while `ha-nova setup --host ... --non-interactive all` works

## 2026-03-14: Setup Arg Order Fix

### Completed
- Added a Go regression test for `setup all --host ... --relay-token ... --non-interactive`
- Normalized `setup` arguments so a known client target at the start is moved behind flags before Go flag parsing
- Rebuilt local RC bundles and reran the real macOS fresh-home smoke using the original user-shaped command order

### Verification
- `cd cli && go test ./...` → passed
- local rebuilt bundle smoke with `ha-nova setup all --host 127.0.0.1 --relay-token ... --non-interactive` → passed

### Current Shape
- `ha-nova setup` now accepts both the natural user form with the client first and the stricter Go flag-first form

## 2026-03-14: RC Prerelease Publish

### Completed
- Added a red contract for optional prerelease publishing in the existing RC workflow
- Extended `release-candidate.yml` with optional `publish_release` and `version_tag` inputs
- Added a conditional prerelease publish job that runs only after bundle smoke passes
- Documented how to test the real public installer path with `HA_NOVA_VERSION=vX.Y.Z-rcN`

### Findings
- The public macOS install failure against `v0.1.12` was a release-publication mismatch, not a local runtime failure
- The missing piece for real one-liner testing is public bundle availability, so the RC workflow now needs an opt-in prerelease publish path

## 2026-03-14: Rebase onto PR #96

### Completed
- Fetched `origin/main`, inspected remote commit `ef2ef1a`, and rebased the local Go-first runtime work onto the merged short skill-directory rename
- Resolved the only real conflicts in `scripts/onboarding/install-local-skills.sh`, `scripts/update.sh`, and `tests/onboarding/self-update-contract.test.ts`
- Preserved the short source dir model (`skills/read`, `skills/review`, etc.) while keeping the Go-first product runtime and RC prerelease workflow

### Findings
- The overlap with `origin/main` was much smaller than the initial diff looked: only a small set of install/update/test files needed hand resolution
- One peer review correctly caught that `install-local-skills.sh` was still pretending to provide a working self-update path for repo/dev installs

## 2026-03-14: Repo/Dev Wrapper Simplification

### Completed
- Removed the copied `~/.config/ha-nova/update` install from `install-local-skills.sh`
- Switched repo/dev `relay` and `version-check` helper installs to small wrappers that delegate into `scripts/onboarding/bin/ha-nova`
- Tightened `tests/onboarding/install-skills-per-client.test.ts` to derive short skill dirs from the real `skills/` tree and verify the installed helper wrappers

### Verification
- `npm test -- tests/onboarding/self-update-contract.test.ts tests/onboarding/install-skills-per-client.test.ts tests/onboarding/setup-resume.test.ts tests/onboarding/macos-onboarding-script-contract.test.ts tests/onboarding/platform-dispatch.test.ts` → passed
- `npm run verify` → passed

### Current Shape
- Product update remains Go-first only
- Repo/dev helper installs keep `relay` and `version-check` convenience entrypoints
- Repo/dev helper installs no longer claim a fake self-update capability without state

## 2026-03-15: Desktop Validation Planning

### Completed
- Wrote a dedicated validation spec at `docs/superpowers/specs/2026-03-15-desktop-validation-design.md`
- Wrote the execution plan at `docs/superpowers/plans/2026-03-15-desktop-validation.md`
- Split the future proof strategy into four lanes: artifact integrity, macOS fresh-home, Windows headless installer, and Windows desktop setup/client validation

### Findings
- Windows `setup` cannot currently be treated as proven via SSH because the credential-store step can fail in a non-desktop logon session
- The right confidence model is hybrid: prepare and collect over SSH, but execute real Windows `setup` inside an RDP desktop session

## 2026-03-15: Desktop Validation Execution

### Completed
- Added private RC helper runners and mock server under `scripts/dev/`
- Added contract coverage for the new helper scripts and macOS keychain behavior
- Hardened Windows uninstall/error handling and Windows PATH matching
- Added private RC keyring isolation via `HA_NOVA_KEYRING_SERVICE`
- Switched macOS keychain access back to shared default-keychain resolution and dropped explicit ACL tweaking on writes
- Fixed Gemini Go installs to map short repo skill dirs to `ha-nova-*` Gemini flat-copy targets
- Ran private RC macOS smoke: passed
- Ran private RC macOS `setup all`: passed
- Ran private RC macOS client lanes:
  - `codex`: passed
  - `opencode`: passed
  - `gemini`: passed
  - `claude`: passed
- Ran private RC Windows headless installer lane on VM `192.168.1.188`: passed
  - install: passed
  - `ha-nova version`: passed
  - `ha-nova uninstall --yes`: passed
  - final install-root disappearance: passed

### Findings
- The first macOS helper versions incorrectly relied on `PATH` exported by the child installer process; using the explicit installed runtime path fixed the runners.
- The private-RC lane should isolate credentials with the explicit test-keyring override instead of forcing product code onto `login.keychain-db`.
- Private RC secure-token writes need an isolated service name; otherwise local validation risks overwriting a maintainer's real HA NOVA credentials.
- The Go Gemini installer still used stale `ha-nova-*` source dir names and silently skipped companion skills until corrected.
- Claude plugin registration is best verified through `.claude/plugins/installed_plugins.json`; success output alone is not reliable proof.
- `npm run verify` is flaky in this harness when chained as one process and can be killed by the OS, but the three underlying gates passed cleanly when run separately:
  - `npm run typecheck`
  - `npm test`
  - `npm run test:cli`
- Windows PowerShell runners cannot safely capture native-command warnings through plain `2>&1`; routing HA NOVA calls through `cmd.exe /d /s /c` keeps warnings as text and makes the result files reliable on Windows PowerShell 5.
- Windows desktop validation is still pending the real RDP lane; the current proof on Windows covers installer/version/uninstall only, not secure-token setup or real client integration.

## 2026-03-15: Desktop Validation Execution

### Completed
- Added private-RC helper runners for macOS and Windows plus a tiny mock HA/relay server
- Tightened the Windows uninstall path so the helper performs token/PATH/config cleanup after the install root is discarded
- Added `--relay-url` to `ha-nova setup` so the private validation lanes can use free mock ports instead of fixed `8791`
- Added contract coverage for the desktop-validation helpers and Windows PATH/uninstall edge cases
- Built fresh private RC bundles and served them locally for real macOS validation

### Verification
- `npm run verify` → passed (`52/52` files, `311/311` tests)
- `cd cli && go test ./...` → passed
- `npm run release:rc:local` → passed
- `scripts/dev/macos-private-rc-smoke.sh` against local bundles → passed
- `scripts/dev/macos-private-rc-setup-all.sh` against local bundles + mock HA/relay on custom ports → passed
- `scripts/dev/macos-private-rc-client.sh codex` → passed
- `scripts/dev/macos-private-rc-client.sh opencode` → passed
- `scripts/dev/macos-private-rc-client.sh gemini` → passed
- `scripts/dev/macos-private-rc-client.sh claude` → passed

### Findings
- The original macOS temp-home keychain failure was a harness diagnosis step, not a product-path failure
- A fixed-port assumption in the helper lane was the real instability; adding `--relay-url` and using free mock ports removed that flake
- The original Claude helper assertion was wrong: successful plugin registration can be silent, so the harness now checks `claude plugin list` directly

## 2026-03-15: Safe Test Architecture Planning

### Completed
- Planned a reduced test architecture around one safe default gate and three explicit desktop/VM lanes
- Wrote the design doc at `docs/superpowers/specs/2026-03-15-safe-test-architecture-design.md`
- Wrote the implementation plan at `docs/superpowers/plans/2026-03-15-safe-test-architecture.md`

### Findings
- The real failure mode was not the new Go runtime itself, but host-affecting legacy shell tests still reachable from default verification
- The minimal clean fix is boundary-based: make `verify` host-safe, keep desktop proof explicit, and stop mixing them

## 2026-03-15: Safe Test System Planning

### Completed
- Planned a separate safe-test architecture after the host-side macOS browser/keychain incidents
- Split the future test system into `safe default` and `explicit desktop validation`
- Chose to remove risky shell onboarding flows from default verification instead of trying to harden them all equally

### Findings
- The root safety problem is not only browser-opening code; it is that routine verification can still reach interactive shell onboarding paths
- The smallest robust fix is a hard default boundary: safe local/CI verification never opens browsers and never touches real secure stores
- Desktop validation remains necessary, but only as explicit macOS and Windows release lanes

## 2026-03-15: Safe Test System Implementation

### Completed
- Switched `npm test`/`npm run verify` to a host-safe Vitest split that excludes the old shell onboarding setup suites
- Added `HA_NOVA_NO_BROWSER=1` handling to the Go runtime and shell browser helpers
- Added a file-based `HA_NOVA_TEST_KEYRING_FILE` override for Go secure-token storage
- Routed safe test helpers through the browser/keyring guards
- Added contract coverage for the new test system structure
- Added explicit Windows desktop helper npm entrypoints plus a documented macOS emergency cleanup command
- Tightened the safe-lane guard so clipboard writes are disabled together with browser launches
- Required explicit `HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1` before the file-based test keyring override becomes active

### Findings
- The smallest stable split is `test:safe` for default verification plus explicit desktop helper commands/scripts for release proof
- The old shell onboarding suites were the wrong place to define the default quality gate, even with mocks
- A file-path env var alone was too easy a downgrade path for secure-token storage; a second explicit opt-in keeps the test escape hatch small but deliberate
- The first macOS desktop-lane run still reported clipboard copy because it was consuming stale bundles; `test:desktop:macos` now refreshes and serves its own private RC artifacts before running the helper lanes

## 2026-03-15: Desktop Validation Execution Refresh

### Completed
- Added a self-refreshing `scripts/dev/macos-private-rc-suite.sh` and pointed `npm run test:desktop:macos` at it
- Re-ran the full macOS desktop lane against freshly rebuilt private RC bundles
- Verified the Windows headless lane against the live VM using the prepared private RC bundle server
- Staged the Windows desktop runner scripts on the VM under `C:\\Users\\markus\\ha-nova\\scripts\\dev`

### Verification
- `npm run test:desktop:macos` → passed against fresh bundles
- macOS lane no longer prints `Copied relay token to clipboard`
- Windows VM headless lane via `windows-private-rc-install.ps1` → passed
- Windows result file: `VERSION_EXIT:0`, `UNINSTALL_EXIT:0`, `UNINSTALL_EXISTS:False`

### Findings
- The stale-bundle issue was real and could have produced a false-green desktop proof
- The fresh macOS lane now proves the current guard behavior rather than an older bundle
- Windows headless proof is now real; the only remaining platform-specific release proof gap is the interactive RDP desktop lane

## 2026-03-15: Setup Prompt Parity Fix

### Completed
- Restored the old release's numbered client-selection list in the Go `ha-nova setup` flow
- Added CLI tests for the list rendering, default selection, numeric input, and typed client names
- Removed the dead `detectLikelyClients()` branch from interactive setup selection

### Findings
- The UX regression was real: the Go runtime had fallen back to a raw free-text prompt instead of the old guided list
- The release-equivalent minimum is the old `Which AI client do you use?` numbered list with Claude as the default

## 2026-03-15: Setup Wizard Parity Review

### Completed
- Reviewed the new Go setup flow directly against the current release wizard flow
- Confirmed that only the first client-selection prompt had been restored so far
- Wrote a focused spec and implementation plan for full wizard parity on the existing feature branch

### Findings
- The current Go setup flow is still below the old release UX bar after the client-list fix
- Missing parity areas are phased guidance, setup-state/resume summary, guided relay/WS retries, and final success/incomplete banners

## 2026-03-15: Setup Wizard Parity Implementation

### Completed
- Added Go-native wizard UI helpers for header, step display, status summary, and complete/incomplete banners
- Added a minimal `setupState` model plus current-state detection for config/token/relay/ws/skills
- Split interactive setup from non-interactive setup so the richer wizard only affects the normal userflow
- Restored a phased interactive flow: client choice, host guidance, secure-access step, verification step, skill-install step, final banner
- Fixed the multi-prompt stdin bug by reusing one buffered reader across the whole interactive wizard
- Ran a safe local dry-run with temp home, no browser, and test keyring to inspect the real prompt flow

### Verification
- `cd cli && go test ./... -run 'TestPromptSetupClient|TestRenderSetup|TestSetupState|TestPromptReadersCanBeReusedAcrossSequentialSetupQuestions|TestNormalizeSetupArgs'` → passed
- safe local dry-run: `printf '4\\n127.0.0.1:38123\\n\\nN\\n' | (cd cli && go run . setup --relay-url http://127.0.0.1:38791)` → showed the new phased wizard flow end-to-end
- `npm run verify` → passed (`49/49` files, `277/277` tests)

### Findings
- The old-release list prompt was only the visible tip; the real parity win came from separating interactive wizard UX from the non-interactive automation path
- The original prompt helpers had a real stdin buffering bug that only showed up in scripted interactive runs; fixing that was necessary for reliable wizard behavior

## 2026-03-15: Setup Wizard Go-Contract Tests + Follow-up Fixes

### Completed
- Added direct Go-level interactive wizard tests in `cli/setup_interactive_test.go` for:
  - fresh-install happy path with real skill-tree assertions
  - fully-complete resume path
  - WS-degraded incomplete path
- Added a dedicated already-done banner for fully-complete resume flows
- Restored the two-token explanation text in Step 2 of the Go wizard
- Tightened `relayHealthWSConnected()` so it only trusts explicit `ha_ws_connected` values
- Skipped Step 2 when relay health is already verified and the existing token is still valid

### Verification
- `cd cli && go test ./... -run 'TestInteractiveSetup(FreshInstallShowsWizardAndInstallsGeminiSkills|AlreadyDoneUsesResumeBanner|WSDegradedEndsIncomplete)'` → passed
- `cd cli && go test ./...` → passed
- `npm run verify` → passed
- `npx vitest run tests/onboarding/setup-fresh-install.test.ts tests/onboarding/setup-resume.test.ts tests/onboarding/setup-relay-failures.test.ts` → passed (`16/16`)

### Findings
- The first red run exposed a real state bug: `status:"ok"` was incorrectly treated as proof that Home Assistant WS was connected
- The old shell parity suites are still useful, but they do not prove the new Go wizard; the new direct Go tests close that gap for the first time
- Remaining review rest: the “already done” path still lacks the old shell flow's extra WS-ping fallback before calling a system fully ready

## 2026-03-15: Canonical Setup Host Fix

### Completed
- Added `applySelectedSetupHost()` in the Go setup flow
- Recomputed `relay_base_url` whenever setup receives a new Home Assistant host/URL choice
- Preserved explicit relay overrides instead of force-rewriting custom relay URLs
- Added direct Go tests for stale-relay replacement and explicit-relay preservation

### Verification
- `cd cli && go test ./... -run 'TestApplySelectedSetupHost'` → passed
- `cd cli && go test ./...` → passed
- `npm run verify` → passed

### Findings
- The split-brain state was real: an old `relay_base_url` could survive a new HA host choice and send verification to the wrong machine
- The minimal safe rule is: canonical host selection updates relay derivation, but explicit relay overrides still win
- Review follow-up found a second real bug: completed resume flows were exiting before endpoint overrides were applied or persisted
- The fix now applies endpoint overrides before resume-state detection and saves the updated config even when the wizard ends in the `already set up` banner
- A fresh-review red run exposed another real test issue: the prompt-driven wizard test had been passing via an ambient localhost relay on `:8791`, not purely through its own fixtures
- The discovery path now parses `arp` output in Go, so Windows host fallback no longer depends on `sh`/`sed`/`head`
- The old-release ordering gap was also real: discovery had drifted behind Step 1; the Go wizard now discovers/prompts the HA host before the app-install guide again
- The next parity gap turned out not to be “missing network scan,” but missing visible feedback; the old release already used candidate-based detection and a visible host-check spinner, not a subnet scanner
- Discovery now keeps the same KISS detection model but shows a visible minimum-duration progress phase in TTYs, and host validation now shows the old `Checking connection to Home Assistant...` progress step again
- Another real parity gap surfaced during a live Windows run: the Go wizard had dropped the old `/ws` ping fallback and LLAT-specific degraded guidance
- Setup WS verification now mirrors the old release again: health `ha_ws_connected=false` triggers a `/ws` ping re-check, and `LLAT is required` is surfaced as a concrete `ha_llat` action instead of only a generic WS warning
- Live app logs showed `auth_source:"env_ha_llat"` and `auth_capability:"full"` during relay bootstrap, which proves a non-empty LLAT reached runtime bootstrap but does not fully rule out every app-side persistence edge case
- The remaining real issue is semantic drift: relay `/health` is a passive lazy-WS snapshot, while setup/doctor need effective readiness; the fix stays in the CLI interpretation layer, not in App option persistence

## 2026-03-15: Multi-Review Tightening For Readiness + Later Skill Use

### Completed
- Re-checked the latest readiness findings against `cli/setup_state.go`, `cli/commands.go`, `cli/setup_relay_diagnostics.go`, `cli/runtime.go`, and `cli/relay.go`
- Tightened the WS-readiness spec/plan so the shared readiness truth now explicitly covers:
  - setup Step 3
  - resume / already-set-up detection
  - `ha-nova doctor`
  - post-onboarding confidence for later skill use
- Split out the Windows inline `relay ws -d ...` problem as its own CLI diagnostic bug instead of mixing it into the readiness fix

### Findings
- The decisive false-negative proof is the pair `relay health => ha_ws_connected=false` plus successful `/ws` ping, not `/health` alone
- `detectSetupState()` is still a drift point until it consumes the same readiness helper as setup and doctor
- Later skill use on macOS/Windows does not need a second readiness model; it needs onboarding, doctor, and resume to agree on one truth and then one real skill-call smoke per supported lane
- Windows inline `relay ws -d ...` remains separate from onboarding truth; `--data-file` working proves the relay path itself can be healthy while the CLI payload path is still rough

## 2026-03-15: Shared Readiness + Windows Relay Payload Hardening

### Completed
- Added shared relay readiness evaluation in `cli/setup_readiness.go`
- Routed setup Step 3, resume-state detection, and `ha-nova doctor` through the same `/health` + `/ws` truth
- Added direct Go regression coverage for:
  - `health=false + ws ping ok`
  - LLAT-specific degraded guidance
  - resume-state ws-fallback success
  - doctor ws-fallback success / LLAT diagnosis
- Hardened inline relay JSON payload handling in `cli/relay.go` so obviously shell-wrapped JSON is normalized and invalid inline payloads fail locally with a clearer message

### Verification
- `cd cli && go test ./... -run 'Test(CheckRelayReadiness|DetectSetupStateUsesWSPingFallbackForResume|RunDoctor|LoadRelayPayload)'` → passed
- `cd cli && go test ./...` → passed
- `npm run verify` → passed (`49/49` files, `277/277` tests)

### Findings
- The real readiness bug was broader than Step 3: resume-state and doctor were still capable of contradicting setup on the same machine
- For later skills, the important part is not a second readiness layer inside markdown skills; it is that onboarding, doctor, and resume now agree before the first real skill call
- Windows inline `relay ws -d ...` was still too shell-fragile for later skill use; local JSON validation plus wrapper normalization is the smallest safe hardening for now

## 2026-03-15: Multi-Agent Cross-Platform Skill Audit

### Completed
- Audited active HA NOVA skills, client install docs, Claude plugin metadata, and Claude install/update behavior for cross-platform drift
- Collected two independent agent reviews plus local repo evidence
- Wrote a dedicated cross-platform skill contract spec/plan

### Findings
- The skills layer is still too bash-/Unix-centric: many active skills still teach `relay ws -d '...'`, Unix temp files, shell pipes, and backslash continuations as the normal path
- Claude is a separate high-risk drift point: the current plugin marketplace metadata still points at the GitHub repo, so Claude can load stale or repo-sourced content instead of the exact tested bundle
- The current repo already has the runtime pieces for Windows/macOS onboarding, but not yet a consistently cross-platform instruction contract for later skill calls

## 2026-03-15: Mock Reported-Version Wording

### Completed
- Renamed the private mock argument from `--relay-version` to `--reported-version`
- Renamed helper env vars from `MOCK_RELAY_VERSION` to `MOCK_REPORTED_VERSION`
- Changed mock startup output to say `fake relay /health` plus `reported version`
- Added/updated contract coverage so the wording cannot silently drift back

### Findings
- The old wording was genuinely confusing once App versioning (`nova/config.yaml`) and skill/bundle versioning (`version.json`) diverged
- For private desktop validation, the mock should mirror the bundle line under test in `/health.version` while staying explicit that it is not the real HA App version

## 2026-03-15: Shell-Agnostic Skill Contract

### Completed
- Added a hard skill contract test that active HA NOVA skills must expose `--data-file` / `--body-file` / `--out`
- Rewrote the context skill and relay API reference so file-based relay calls are the canonical path
- Migrated the active HA skills (`read`, `review`, `helper`, `entity-discovery`, `fallback`, `service-call`, `safe-refactoring`) off bash-first inline JSON and `/tmp` examples

### Findings
- The biggest remaining Windows/macOS drift was not runtime anymore; it was the markdown contract teaching Unix habits after setup succeeded
- Tightening the canonical examples was enough for this chunk; no relay runtime changes were needed

## 2026-03-15: Client Install Docs Scope

### Completed
- Added a safe contract test for client install docs
- Kept `README.md` free of Claude/OpenCode Windows migration details
- Added Claude Windows notes (`Git for Windows`, `WSL`, `claude install`, `npm.cmd`) only to `.claude/INSTALL.md`
- Added OpenCode Windows WSL note only to `.opencode/INSTALL.md`

### Findings
- The product docs and the client docs need different granularity: HA NOVA README should stay product-level, while client-specific install quirks belong beside that client only

## 2026-03-15: Claude Local Marketplace Determinism

### Completed
- Added a Go unit test proving Claude setup registers the installed bundle root, not the GitHub repo URL
- Added onboarding contract coverage proving `install-local-skills.sh claude` stages `~/.config/ha-nova/claude-marketplace/.claude-plugin/marketplace.json`
- Reworked Go Claude setup to rewrite the installed `.claude-plugin/marketplace.json` to the absolute install root before `claude plugin marketplace add`
- Reworked the repo/dev shell installer to stage a local Claude marketplace under `~/.config/ha-nova/claude-marketplace` pointing at the local checkout payload
- Updated `.claude/INSTALL.md` so repo-checkout repairs use `bash scripts/onboarding/install-local-skills.sh claude`

### Findings
- The real Claude drift was not the plugin payload itself but the marketplace source of truth: repo/GitHub URLs let Claude load content outside the validated payload
- Installed bundles and repo/dev installs need different mechanics but the same outcome: Claude must always see a local payload path, never a drifting remote source during validation

## 2026-03-15: Claude Plugin Refresh Parity

### Completed
- Added failing Go + onboarding tests proving an already-installed Claude plugin must be refreshed with `claude plugin update`
- Updated Go Claude setup so existing `ha-nova@ha-nova` uses `plugin update`, while first install still uses `plugin install`
- Updated `install-local-skills.sh claude` to follow the same refresh-vs-install split
- Confirmed the stale-Windows symptom matched this exact gap: marketplace source was already local, but existing Claude cache content was not being refreshed

### Findings
- The stale Claude behavior was one layer above the marketplace source rewrite: setup still needed an explicit refresh verb for already-installed plugins
- `install` is not a sufficient freshness contract once Claude already has the plugin cached; `update` is the minimal deterministic fix

## 2026-03-15: Token Reuse Wizard + Page Flow

### Completed
- Added token-choice navigation for fresh-device setup: keep saved token, paste existing token, or generate a new token
- Added page-style screen clearing for interactive TTY wizard pages
- Added client-page `exit` handling plus regression coverage for pasted-token onboarding, back-to-host correction, and clean wizard cancellation
- Restored resume speed by skipping the token-choice page when the current device already has a saved relay token
- Added TTY resume pause before page redraw, repeated-`back` handling on the first client page, relay-token-flag persistence before verification, and invalid-choice reprompts for client/token pages
- Split setup verification into `cli/setup_verify.go` to keep the main interactive wizard file within the project size guardrail

### Verification
- `cd cli && go test ./...` → passed
- `npm run verify` → passed (`50/50` files, `282/282` tests)

### Findings
- The only real regression from the new token page was on same-device resume: forcing token-choice there broke old fast-retry flows and several verification tests
- Fresh-device token reuse and same-device resume need different defaults; combining them into one path makes the wizard slower and noisier than the old release

## 2026-03-15: Local Validation Harness

### Completed
- Added `scripts/dev/start-local-validation-harness.sh`
- Added `npm run dev:validation:harness`
- Documented the harness in `docs/releasing.md`
- Kept the harness intentionally small: rebuild bundles, serve them, optionally start the tiny mock, print copy/paste install commands

### Findings
- The repeated local-validation failures were all harness problems: stale bundles, wrong filenames, dead local bundle servers, or missing mock processes
- A single foreground helper is enough; automating the real wizard itself would add complexity without improving the actual UX proof

## 2026-03-15: Final Review Hardening

### Completed
- Fixed non-interactive setup so `--host` / `--ha-url` use the same normalization contract as the interactive wizard
- Fixed interactive `--relay-token` runs so `back` from verify can actually reach earlier pages again
- Fixed discovery confirmation so a real, reachable `homeassistant.local` is treated as confirmed instead of fallback
- Fixed Unix uninstall path cleanup so HA NOVA only removes PATH lines it actually managed
- Fixed Claude plugin refresh to recover from stale `installed_plugins.json` by falling back from `plugin update` to `plugin install`
- Extended Windows dev cleanup to remove Claude plugin artifacts and to attempt a clean uninstall before hard cleanup
- Fixed Windows desktop validation so `-Client all` proves all expected artifacts
- Fixed the Windows local-skill installer fallback so it no longer creates a fake `relay.exe` from a Bash wrapper
- Fixed harness/mock reported-version drift by deriving the reported version from the served bundle
- Narrowed Windows client docs/update wording so they stop over-promising unverified native client coverage
- Removed the obsolete `git pull ... re-run setup` legacy update hint

### Verification
- `go test ./... -run 'Test(RemoveManagedPath|InstallClaudePluginFallsBack|DetectDefaultHAHostTreatsReachableHomeassistantLocalAsConfirmed|ApplySetupFlagOverridesNormalizesURLShapedHost|InteractiveSetupRelayTokenFlagCanBackToHostAfterVerifyFailure)'` → passed
- `npx vitest run tests/onboarding/client-install-docs-contract.test.ts tests/onboarding/desktop-validation-contract.test.ts tests/onboarding/macos-keyring-contract.test.ts` → passed
- `bash -n scripts/dev/start-local-validation-harness.sh scripts/dev/macos-private-rc-setup-all.sh scripts/dev/macos-private-rc-client.sh scripts/onboarding/install-local-skills.sh` → passed
- `cd cli && go test ./...` → passed

### Findings
- The biggest remaining risk before this pass was no longer one big bug but small contract drifts between wizard/setup, legacy helpers, validation harnesses, and client docs
- The full-review pass found real issues in exactly those seams: stale Claude state, bundle/mock drift, PATH cleanup ownership, and Windows/macOS contract mismatches

## 2026-03-15: Claude Marketplace Remote vs Local

### Completed
- Reproduced the real macOS Claude Step-4 failure with Claude itself: absolute string `plugins.0.source` is rejected, relative sources like `./` and `./plugins/ha-nova` are accepted
- Switched the repo marketplace template to same-repo relative source `./`
- Split Claude marketplace handling into:
  - default production path: GitHub marketplace URL
  - explicit local test path: `HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1`
- Updated Go runtime local staging to emit Claude-valid relative local sources
- Updated repo/dev installer staging to emit `./ha-nova`
- Fixed Claude installed-plugin detection/removal to handle the real object-map `installed_plugins.json` shape
- Hardened local Claude helper flows and validation harness output around the explicit local override

### Verification
- `claude plugin marketplace add https://github.com/markusleben/ha-nova && claude plugin install/update/remove ha-nova@ha-nova` with temp `HOME` → all passed
- `npx vitest run tests/onboarding/install-skills-per-client.test.ts tests/onboarding/client-install-docs-contract.test.ts tests/onboarding/desktop-validation-contract.test.ts tests/skills/ha-nova-contract.test.ts` → passed
- `cd cli && go test ./... -run 'TestInstallClaudePlugin|TestRemoveInstalledClients'` → passed

### Findings
- The real Claude schema failure was not a generic plugin bug; it was specifically our absolute-path marketplace rewrite
- End-user update semantics and local bundle validation need opposite defaults; one path cannot honestly serve both without an explicit override

### Follow-up fixes
- Non-interactive setup now writes the relay token first and rolls it back if `config.json` persistence fails
- `install.sh` now rewrites `state.json` on reinstall instead of leaving stale Unix metadata behind
- Windows cleanup now also deletes `ha-nova.test.*` Credential Manager entries via `cmdkey`
- Private Claude validation lanes now assert the local marketplace source/root, not just the installed plugin record

## 2026-03-15: Final KISS Review Tightening

### Completed
- Removed clipboard/browser side effects from `setup --non-interactive`
- Added rollback for non-interactive setup when readiness verification fails, restoring the prior token/config/state snapshot
- Tightened `--host` flag handling so unresolved hosts fail instead of silently degrading to guessed URLs
- Hardened Windows ARP parsing to ignore interface/header rows
- Made the mDNS discovery regression test deterministic via an injectable availability gate
- Tightened the bundle builder so mixed flat+nested `dist/` artifacts fail instead of being silently mixed
- Renamed old macOS shell onboarding npm shortcuts to `dev:legacy:onboarding:macos*`
- Extended desktop/safe-test/client-doc contracts to pin `HA_NOVA_NO_BROWSER`, legacy-shell warnings, harness cleanup wording, and the public Claude marketplace/default-vs-local contract

### Verification
- `cd cli && go test ./... -run 'TestRunSetupNonInteractive|TestApplySetupFlagOverrides|TestParseARPHosts|TestDiscoverHAViaMDNS'` → passed
- `npx vitest run tests/onboarding/macos-onboarding-script-contract.test.ts tests/onboarding/safe-test-system-contract.test.ts tests/onboarding/desktop-validation-contract.test.ts tests/onboarding/client-install-docs-contract.test.ts tests/onboarding/release-contract.test.ts` → passed
- `cd cli && go test ./...` → passed
- full `npm run verify` rerun in progress / final pass requested immediately after this tightening

### Findings
- The last real regressions were no longer feature bugs; they were seam bugs between automation-vs-interactive setup semantics, discovery heuristics, stale `dist/` layouts, and contributor-facing npm/doc contracts
- The final hardening pass deliberately chose smaller guardrails over new architecture: fail fast, rollback, rename legacy entrypoints, and pin the promises in tests

### Follow-up fixes
- Restored the completed-setup host-override path so an already-good machine can persist a new host/URL without being forced through a fresh readiness proof
- Changed Windows secure-storage semantics so Credential Manager success remains authoritative even if the legacy DPAPI mirror cleanup/write later fails

## 2026-03-16: macOS mDNS Browse Indentation Fix

### Completed
- Reproduced the real macOS discovery miss against live `dns-sd` output on the maintainer machine
- Isolated the root cause to `parseMDNSBrowseInstance()` rejecting browse rows with leading whitespace before the timestamp
- Added a regression test covering the real indented `dns-sd -B` row shape
- Relaxed the browse regex to tolerate optional leading indentation while keeping the rest of discovery unchanged

### Verification
- `cd cli && go test ./... -run 'TestParseMDNSBrowseInstanceAcceptsIndentedBrowseRows|TestParseMDNSBrowseInstanceReturnsHomeAssistantInstance|TestDiscoverHAViaMDNSKeepsLookupOutputAfterTimeoutStyleCompletion'` → passed
- `cd cli && go test ./...` → passed
- `npm run verify` → passed (`51/51` files, `287/287` tests)
- Parallel delta reviews → no findings

### Findings
- The discovery fallback to `homeassistant.local` was not a policy change or candidate-order regression
- The actual bug was a single parser assumption: `dns-sd` browse rows were assumed to begin at column 0, but real macOS output can indent them

## 2026-03-16: Claude Local Validation Cache Bust

### Completed
- Proved that the installed Claude plugin cache still contained old HA NOVA skills pointing to `~/.config/ha-nova/relay` and `npm run onboarding:macos`, while the repo skills were already updated to `ha-nova relay ...`
- Isolated the problem to Claude local validation reusing the stale cached plugin payload for version `0.1.12`
- Changed the Go local-Claude path to remove the plugin first, clear stale cache, clear stale installed-plugin records, and reinstall fresh
- Changed the local shell installer to mirror the same refresh behavior and clean stale `installed_plugins.json` entries
- Updated local Claude install docs to make the forced cache refresh explicit

### Verification
- `cd cli && go test ./... -run 'TestInstallClaudePluginLocalMode(ReinstallsAndClearsCache|RemovesPluginBeforeDeletingCache)'` → passed
- `npx vitest run tests/onboarding/install-skills-per-client.test.ts tests/onboarding/client-install-docs-contract.test.ts` → passed
- `bash -n scripts/onboarding/install-local-skills.sh` → passed
- `cd cli && go test ./...` → passed
- `npm run verify` → passed (`51/51` files, `288/288` tests)
- Parallel reviews on the final delta → no findings after follow-up fixes

### Findings
- The bad Claude response was not caused by the current repo skills; it came from a stale cached local plugin payload
- End-user GitHub marketplace behavior should stay on the normal install/update path; only the local validation path needs the aggressive cache-bust/reinstall behavior

## 2026-03-16: macOS Uninstall Deletes Relay Token

### Completed
- Rechecked the current `origin/main` shell uninstall path and confirmed it removes the macOS Keychain relay token
- Rechecked the current `origin/main` macOS onboarding flow and confirmed reinstall/token-reuse only works when setup is rerun without a prior uninstall
- Restored Go uninstall policy to delete the relay token on macOS like `origin/main`
- Kept the new uninstall output reporting so the token deletion is explicit to the user
- Added a regression test pinning the delete-on-uninstall policy

### Verification
- `cd cli && go test ./... -run 'TestShouldDeleteRelayAuthTokenOnUninstallFollowsPlatformPolicy|TestInstallClaudePluginLocalModeReinstallsAndClearsCache|TestInstallClaudePluginLocalModeRemovesPluginBeforeDeletingCache|TestRunDoctorTreatsWSPingSuccessAsReady'` → passed
- `cd cli && go test ./...` → passed
- `npm run verify` → passed (`51/51` files, `288/288` tests)

### Findings
- `origin/main` currently deletes the relay token on macOS uninstall via `scripts/onboarding/uninstall.sh`
- The real parity target is delete-on-uninstall; the separate password dialog must therefore be fixed by isolating the prompt source, not by keeping the token

## 2026-03-16: Uninstall Feedback Parity

### Completed
- Added a focused uninstall output regression test that requires concrete removal lines, relay-token reporting, and the final success line
- Added a tiny uninstall report helper shared by the normal uninstall path and the Windows helper path
- Wired client removal, PATH cleanup, file removal, cache removal, and token policy into the report output

### Verification
- `cd cli && go test ./... -run 'TestRunUninstallReportsConcreteRemovalsAndTokenPolicy|TestRunInternalUninstallPrintsFinalSuccess'` → passed

### Findings
- The Go uninstall had become too quiet compared to `origin/main` shell uninstall
- The missing feedback was not a platform issue; it was simply absent reporting in the Go path

## 2026-03-16: Client Registry Scope Review

### Completed
- Re-mapped the current hardcoded client surfaces in `cli/setup_ui.go`, `cli/clients.go`, `scripts/onboarding/install-local-skills.sh`, `docs/reference/skill-architecture.md`, and onboarding contracts
- Reviewed future-target pressure from Cursor / VS Code against official MCP/customization docs instead of guessing from today's installer shapes
- Tightened the client-registry spec and plan so MVP stays limited to the four real installable targets, three existing adapter families, and one small checked-in JSON registry source

### Verification
- Parallel codebase reviews on registry boundaries and migration order
- Official docs check via Exa on Cursor MCP/customization and VS Code Copilot customization / agent tools / MCP support

### Findings
- The real duplication boundary is install adapter + OS/support policy + doc/validation metadata; it is not the skills themselves
- Cursor / VS Code should not be forced into the first registry pass because their integration shape is wider than today's skill installers
- KISS path: use one small `clients/registry.json` first, migrate the current four targets, reserve future editor adapters for a later explicit contract

## 2026-03-16: Onboarding + Uninstall Parity Polish

### Completed
- Added a small interactive LLAT walkthrough helper so Step 2 now actively guides the Home Assistant security-token flow before verify
- Restored LLAT guidance even when the relay token was already supplied on the interactive setup command line
- Moved interactive `config.json` / `state.json` persistence to after verify completes or explicitly ends incomplete
- Added an uninstall preflight summary
- Added an uninstall relay-running probe/note
- Fixed uninstall reporting so missing relay tokens are not falsely reported as removed
- Fixed noop uninstall output so it no longer claims `HA NOVA removed`
- Aligned actual uninstall cache removal with the new preflight wording by removing the whole HA NOVA cache directory

### Verification
- `cd cli && go test ./... -run 'TestInteractiveSetupFreshInstallGuidesLLATSetup|TestInteractiveSetupWithRelayTokenFlagStillGuidesLLATSetup|TestInteractiveSetupBackFromVerifyDoesNotPersistConfig|TestRunUninstallReportsConcreteRemovalsAndTokenPolicy|TestRunUninstallShowsPreflightAndRelayStillRunningNote|TestRunUninstallNoopDoesNotClaimRemoval'` → passed

### Findings
- The last setup parity gap was not token generation itself; it was the missing proactive LLAT walkthrough before verify
- The uninstall gap was not only verbosity; there were also two small truthfulness issues in reporting (`relay auth token` and noop final success)

## 2026-03-16: Final Onboarding + Uninstall Hardening

### Completed
- Updated the remaining interactive wizard tests to feed the full LLAT walkthrough and assert clean exits
- Fixed Step-3 retry prompting so relay-unreachable paths ask only `Retry connection check?`
- Preserved the real verify failure category when the user declines retry
- Stopped Claude setup-state detection from trusting stale `InstalledClients` without a real plugin record
- Made Claude setup fail loudly if `claude` is not installed instead of pretending success
- Added interactive persistence rollback when `state.json` save fails after `config.json` already changed
- Added Claude marketplace removal during uninstall cleanup
- Fixed the Windows desktop validation script to validate the actual local install root
- Changed uninstall so relay-token deletion failures are reported after the rest of cleanup still completes
- Printed the relay-running preflight note in the parent Windows uninstall path before helper handoff
- Made generic `/ws` transport failures stay generic instead of blaming LLAT without proof
- Made PATH removal reporting fail loud instead of claiming success on ignored write errors
- Switched Claude setup-state truth to the real plugin-install record and cleaned broken stale records in the uninstall fast path
- Narrowed uninstall cleanup to managed config/cache artifacts and empty-dir cleanup instead of deleting whole trees blindly
- Stopped completed-setup host overrides from reusing the old healthy state without verifying the new target
- Delayed interactive relay-token writes until the rollback-safe persistence point
- Moved non-interactive snapshots ahead of token writes and added rollback on initial `state.json` save failure

### Verification
- `cd cli && go test ./... -run 'TestInteractiveSetupFreshInstallShowsWizardAndInstallsGeminiSkills|TestInteractiveSetupBackFromRelayInstallLetsUserChangeHost|TestInteractiveSetupRelayTokenFlagCanBackToHostAfterVerifyFailure|TestInteractiveSetupInitialClientPageAllowsRepeatedBack|TestInteractiveSetupContinueAnywayPersistsExplicitURL|TestInteractiveSetupFreshInstallCanPasteExistingRelayToken|TestInteractiveSetupRelayTokenFlagPersistsBeforeVerify'` → passed
- `cd cli && go test ./... -run 'TestInteractiveSetupAlreadyDoneUsesResumeBanner|TestInteractiveSetupCompletedResumePersistsHostOnlyOverride'` → passed
- `cd cli && go test ./... -run 'TestInteractiveSetupContinueAnywayPersistsExplicitURL|TestApplyUninstallTokenPolicyFailsLoudWhenDeleteFails|TestInstallClaudePluginFailsWhenClaudeCLIMissing|TestRemoveInstalledClientsSkipsMissingClaudePluginQuietly|TestClientAppearsInstalledForClaudeIgnoresStaleStateWithoutPluginRecord|TestPersistInteractiveSetupStateRollsBackConfigAndTokenWhenStateSaveFails'` → passed
- `cd cli && go test ./... -run 'TestRunUninstallContinuesRemovingFilesWhenTokenDeleteFails|TestApplyUninstallTokenPolicyFailsLoudWhenDeleteFails|TestRunUninstallReportsConcreteRemovalsAndTokenPolicy|TestRunUninstallShowsPreflightAndRelayStillRunningNote|TestRunUninstallNoopDoesNotClaimRemoval'` → passed
- `cd cli && go test ./... -run 'TestVerifySetupConnectionOnceKeepsTransportFailureGeneric|TestRemoveManagedPathWithReportFailsLoudWhenUnixWriteFails|TestRunUninstallPreservesUnknownConfigAndCacheFiles|TestRemoveInstalledClientsRemovesBrokenClaudeRecordWhenPluginInstallPathIsGone|TestClientAppearsInstalledForClaudeIgnoresBrokenInstallPathRecord|TestInteractiveSetupCompletedResumeRejectsBrokenHostOverride|TestRunSetupNonInteractiveRollsBackWhenInitialStateSaveFails'` → passed

### Findings
- The remaining red full-suite failures after the parity work were mostly fixture drift from the stricter LLAT and Claude-plugin truth rules
- One real product bug surfaced in Step 3: relay-unreachable retries were accidentally prompting twice and downgrading the final issue to `ws_degraded`
- One real uninstall bug remained after the first pass: relay-token deletion could abort the command too early and leave the machine half-uninstalled
- Late skeptical reviews still found a few truthfulness gaps:
  - generic `/ws` transport errors were still over-blaming LLAT
  - PATH cleanup could be reported as removed even when the write failed
  - stale/broken Claude plugin records could survive one uninstall fast path
  - uninstall was too aggressive about deleting whole config/cache trees
  - completed setup could save an unverified host override as “already done”
  - token writes could outlive cancelled interactive setup or early non-interactive save failures

## 2026-03-16: Claude Current Cache Layout Fix

### Completed
- Reproduced the local Claude reinstall failure against the maintainer's real cache layout under `~/.claude/plugins/cache/ha-nova`
- Added focused regression coverage for:
  - Go local install cleanup against the direct-root cache layout
  - shell local installer cleanup against the direct-root cache layout
- Switched both cleanup paths to delete `~/.claude/plugins/cache/ha-nova` as the canonical HA NOVA cache root
- Reproduced the remaining failure with the real `claude` CLI: the Go installed-bundle local directory-marketplace path breaks when the staged plugin payload contains a top-level regular file `ha-nova`
- Changed Go local Claude staging to always use `~/.config/ha-nova/claude-marketplace/ha-nova` and to exclude the top-level bundled `ha-nova` binary from installed-bundle payloads

### Verification
- `cd cli && go test ./... -run 'TestInstallClaudePluginLocalModeClearsCurrentClaudeCacheRoot|TestInstallClaudePluginLocalModeReinstallsAndClearsCache|TestInstallClaudePluginLocalModeRemovesPluginBeforeDeletingCache'` → passed
- `npx vitest run tests/onboarding/install-skills-per-client.test.ts -t 'Claude locally'` → passed
- `bash` repro with real `claude` CLI:
  - staged local payload **without** top-level `ha-nova` file → install succeeded
  - same payload **with** top-level `ha-nova` file → `ENOTDIR ... cache/ha-nova/ha-nova/0.1.12`
- `cd cli && go test ./... -run 'TestInstallClaudePluginStagesInstalledBundleMarketplaceWhenLocalOverrideEnabled|TestInstallClaudePluginStagesDevMarketplaceOutsideRepoRoot|TestInstallClaudePluginLocalModeClearsCurrentClaudeCacheRoot|TestInstallClaudePluginLocalModeReinstallsAndClearsCache|TestInstallClaudePluginLocalModeRemovesPluginBeforeDeletingCache'` → passed
- `npx vitest run tests/onboarding/install-skills-per-client.test.ts -t 'Claude'` → passed

### Findings
- The failure was not a stale bundle; it was a cache-layout mismatch between our cleanup logic and current real Claude installs
- Our previous local cleanup only handled the older nested `cache/ha-nova/ha-nova/...` shape
- Current real Claude installs cache HA NOVA directly under `cache/ha-nova`, including a top-level `ha-nova` file, which explains the observed `ENOTDIR` during reinstall
- The remaining `ENOTDIR` after cache cleanup was not caused by the cache alone; it came from the Go installed-bundle local plugin payload when it included a top-level `ha-nova` binary

## 2026-03-16: Claude Uninstall Project Memory

### Completed
- Compared current Go uninstall against `origin/main` for Claude cleanup scope
- Confirmed plugin + marketplace + cache were already gone after uninstall in the reproduced macOS case
- Found the remaining ghost under `~/.claude/projects/*/memory/`, especially `ha-nova-skills.md` plus HA NOVA-specific `MEMORY.md` sections
- Re-checked the safety model and backed out automatic Claude project-memory deletion entirely
- Final behavior now only detects HA NOVA-related Claude project-memory files and warns explicitly

### Findings
- `origin/main` did not manage Claude project memory either; the new ghost is not explained by plugin uninstall parity alone
- The observed `Unknown skill` after uninstall is consistent with Claude loading stale HA NOVA project memory, not with a still-installed plugin
- Claude project memory is user data; auto-deleting it by default is too risky for a clean uninstall

## 2026-03-16: Final Parity Truth Cleanup

### Completed
- Re-reviewed the current Go runtime against `origin/main` across setup parity, Claude install/update safety, uninstall cleanup, and docs/contracts truth
- Tightened the interactive setup fast-path so a preseeded host + relay-token run skips the LLAT walkthrough, while normal first-run flows still keep the active LLAT guide
- Made the normal GitHub Claude marketplace refresh non-destructive: keep the existing registration unless the new source can actually be used
- Made post-update client sync detect real installed clients from disk so Claude refresh is not skipped when `state.json` drifted
- Added marketplace metadata to the published Claude marketplace manifest so `claude plugin validate` stays warning-free
- Made the uninstall preflight token label reusable so the cross-platform uninstall contract stays truthful on macOS, Windows, and Linux

### Verification
- `claude plugin validate .claude-plugin/plugin.json` → passed
- `claude plugin validate .claude-plugin/marketplace.json` → passed
- `claude plugin marketplace add https://github.com/markusleben/ha-nova && claude plugin install ha-nova@ha-nova` → passed
- `claude plugin update ha-nova@ha-nova` → passed
- `claude plugin remove ha-nova@ha-nova && claude plugin marketplace remove ha-nova` → passed

### Findings
- The last real Claude end-user risk was not install itself, but destructive marketplace refresh if GitHub re-registration failed mid-update
- The last real update-drift risk was relying only on stored state instead of the actual client installation footprint
- The published Claude marketplace manifest was functionally valid before, but still carried an avoidable validator warning
- Tightened Unix PATH cleanup so uninstall now removes only the HA NOVA-managed marker block and leaves unrelated generic `~/.local/bin` exports alone
- Strengthened the onboarding contract layer so Vitest now runs focused Go wizard/readiness/uninstall tests instead of only checking for the presence of Go test names
- Clarified the local Claude validation lane as an intentionally fresh reinstall path; end-user GitHub marketplace update semantics stay unchanged
- Tightened release/docs truth: RC Unix install command now exports `HA_NOVA_VERSION` to `bash`, README says “ships adapters” instead of implying universal readiness, and the architecture doc no longer attributes the context skill to the production SessionStart hook path
- Added install-root rollback for update: the previous runtime now stays recoverable until client sync succeeds, so post-update client-sync failures no longer strand the machine on a half-updated runtime

## 2026-03-16: Final Review Cleanup Pass 2

### Completed
- Re-ran fresh multi-lane reviews after the first “clean” pass and kept fixing until the new findings were actually gone
- Fixed Claude marketplace self-healing so stale local/non-GitHub registrations are replaced by the GitHub marketplace on normal installs and restored on failure
- Corrected `.claude/INSTALL.md` manual repair to match the real safer remove+add marketplace flow
- Corrected `docs/releasing.md` + release contract so fresh-install smoke explicitly means installer -> wizard handoff, not a separate `ha-nova setup <client>` step
- Restored the Windows update-helper test hook after the replace rollback refactor so broad `go test ./...` stays trustworthy
- Split secure-store truth from “missing token” truth across doctor/setup/uninstall messaging
- Fixed Windows uninstall preflight wording to name the installed CLI binary path instead of the Unix symlink path
- Fixed Windows internal uninstall to print already removed items before surfacing a late relay-token deletion failure

### Verification
- targeted Go regression passes for Claude install, secure-store doctor path, Windows replace helper, Windows uninstall helper, and uninstall preflight wording
- `npx vitest run tests/onboarding/release-contract.test.ts tests/onboarding/install-skills-per-client.test.ts tests/onboarding/client-install-docs-contract.test.ts`
- fresh `go test ./...`
- fresh `npm run verify`

### Findings
- The first “clean” pass still missed one broad-suite failure (`windows_update_helper_test`) because the last helper-hook refactor had not been rerun through the full Go suite yet
- The first “clean” pass also still had real truth drift in two places: stale Claude marketplace recovery for end-user installs, and release docs still implying a separate setup command during fresh-install smoke
- Secure-store read failures were still being flattened into “missing token”, which made doctor/setup/uninstall less honest than intended on all three OSes

## 2026-03-17: JQ Escape Hardening

### Completed
- Reproduced the Windows Claude failure shape as a jq parse error on bare `\.` regex escapes
- Added a narrow jq parse retry that normalizes bare `\.` to `\\.` before a second parse attempt
- Rewrote helper-domain skill examples to use split-domain matching instead of regex
- Added a skill contract to keep helper-domain examples off the escape-sensitive regex pattern

### Findings
- The Windows failure was not a relay/network issue; it was jq source failing before execution
- The risky pattern was small and specific enough for a narrow CLI recovery instead of a large jq preprocessor
- Helper-domain matching never needed regex in the first place; replacing it removes the easiest way for models and stale memory to regenerate the bug

## 2026-03-17: Existing Relay Token Verify-First

### Completed
- Added verify-first behavior for existing relay-token flows in the interactive wizard instead of always forcing the LLAT walkthrough
- Added diagnosis-driven repair menus for LLAT-proven, relay-auth-proven, and ambiguous verify failures
- Updated partial resume so token-present + WS-pending now lands on verify first and only returns to token setup when the user explicitly backs out
- Added regression coverage for the reuse-token happy path, partial resume verify-first path, LLAT repair guidance, back-from-verify persistence, and relay-token-flag navigation

### Findings
- The old reuse-token flow was still optimized for first-time setup, not second-device/setup-repair cases
- A single generic repair menu was weaker than the relay signals we already have from `/health` + `/ws`
- Back-navigation tests were still encoded around the previous LLAT-first choreography and had to be updated to the new repair-page rhythm

## 2026-03-17: Cross-Platform Skill Examples

### Completed
- Audited active HA NOVA skills and supporting reference docs for shell-specific JSON handling
- Rewrote complex relay examples to the file-based pattern: `--data-file`, `--body-file`, `--out`, `--jq-file`, and `relay jq --file`
- Removed remaining primary-path guidance that depended on Python, Node, `cat`, heredocs, or `/tmp`
- Added skill contract coverage for inline payloads, `/tmp`, Python, Node, mktemp, shell pipes, heredocs, and glob-sensitive inline jq

### Findings
- The relay/runtime itself was fine; the drift sat mainly in skill examples and model-followable snippets
- Long inline jq extraction filters were the biggest cross-platform footgun because they invited shell quoting failures and ad-hoc fallback tooling
- The cleanest fix was documentation/test convergence, not new CLI features
- One small CLI addition (`relay jq --jq-file`) was still worth it because it removed the last shell-quoted exception from the canonical example path

## 2026-03-17: Early Client Availability Truth

### Completed
- Added a shipped internal client registry at `clients/registry.json`
- Added one shared client-status helper that computes supported-on-os, runtime-detected, configured, attached, and ready-now
- Moved setup client choice to a status-driven list with disabled unavailable clients and `All available clients`
- Changed non-interactive setup to fail before persisting config/state when an explicit target client is not available
- Updated `doctor` to warn on configured-but-missing-runtime / configured-but-not-attached clients
- Updated post-update sync to skip missing runtimes truthfully instead of treating them as successful refreshes
- Added regression coverage for disabled client choices, early setup failure, degraded doctor output, and skipped post-update sync

### Findings
- The real bug was not only in setup choice; install, resume, doctor, and update were all making different assumptions about what “installed” meant
- File-based clients were the main source of false confidence because skill footprints looked installed even when the user could not actually run the client
- A minimal registry plus built-in adapter/runtime logic was enough; pushing runtime-probe mechanics into JSON would have made the design less KISS, not more
- The safe Windows truth is environment-local: if a client runs in WSL, HA NOVA must be set up there too

## 2026-03-17: Client Registry Bundle Parity

### Completed
- Added `clients/registry.json` to install bundles
- Hardened staged bundle validation to require the shipped client registry
- Removed panic-based registry loading from user-facing setup/doctor/update code paths
- Added release-contract coverage that inspects built bundles for the registry file

### Findings
- The Windows crash was not a logic bug in client availability itself; the bundle simply did not include the new registry file
- Panicking on missing runtime metadata was too brittle for install-time and setup-time failures
- The correct fix needed both sides: ship the file and reject future bundles that forget it

## 2026-03-17: Starlight UI Refresh

### Completed
- Switched the shared CLI accent from cyan to amber and made ANSI support a hard requirement for styled/enhanced UI modes
- Slimmed the setup header from a box to a compact title-plus-rule layout
- Tightened enhanced setup menus so choices stay closer to the left edge and disabled reasons render on their own muted second line
- Changed styled warning markers from neutral-looking circles to cautionary markers
- Mirrored the same accent/warning semantics into `install.sh` and `install.ps1`
- Made PowerShell plain-mode installer output use plain output/error streams instead of `Write-Host`-only paths
- Added regression coverage for ANSI gating, compact menu rendering, and installer presentation contracts

### Findings
- The real safety boundary was not “TTY or not” but “TTY plus ANSI/VT support”; otherwise redraw/clear behavior remains risky
- Header chrome was overpowering the actual setup question; slimming the header made the decision text read as the focal point
- Disabled-choice reasons were the main scanability problem in compact menus, not the choice labels themselves

### Follow-up
- Normalized setup page spacing with small shared paragraph/list helpers instead of ad-hoc blank-line sequences
- Added one extra separation break before enhanced menus so explanatory copy and decision UI do not visually run together
- Tightened setup plain-mode gating so non-interactive input always disables enhanced/styled page behavior
- Kept plain step labels semantically complete (`Step n of m - Title`) while stripping only the visual chrome
- Preserved Unix installer client state on reinstall instead of resetting configured clients/modes
- Routed release/update/relay version notices through shared structured warning rendering instead of preformatted raw strings

## 2026-03-17
- Fixed Windows uninstall console cleanup: split visible helper launch from detached temp self-delete cleanup to stop the shell hanging after the final status line.
- Kept the hidden PowerShell self-delete cleanup to avoid visible ping/cmd flashes, but restored visible final uninstall helper output and added an explicit Ctrl+C hint for shells that do not return automatically.
- Moved the Windows Ctrl+C hint from the early parent message to the final helper footer so it appears exactly where the user needs it.
- Setup complete banner now shows the actual installed client list so Windows multi-client runs stay understandable at a glance.
- Gemini flat copies now rewrite sub-skill frontmatter names and `ha-nova:<subskill>` dispatch references to the same namespaced `ha-nova-...` install names the client already sees on disk.
- Hardened shared HA NOVA + entity-discovery skill guidance for Gemini/PowerShell: no `&&`, no external `jq`, default `--jq-file` for non-trivial/domain-count filters, and no automatic switch-domain expansion when the user only asked for lights.
- Re-ran the four onboarding contracts excluded from `npm test`; found one stale Gemini expectation in `tests/onboarding/macos-onboarding-script-contract.test.ts`.
- Fixed that contract to assert the current installed flat-copy name (`name: ha-nova-...`) instead of the source-tree short name (`name: write`).
- Normalized active skill terminology from “add-ons” to “Apps” in the fallback/context skill surfaces, while leaving technical `/addons/*` API language untouched where needed.
- Audited active rollout-facing docs for product-truth drift after the installer/runtime hardening pass.
- Updated `PROJECT.md` to match the shipped Claude marketplace path, current skill inventory, and the actual English-only policy scope.
- Aligned Windows client install docs with the README support matrix: Claude is the smoke-validated lane; Codex/OpenCode/Gemini remain explicit Windows experiments for now.
- Simplified the release checklist so the client matrix verifies wizard-installed integrations first and only uses `ha-nova setup <client>` as a repair/manual path.
- Added docs contracts for `PROJECT.md` and the Windows support wording in per-client install docs.
- Split `cli/commands.go` into responsibility-based files for setup, doctor/probes, update, uninstall, and bundle staging/apply.
- Split `cli/clients.go` into install, Gemini rewrite, Claude plugin, uninstall, and shared FS-copy files; removed dead wrappers that no longer had callers.
- Unified normal and wizard prompt primitives behind shared reader helpers so onboarding nav behavior no longer depends on duplicated implementations.
- Aligned release workflows to the same Node 20 lane as CI and removed the dead GoReleaser signing block because the active release workflow always uses `--skip=sign`.
- Added release-contract coverage for workflow Node parity and the active no-sign release policy.

## 2026-03-18
- Split the setup wizard token phase so Relay Auth Token and Home Assistant Access Token are no longer taught on the same screen.
- Added a dedicated LLAT stage with its own step number, title, and back-navigation instead of inlining the walkthrough under Relay Auth Token setup.
- Repeated the current Relay Auth Token inside the LLAT stage as reminder-only copy so users can recover if they forgot to paste it into NOVA Relay.
- Kept verify-step numbering truthful to the active flow by computing step layout from whether the LLAT walkthrough is actually present.
- Expanded interactive setup coverage to assert the new LLAT step copy and relay-token reminder behavior.
- Tightened the LLAT screen hierarchy again after visual review: primary action first, reminder moved into a muted optional note block, and the intro copy shortened to one direct sentence.
- Fixed the deeper layout bug behind the awkward screenshot: wizard `Press Enter ...` prompts now render as their own indented block with a blank line above, instead of sticking to the preceding paragraph.
- Audited release-facing markdown for the larger Windows-support release and aligned the public story across README, PROJECT, per-client install docs, release docs, PR template, and GitHub release header.
- Kept the support claim honest by separating Windows platform support from the narrower current Windows client-validation matrix.
- Added the missing Windows PowerShell one-liner and ARM64 caveat to the Codex/Gemini/OpenCode install docs so those pages are executable again instead of prose-only.
- Audited the shared update path versus client-specific startup notices before release.
- Confirmed the common updater is the Go runtime path (`ha-nova check-update` / `ha-nova update`) across installs, while the automatic in-session `UPDATE AVAILABLE` banner is still Claude-specific via SessionStart.
- Updated release-facing docs to reflect the newly confirmed Gemini-on-Windows validation and to stop implying that every client gets the same automatic startup update notice.
- Tightened the Linux wording in README/PROJECT so Linux remains listed as a supported lane, but only with explicit build + CI-smoke confidence until a real Secret Service-backed Linux run is done.
- Ran three sub-agent docs/UX reviews on the Linux wording; all agreed the shared `macOS / Linux` install block created an equal-confidence impression before the caveat appeared later.
- Moved the Linux caveat to the exact Quick Start decision point and simplified it to plain product language instead of engineering jargon.
- Considered adding keyless release signing with Sigstore/Cosign, then deliberately dropped it again for this release train because it improves supply-chain posture rather than end-user UX and would add new moving parts to tomorrow's publish path.
