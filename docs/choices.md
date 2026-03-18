# Opinionated Defaults & Design Choices

## 2026-03-18: Release-Day Version Default

- **Today's release default:** bump to `0.2.0`, not `0.1.13`.
- **Reason:** `v0.1.12` already exists on GitHub, and this worktree is a larger cross-platform/runtime/install/update UX release rather than a narrow patch.
- **PR issue linkage:** close only issues that are clearly delivered by this branch. Today that means `#91` (Windows platform support); leave unrelated skill/runtime backlog issues open.
- **Go contract timeout default on CI:** allow cold-run module download time in onboarding contract tests (`go build` up to 120s, targeted `go test` up to 180s).
- **Reason:** fresh GitHub runners download Go modules during these tests; short local-only timeouts turn network/cache warmup into false negatives.

## 2026-03-06: Agent vs Inline Execution in Skills

- **Decision rule:** Skills use agents only when the operation involves 5+ relay calls, multi-step deterministic logic (entity resolution with fallback, payload normalization with singular/plural aliasing), and domain reload. Everything else runs inline.
- **Currently only `ha-nova:write` uses agents** (resolve-agent + apply-agent). All other skills (read, helper, review, entity-discovery, service-call, onboarding) are fully inline.
- **Why this matters:** Helper CRUD was initially built with a `helper-apply-agent.md` (mirroring write's pattern). This was wrong — helper configs are flat (2-4 relay calls, no normalization, no reload). Agent overhead was unjustified. Removed in favor of inline execution.
- **Rule of thumb:** "If `service-call` could do it, it's inline. If it needs what `write` needs, use agents."
- **Documented in:** `docs/reference/skill-architecture.md` → "Agent vs Inline Decision Rule" section.

## 2026-03-06: Understanding Scope

- **Definition of "project understanding"**: Treated as product intent + architecture + runtime surface + onboarding/distribution + test/verification posture, not a line-by-line walkthrough of every shell script.
- **Source priority for understanding pass**: Trusted mandatory reference docs and executable contracts over README prose whenever they differed in detail or maturity.
- **Current-state framing**: Distinguished between documented roadmap capabilities (`/files`, `/backups`, subscriptions) and code implemented today in `src/` (`/health`, `/ws`, `/core`).

## 2026-03-05: Test Infrastructure + Installer + CLI Flags

- **Global test timeout**: Set `testTimeout: 30000` in vitest.config.ts. Onboarding tests spawn bash processes with mock binaries, which can take 5-10s under concurrent load.
- **Fixture-based mocks**: Curl mock routes by URL suffix (`*/health`, `*/ws`, `*/api/discovery_info`) and serves JSON fixture files. Simpler than inline bash strings, easier to maintain.
- **CLI flags as env vars**: this was the shell-era onboarding contract. Current Go-first setup uses native CLI flags on `ha-nova setup`.
- **install.sh location**: Repo root (`install.sh`), per convention for curl|bash installers.
- **Install dir**: `~/.local/share/ha-nova` (XDG data dir convention). CLI at `~/.local/bin/ha-nova`.
- **Non-interactive verify**: When both `--host` and `--token` are provided, relay probe retries are limited to 1 attempt (no interactive wait-for-enter).
- **Update subcommand**: historical shell-era behavior. Current Go-first behavior is bundle refresh through the Go runtime, with git/dev flows treated separately.

## 2026-03-11: Review Check Taxonomy + Threshold Guard Validation

Superseded in structure by the 2026-03-12 catalog split below. Keep the threshold semantics, but do not treat the inline Step 1 block as the long-term catalog home anymore.

- **Primary documentation home for review codes**: `docs/reference/skill-architecture.md`, not `CONTRIBUTING.md`. Architecture owns the taxonomy; contributing only points to it.
- **At that time, the review SSOT was `skills/review/SKILL.md` Step 1**: this was true before the catalog split and is preserved here for historical context only.
- **Threshold detection scope for v1**: direct helper-backed thresholds only (`numeric_state` and direct template comparisons with explicit `input_number.<id>`), no broad fuzzy state-machine inference.
- **Boundary logic is operator-aware**: `>`/`>=` near `min` is risky; `<`/`<=` near `max` is risky.
- **Step mismatch uses the helper lattice relative to `min`**: not `value % step`, to avoid wrong results when the helper range does not start at zero.
- **R-10 stays independent**: H-09 does not auto-emit queue-saturation findings; R-10 is only reported when its own criteria match.

## 2026-03-12: Review Catalog Split

- **Stable facade stays at `skills/review/SKILL.md`**: existing docs, agents, and clients keep one durable review entrypoint.
- **Full rule catalog moves to `skills/review/checks.md`**: workflow and rule catalog are separated, but the rules remain pure Markdown.
- **Current review SSOT split**: entrypoint/workflow in `skills/review/SKILL.md`; detailed rules in `skills/review/checks.md`.
- **Gemini compatibility rule**: companion files required by a flat-copied skill must be copied alongside `SKILL.md` and internal references must resolve after copy.
- **No rule-ID churn during refactor**: `S/R/P/M/F/H` IDs remain stable; only file placement changes.
- **Gemini rewrite strategy**: same-skill companion references collapse to local filenames after flat copy; cross-skill and shared-doc references resolve back to absolute repo paths.
- **Static vs live helper checks**: pre-write draft review stays static (`H-01..H-08` only when no live helper state is available); `H-09`/`H-10` stay in live/post-write review.

## 2026-03-12: Uninstall Must Remove Installer Clone

- **Complete uninstall includes installer-managed paths**: `scripts/onboarding/uninstall.sh` must remove `~/.local/share/ha-nova` and `~/.local/bin/ha-nova`, not just skills/config/cache.
- **Reason**: `install.sh` creates a separate local clone plus CLI link; leaving them behind is a partial uninstall and breaks clean-state reinstall/update testing.

## 2026-03-12: Global CLI After Installer

- **Canonical post-install command**: use `ha-nova ...`, not `npx ha-nova ...`.
- **Why**: the one-line installer provisions a local CLI in `~/.local/bin`; `npx ha-nova` is only valid in repo-local/dev contexts and fails for normal installer users.
- **PATH default**: `install.sh` writes `export PATH="$HOME/.local/bin:$PATH"` to the detected shell startup file and prints a current-shell fallback command when the shell still has the old PATH.

## 2026-03-12: Gemini Skill Root Isolation

- **Gemini user install root**: use `~/.gemini/skills`, not `~/.agents/skills`.
- **Why**: Codex already owns `~/.agents/skills/ha-nova` as a symlinked tree; placing Gemini flat copies in the same discovery root causes duplicate skill listings in Codex.
- **Migration rule**: Gemini install cleans up legacy flat copies from `~/.agents/skills` but leaves the Codex symlink intact.

## 2026-03-12: dev-sync KISS Rule

- **Single install primitive**: `scripts/onboarding/install-local-skills.sh` is the source of truth for Codex, OpenCode, and Gemini refreshes.
- **`dev-sync` scope**: keep `scripts/dev-sync.sh` only as a thin wrapper plus Claude-special cache sync, not as a second install implementation.
- **Reason**: avoids duplicate refresh logic and keeps file-based client behavior aligned with normal setup installs.

## 2026-03-12: HA NOVA Skill Namespacing

- **Sub-skill IDs are product-scoped**: use `ha-nova-*` for sub-skill folder names and frontmatter names instead of generic IDs like `read` or `helper`.
- **Reason**: shared discovery surfaces like Codex `/skills` must make ownership obvious and avoid ambiguous generic skill names.
- **Matching rule**: folder name and frontmatter `name` stay identical for each sub-skill (`skills/ha-nova-read/SKILL.md` -> `name: ha-nova-read`).
- **Router references**: the context skill dispatch table uses the full namespaced handles (for example `ha-nova:ha-nova-read`) so Claude/plugin routing stays aligned with the canonical skill IDs.

## 2026-03-12: Relay CLI Missing-Setup UX

- **Missing onboarding config is a setup-state error, not a raw file-path error**: `scripts/relay.sh` should tell the user HA NOVA is not set up yet and point them to `ha-nova setup`.
- **Reason**: after `install-local-skills.sh`, the relay CLI exists before onboarding is completed, so a raw `missing ~/.config/ha-nova/onboarding.env` message is technically true but poor UX.

## 2026-03-12: Gemini Flat-Copy Path Contract in CI

- **Cross-platform absolute-path contract**: flat-copy tests must accept any absolute repo path, not just macOS `/Users/...`.
- **Reason**: installer rewrite behavior is path-root agnostic; GitHub Actions validates the same rewrite under Linux runner paths like `/home/runner/work/...`.
- **Hermetic installer contracts**: onboarding tests that execute `install-local-skills.sh` or `update` must mock `claude` instead of touching a real local CLI.
- **Reason**: local machine state must not decide whether installer/update contracts pass or hang.

## 2026-03-12: Installer Bash Login PATH + dev-sync Legacy Gemini

- **Installer PATH target for Bash users**: prefer `~/.bash_profile`, then `~/.profile`, instead of `~/.bashrc`.
- **Reason**: macOS Bash sessions are commonly login shells; writing only `~/.bashrc` can leave `ha-nova` unavailable in new terminals.
- **`dev-sync` migration rule**: Gemini refresh must honor both the current `~/.gemini/skills/...` marker and the legacy `~/.agents/skills/...` marker.
- **Reason**: updater and dev-sync should not disagree about which legacy Gemini installs are still supported during migration.

## 2026-03-12: Post-Merge Codex Findings Follow-Up

- **`dev-sync` client detection**: Codex and OpenCode refresh only when their install roots are live symlinks.
- **Reason**: a plain directory in `~/.agents/skills/ha-nova` can be a legacy Gemini artifact; only a symlink reliably identifies Codex/OpenCode.
- **Gemini setup completeness**: Gemini readiness checks require every markdown file copied by the flat installer, not only `SKILL.md`.
- **Reason**: setup resume must not claim Gemini is already installed when companion docs like `checks.md` are missing.

## 2026-03-12: Installer TTY Handling for `curl | bash`

- **No global stdin swap**: the one-line installer must not `exec < /dev/tty`.
- **Reason**: when bash reads the script from a pipe, replacing stdin globally can leave the shell reading from the terminal after the script body ends, so users see a blinking cursor instead of a clean return.
- **Interactive rule**: use `/dev/tty` only for explicit prompt reads and for the setup handoff subprocess.

## 2026-03-12: Codex Review Signal Channels

- **Review flow must read three Codex channels**: PR issue reactions, inline PR review comments, and PR issue/discussion comments.
- **Reason**: Codex can emit the final clean result as a summary comment like `Codex Review: Didn't find any major issues.` even when there is no new inline comment or reaction yet.

## 2026-03-12: Setup Must Not End Green on Degraded HA WS

- **Setup outcome split**: if relay health is reachable but upstream Home Assistant WS is still degraded after retries, end with `Setup incomplete`, not `Setup complete!`.
- **Recovery path**: setup itself owns the first recovery loop; `ha-nova doctor` is a later re-check command, not the primary in-setup recovery.
- **Diagnosis wording**: only claim `ha_llat` specifically when the `/ws` probe proves it; otherwise describe generic upstream HA WS failure.

## 2026-03-12: Full-Project Understanding Pass Scope

- **Definition of done for this pass**: product intent + mandatory reference docs + runtime architecture + skill/onboarding/distribution paths + live verification of the safety net.
- **What this intentionally is not**: a line-by-line explanation of every shell helper or every markdown rule entry.
- **Source priority**: executable contracts and current runtime code outrank older prose when details differ.
- **Verification bar**: do not call the project "understood" from static reading alone; also run `npm run typecheck` and `npm test`.

## 2026-03-12: HALMark Adoption Scope

- **Adoption model**: use HALMark as an inspiration source for a small HA NOVA rule-and-test subset, not as a vendored framework or second source of truth.
- **First-pass scope**: prioritize `FG-18`, `FG-24`, `FG-08`, `FG-15`, and `FG-17`.
- **Deferred items**: keep `FG-09` and `FG-13` as softer follow-ups; do not pull `FG-10` into the first pass.
- **Architecture rule**: all adoption lives in skills, review checks, docs, and contract tests; nothing moves into the relay runtime.
- **Harness rule**: do not integrate the current HALMark harness because the public runner/case files are still effectively empty.

## 2026-03-12: HALMark First-Pass Execution Defaults

- **Invalid-premise coverage**: enforce `FG-18` in the shared HA context plus write/guide skill surfaces; keep the router as the canonical baseline and add only the minimal local wording each skill needs.
- **Scope-creep wording**: express `FG-08` concretely in write/refactor flows (`no unrelated structure/alias/format rewrites`, `no unrelated config changes`), not as vague stewardship slogans.
- **Delete safety bar**: express `FG-17` as consumer visibility before confirmation plus post-delete verification before claiming success.
- **Templated-event adoption**: add exactly one new review rule (`R-16`) and one linked anti-pattern note in template guidance for `FG-15`.
- **Attribution style**: use a lightweight repository acknowledgment for HALMark/Nathan Curtis after nontrivial rule adoption lands; do not mirror external specs in-tree.

## 2026-03-12: HALMark Verification Scope

- **Smoke-test style**: add scenario-based contract tests, not live E2E runs.
- **Reason**: the adopted HALMark changes live in skill/docs contracts, so the strongest low-cost verification is scenario-to-contract coverage for those exact additions.
- **Scope limit**: verify only the HALMark behaviors added in this task (`FG-18`, `FG-24`, `FG-08`, `FG-17`, `FG-15`), not unrelated legacy skill behavior.

## 2026-03-12: HALMark Live E2E Fixture Placement

- **HALMark live scenario catalog path**: store the Codex E2E scenario JSON under `tests/fixtures/`, not `scripts/e2e/`.
- **Reason**: the shared scenario harness treats `scripts/e2e/` usage as a helper-script smell; putting the prompt fixture there created a false positive during the real `FG-17` run.

## 2026-03-14: Windows Bundle Installer + Unified End-User Distribution

- **End-user distribution model**: release bundles first, not `git clone`.
- **Reason**: the simplest supported install path must work for non-terminal-savvy users and must not require local repo state.

- **Windows entrypoint**: PowerShell one-liner via `install.ps1`.
- **Reason**: this is the most natural zero-context install surface for Windows users and matches the UX priority of “one command, then it just works”.

- **Windows runtime dependency**: no Git Bash dependency in the end-user path.
- **Reason**: the Go-first runtime made native Windows bootstrap/setup viable without carrying shell runtime assumptions into the product contract.

- **HA NOVA end-user prerequisites**: no local Node/npm requirement.
- **Reason**: Node is a repo/dev dependency here, not something HA NOVA itself should force onto normal installers.

- **Bundle archive layout**: one top-level `ha-nova/` directory inside each archive.
- **Reason**: macOS shell install, Windows PowerShell install, and shell self-update all need a predictable extraction root.

- **Public relay command contract**: use `ha-nova relay ...` everywhere public-facing. Raw `~/.config/ha-nova/relay` paths are legacy-cleanup territory, not part of the supported runtime contract.
- **Reason**: the Go CLI is now the stable product interface across macOS, Windows, and Linux; file-path relay invocations are compatibility baggage, not a supported product surface.

- **Windows file-client install mode**: copy fallback by default instead of relying on symlink support.
- **Reason**: Windows symlink behavior varies by privileges and developer mode; copying is the safer no-surprises default.

- **Windows ARM handling for release assets**: prefer `amd64` relay/bundle assets for now.
- **Reason**: current published Windows release support is `amd64`; using the x64 asset is the practical compatibility default until native Windows ARM bundles exist.

## 2026-03-14: Go-First Runtime Cutover

- **Unix installer behavior**: rerunning `install.sh` replaces the installed bundle in-place without an update/reinstall prompt.
- **Reason**: the new bootstrap must stay one-shot and predictable; update semantics now belong to `ha-nova update`, not to shell prompts.

- **Windows PATH contract**: add the install-root `ha-nova.exe` directory to the user `PATH`; do not create a separate Windows launcher.
- **Reason**: PowerShell resolves `ha-nova` directly to `ha-nova.exe`, which removes the fragile self-deleting `.cmd` layer and keeps one executable of record.

- **Legacy shell scripts**: if they remain in the repo, they are dev-only helpers and not part of the end-user runtime contract.
- **Reason**: the hard cut removes functional backward-compatibility from the supported product path without requiring a repo-wide shell purge on the same day.

- **Steady-state bundle contents**: ship `ha-nova[.exe]`, `skills/`, `.claude-plugin/`, `docs/reference/`, `version.json`, and `bundle.json`; exclude product shell scripts and installers from release bundles.
- **Reason**: new installs must prove that the runtime is Go-first and not secretly dependent on the old shell layer.

## 2026-03-14: Review Fix Follow-Through

- **Bundle update safety**: self-update must reject archives without a valid `ha-nova/` bundle root and `bundle.json`.
- **Reason**: updater inputs come from release assets; accepting arbitrary extraction roots turns malformed or hostile archives into install corruption risk.

- **Archive extraction policy**: reject absolute paths and traversal entries during tar/zip extraction.
- **Reason**: remote bundle extraction must never be able to escape the staging directory.

- **Uninstall migration cleanup**: remove legacy client skill trees even when current install state is missing or incomplete, including stale `.claude/skills/ha-nova*`.
- **Reason**: uninstall must clean real-world upgraded machines, not only perfectly tracked fresh Go installs.

## 2026-03-14: Hard Cut Legacy Compatibility

- **Legacy support model**: no functional backward-compatibility in the main runtime path.
- **Reason**: the installed product contract must stay small and predictable; pre-Go recovery is cheaper and safer as a separate cleanup flow than as an in-place migration story.

- **Legacy recovery surface**: dedicated `legacy-uninstall.sh` / `legacy-uninstall.ps1` one-liners only.
- **Reason**: old installs should be cleaned explicitly before reinstall, not mixed into `ha-nova update` or normal bootstraps.

- **Fresh install config contract**: `config.json` + `state.json` only; no `onboarding.env` fallback or import.
- **Reason**: JSON config/state is now the single source of truth for the Go runtime.

- **Windows binary authority**: install-root `ha-nova.exe` is both the authoritative runtime and the public command target.
- **Reason**: removing the extra launcher is the smallest robust fix for Windows uninstall/update behavior.

- **Windows uninstall finalization**: let a short-lived helper finish removing the install root after `ha-nova uninstall` returns.
- **Reason**: the running Windows executable cannot safely delete its own install directory in-process; the helper removes the visible path a moment later without reintroducing a wrapper layer.

- **Local RC artifact order**: always run `goreleaser build --snapshot --clean` before `build-install-bundle.sh`.
- **Reason**: install bundles package the existing `dist/` binaries; rebuilding only the bundle archive can silently ship stale executables into private RC tests.

- **Bundle integrity rule**: install/update require sidecar SHA-256 verification plus `bundle.json` OS/arch/binary validation before replace.
- **Reason**: corrupted or wrong-platform release assets must fail before they can touch a working install.

## 2026-03-14: Contributor Runtime Contract Cleanup

- **Contributor verify contract**: `npm run verify` is the single repo-wide contributor check and must cover TypeScript plus Go CLI tests.
- **Reason**: after the Go-first cut, Node-only verification no longer protects the main runtime.

- **Shell-era npm aliases**: keep shell onboarding and local skill sync only behind `dev:*` script names.
- **Reason**: the package surface should not advertise a second end-user path after the hard cut.

- **Dev smoke/e2e config source**: repo support scripts should prefer `ha-nova doctor` and `config.json` over `macos-onboarding.sh env` / `onboarding.env`.
- **Reason**: contributors should exercise the same installed runtime contract that end users rely on.

- **Pre-release gate**: public releases require an explicit RC matrix across fresh install, update, uninstall, legacy cleanup, and per-client setup.
- **Reason**: unit/contract tests alone are not enough for installer/bootstrap confidence across macOS, Windows, and Linux.

- **Release workflow split**: keep three layers only: normal CI, manual RC, final tagged publish.
- **Reason**: this is the smallest structure that separates confidence-building from public release without workflow sprawl.

- **GitHub RC scope**: smoke the built bundles directly in GitHub; reserve the real installer path for the manual RC matrix.
- **Reason**: this avoids adding installer override complexity just to test unpublished artifacts.

## 2026-03-14: Final Release Approval Gate

- **Final publish protection**: require a GitHub `production` environment approval before the tagged release job can publish assets.
- **Reason**: contributors may help with CI and RC work, but public release must stay maintainer-gated even if a tag exists.

- **Protection baseline**: enable `required reviewers`, `prevent self-review`, and `v*` tag protection.
- **Reason**: the smallest reliable release-control model is one protected environment plus protected version tags; anything looser makes accidental or unauthorized publish too easy.

- **Single-maintainer fallback**: if the repo currently has only one admin maintainer, keep `production` for future release secrets but rely on protected `v*` tags as the immediate hard release gate until a second maintainer exists.
- **Reason**: `required reviewers` plus `prevent self-review` are only meaningful when someone else can actually approve the release.

- **Current GitHub tag bypass implementation**: use the working repository-role bypass that GitHub accepts for this repo today, not a direct user bypass.
- **Reason**: the direct `User` bypass looked cleaner for a single maintainer, but GitHub reported `current_user_can_bypass: never`; the repository-role bypass is the verified working configuration.

- **Environment restriction strategy**: keep `production` simple and let `v*` restriction live in the dedicated tag ruleset instead of the environment branch/tag policy.
- **Reason**: the environment policy API proved awkward for tag matching, while the tag ruleset already provides the exact hard guard we need with less ambiguity.

## 2026-03-14: macOS Fresh-Home Smoke

- **Pre-release macOS smoke method**: for unreleased refactor work, validate the end-user lifecycle from a locally built install bundle inside a fresh temporary `HOME`, backed by tiny local HA/Relay HTTP servers.
- **Reason**: `install.sh` currently targets GitHub release assets only, so the cleanest way to exercise the current unreleased runtime is a local bundle install plus real CLI setup/doctor/update/uninstall against controlled endpoints.

- **Setup CLI arg-order support**: accept `ha-nova setup all --host ... --relay-token ... --non-interactive` in addition to the flag-first form.
- **Reason**: Go's default flag parsing stops at the first positional argument; without a tiny normalization step, a natural user command shape falls back into interactive prompts.

## 2026-03-14: RC Prerelease Publish

- **RC publish strategy**: extend the existing `release-candidate.yml` with an optional prerelease publish mode instead of adding a second RC publish workflow.
- **Reason**: one RC workflow with an opt-in public bundle publish path is the smallest way to test the real one-liner against GitHub assets without bloating release automation.

- **RC public asset scope**: publish install bundles and checksum sidecars only.
- **Reason**: the installers and self-update path consume the bundle assets directly; raw binaries are not required for the public one-liner test.

- **RC local parity command**: provide one canonical repo command, `npm run release:rc:local`, for the local artifact rehearsal path.
- **Reason**: the release path should stay as DRY as contributor verification; maintainers need one obvious command for the local RC build step.

- **Workflow duplication stance**: accept small duplication between `release-candidate.yml` and `release.yml` instead of introducing reusable-workflow indirection right now.
- **Reason**: two explicit workflows are easier to reason about than a shared abstraction at the current project size; KISS beats maximal DRY here.

- **Installer smoke timing**: keep real public-installer smoke as post-publish confirmation in `release.yml`; treat RC + production approval as the blocking pre-publish gate.
- **Reason**: the true public installer path depends on published release assets; reproducing that pre-publish would add more complexity than value right now.

## 2026-03-14: Rebase Integration and Dev Wrappers

- **Remote integration strategy**: rebase the Go-first runtime work onto `origin/main` and keep the short skill-directory rename from PR `#96` instead of force-pushing over it.
- **Reason**: the rename is now canonical repo state, and only three files had real conflicts; resolving the overlap was smaller and safer than trying to replay the rename again later.

- **Repo/dev helper strategy**: keep `scripts/update.sh` as a pure Go-runtime shim, and do not install a copied `~/.config/ha-nova/update` from `install-local-skills.sh`.
- **Reason**: the copied shim has no reliable install state for repo/dev Gemini flat copies; pretending it is a working self-update path adds breakage, not convenience.

- **Repo/dev compatibility wrappers**: for `install-local-skills.sh`, install repo-local wrappers for `relay` and `version-check` that call `scripts/onboarding/bin/ha-nova`.
- **Reason**: repo/dev installs should keep legacy helper entrypoints working without downloading release assets or requiring a separately installed product runtime.

## 2026-03-15: Desktop Validation Strategy

- **Windows validation split**: treat Windows headless/SSH and Windows desktop/RDP as separate proof lanes.
- **Reason**: installer/version/uninstall are valid over SSH, but `setup` depends on Windows credential-store behavior that must be validated in a real desktop logon session.

- **Support claim rule**: do not promise a Windows client publicly until it passes the desktop validation lane.
- **Reason**: file installation logic alone is not enough proof; native client availability and real desktop-session behavior must match the published support matrix.

## 2026-03-15: Safe Test System

- **Default verification boundary**: `npm test` and `npm run verify` must be host-safe by construction.
- **Reason**: maintainer workstations must never get surprise browser launches or secure-store writes from routine verification.

- **Desktop validation boundary**: macOS desktop and Windows desktop proof stay required before release, but only behind explicit manual commands.
- **Reason**: release confidence still needs real machine proof, but that proof must be opt-in rather than part of the default test path.

- **Legacy shell test stance**: old shell onboarding flows must stop defining default quality gates.
- **Reason**: trying to fully harden every historical shell path adds risk and complexity; the safer move is to keep them out of default verification.

- **Guard strategy**: use one explicit no-browser guard and one file-based test keyring override instead of building a larger test sandbox.
- **Reason**: this is the smallest shared mechanism that makes both Go runtime tests and explicit desktop helpers predictable without adding framework overhead.

- **Desktop-side effect rule**: `HA_NOVA_NO_BROWSER=1` also suppresses clipboard writes, not only browser launches.
- **Reason**: clipboard mutation is still a real host-side desktop effect; the safe lane must leave the maintainer desktop untouched.

- **Insecure test keyring rule**: plaintext test-token files require both `HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1` and `HA_NOVA_TEST_KEYRING_FILE=<path>`.
- **Reason**: a leaked file-path env var alone must not silently downgrade real secure-token storage outside explicit test runs.

- **macOS desktop lane freshness rule**: `test:desktop:macos` must rebuild and serve fresh private RC bundles itself.
- **Reason**: an explicit desktop proof command that can silently consume stale artifacts is not a trustworthy release signal.

- **Windows validation sequencing**: run Windows headless over SSH first and prepare the desktop runner on the VM before any RDP proof.
- **Reason**: this keeps installer/version/uninstall evidence automated and leaves only the true credential-store desktop proof for the interactive lane.

## 2026-03-15: Private RC Credential Isolation

- **Private RC token store isolation**: desktop-validation helpers use `HA_NOVA_KEYRING_SERVICE` to isolate secure-token storage from a maintainer's real HA NOVA install.
- **Reason**: macOS and Windows secure stores are per-user, not per-temp-`HOME`; private RC tests must not overwrite real credentials on the maintainer machine.

- **macOS keychain targeting**: the Go runtime must use the same default keychain resolution and default item ACL behavior as the legacy shell path.
- **Reason**: explicit ACL tweaking during `security add-generic-password` caused interactive “change access rights” prompts that the old release did not trigger. Shared default-keychain semantics are the safer long-term rule.

- **macOS keychain access control**: trust `/usr/bin/security` explicitly when writing the relay token.
- **Reason**: this is the smallest safe change that removes interactive authorization blockers in the CLI's secure-token write path without broad `-A` access.

- **Claude desktop proof source**: validate Claude plugin registration through `.claude/plugins/installed_plugins.json`, not CLI output text.
- **Reason**: a successful plugin install can be silent; file state is the stable proof.

- **Gemini install mapping**: Go Gemini installs must read the current short source skill dirs (`write`, `review`, etc.) and publish them as `ha-nova-*` flat dirs under `.gemini/skills`.
- **Reason**: the repo no longer uses `skills/ha-nova-*` source dirs; keeping the old names in the Go runtime silently drops Gemini companion skills.

- **macOS private-RC harness ports**: allow explicit `--relay-url` during `ha-nova setup` and use non-default mock ports in the helper scripts.
- **Reason**: the desktop-validation lane must not depend on fixed local ports `8123/8791`; free-port mocks are simpler and more reliable than forcing the workstation to be empty.

- **macOS temp-home keychain interpretation**: treat the earlier keychain failure as a harness diagnosis, not a release blocker.
- **Reason**: the private-RC helper isolates credentials via `HA_NOVA_KEYRING_SERVICE` / `HA_NOVA_TEST_KEYRING_FILE`; we no longer need product code to pin `login.keychain-db`.

## 2026-03-15: Safe Test Architecture

- **Default verification contract**: `npm run verify` must become host-safe and exclude real browser/keychain/desktop shell execution.
- **Reason**: the maintainer machine must never see surprise browser or secure-storage side effects from the default gate.

- **Validation lane split**: keep only four lanes: `verify-safe`, `macos-desktop`, `windows-headless`, `windows-desktop`.
- **Reason**: this is the smallest structure that preserves release confidence without adding workflow bloat.

- **Legacy shell stance**: legacy macOS shell onboarding stays out of the default gate and is treated as an explicit compatibility/manual lane only.
- **Reason**: it is the highest-risk host-side-effect surface and should no longer define normal verification.

- **Windows uninstall contract**: parent process only starts the helper; all token/PATH/config cleanup happens in the helper after the install root is discarded.
- **Reason**: this keeps Windows uninstall honest and avoids partial state deletion when the background helper fails after launch.

## 2026-03-15: Setup Prompt Parity

- **Interactive setup client prompt**: the Go runtime must keep the old release's numbered client-selection list as the minimum UX bar.
- **Reason**: the plain `Client (claude/codex/opencode/gemini/all)` free-text prompt is a regression against the current release wizard and is not acceptable for the Windows/macOS userflow.

- **Setup parity bar**: the client list alone is not enough; the full interactive setup flow must return to at least the old release's phased wizard quality.
- **Reason**: a normal userflow needs guided phases, resume/status, verification loops, and explicit completion states, not just a prettier first prompt.

- **Interactive/non-interactive split**: keep the automation path fail-fast and stable, but move the richer wizard UX into the interactive setup path only.
- **Reason**: this restores user-facing setup quality without destabilizing the safe test lanes and explicit desktop validation runners.

- **Prompt reader strategy**: the interactive wizard must reuse one buffered stdin reader across all prompts.
- **Reason**: creating a new `bufio.Reader` per question drops buffered lines in piped/automated runs and breaks the prompt chain.

## 2026-03-15: Setup Wizard Parity Follow-up

- **Resume-complete banner**: the Go wizard now uses a dedicated `Everything is already set up!` banner for fully completed resume paths.
- **Reason**: a resumed/ready system must not look like a fresh install just finished; the old release distinguished those states.

- **Step 2 explanation parity**: the Go wizard now explains both required tokens during the secure-access step.
- **Reason**: later relay/WS troubleshooting only makes sense if the user was first taught the relay token and the Home Assistant access token as separate concepts.

- **WS readiness parsing rule**: `ha_ws_connected` is authoritative; generic relay `status` must not be treated as proof that Home Assistant WS is connected.
- **Reason**: inferring WS readiness from a generic status field produced false “already done” states and hid degraded relay setups.

## 2026-03-15: Canonical Setup Host

- **Chosen setup host wins**: once the user picks a Home Assistant host/URL during setup, that selection becomes the canonical endpoint for the rest of the wizard.
- **Reason**: verification, browser links, saved config, and derived relay URL must not silently drift back to an older discovered/default host.

- **Relay URL derivation rule**: a fresh host/HA URL choice recomputes `relay_base_url`, unless the user explicitly supplied a relay override.
- **Reason**: keeping a stale relay URL after a host change creates split-brain setup state and breaks the normal userflow.

- **Override timing rule**: setup endpoint overrides must apply before resume/health detection and before the early `already set up` exit.
- **Reason**: a completed install still needs to accept deliberate host/relay changes; otherwise the wizard ignores the user's new canonical endpoint.

- **Wizard test isolation rule**: prompt-driven setup tests must not rely on ambient localhost services.
- **Reason**: a real mock service on `:8791` can mask a bad derived relay URL and create false-green wizard evidence.

- **ARP discovery portability rule**: parse `arp` output in Go instead of shelling through `sh|sed|head`.
- **Reason**: Windows cannot be treated as a shell-pipeline environment; host discovery must stay cross-platform in the Go runtime.

- **Wizard ordering parity rule**: Home Assistant discovery and the host prompt happen before the Step 1 app-install guide.
- **Reason**: the old release resolved the canonical HA endpoint early so every later deeplink/check stays anchored to the same user-confirmed host.

- **Discovery parity scope**: setup discovery parity stops at visible progress plus candidate-based detection; no active subnet scan is added.
- **Reason**: the old release did not perform a subnet scan either; the missing parity was user-visible feedback, not a heavier network scanner.

- **Host-check parity rule**: after the user enters a Home Assistant address, setup must visibly run a connection check before moving on.
- **Reason**: the old release showed `Checking connection to Home Assistant...`; without that feedback the new Go flow feels instantaneous and less trustworthy even when it is checking.

- **WS verification parity rule**: when relay health says `ha_ws_connected=false`, setup must run the old `/ws` ping fallback before declaring the setup degraded.
- **Reason**: the old release already treated a successful `/ws` ping as proof that upstream WS was actually fine; without that, the Go wizard can show a false degraded state.

- **LLAT diagnosis rule**: if the relay `/ws` response shows `LLAT is required`, setup must say that explicitly instead of only showing a generic WebSocket failure.
- **Reason**: users need the concrete failing field (`ha_llat`) to recover; the old release already surfaced that cause and action.

- **WS readiness ownership**: keep the relay `/health` endpoint as a passive snapshot and make the CLI own effective readiness interpretation.
- **Reason**: the runtime uses lazy upstream WS connection; changing `/health` semantics would add wider surface-area risk, while setup/doctor only need the old `/health` + `/ws` fallback logic back.

- **Readiness truth scope**: the same readiness verdict must drive setup Step 3, resume-state detection, and `ha-nova doctor`.
- **Reason**: onboarding and later skill use depend on the same local state. One path saying "ready" while another says "broken" destroys trust and makes client support on macOS/Windows look flaky.

- **Proof rule for lazy WS**: the decisive false-negative proof is `relay health => ha_ws_connected=false` combined with a successful `/ws` ping.
- **Reason**: `/health` alone can lag behind the first real WS use; `/ws` is the first point that proves whether the relay can actually talk to Home Assistant.

- **Windows relay CLI scope split**: inline `relay ws -d ...` handling is a separate CLI diagnostic bug, not the readiness bug itself.
- **Reason**: readiness parity must be fixed regardless, but Windows diagnostics are only clean once inline JSON payloads are either fixed or explicitly diagnosed.

- **Inline relay payload rule**: inline relay payloads must fail locally unless they are real JSON objects/arrays after light shell-wrapper normalization.
- **Reason**: later skill calls on Windows cannot depend on the relay app returning `INVALID_JSON` for shell-mangled payloads; the CLI must catch bad inline payloads earlier and accept the common wrapped forms it can normalize safely.

- **Cross-platform skill contract**: active HA NOVA skills must treat file-based relay payloads (`--data-file`, `--body-file`, `--out`) as the default cross-platform contract, not bash-only inline JSON plus Unix temp paths.
- **Reason**: onboarding can be fully green while later skill calls still fail on Windows if the markdown skill layer keeps teaching shell-specific relay usage.

- **Claude source-of-truth rule**: supported Claude installs must resolve HA NOVA plugin content from the installed/tested payload, not a drifting GitHub repo source.
- **Reason**: otherwise Windows/macOS Claude users can pass onboarding and still load stale skill/plugin content that does not match the validated bundle.

- **Mock reported-version rule**: the private desktop-validation mock must talk about a reported `/health.version`, not a "relay version".
- **Reason**: the project now has separate App and skill/bundle version lines; calling the mock field "relay version" makes the fake `/health` payload sound like the real Home Assistant App version.

- **Cross-platform skill command rule**: active HA NOVA skills must document file-based relay calls (`--data-file`, `--body-file`, `--out`, `--jq`) as the canonical path.
- **Reason**: onboarding can be green while later skill calls still fail on Windows if the markdown layer keeps teaching shell-specific quoting, `/tmp`, pipes, and inline `-d` payloads as the normal contract.

- **Client-doc scope rule**: keep README product-level; put Claude/OpenCode Windows-specific prerequisites only into their client install docs.
- **Reason**: users need the real client caveats, but HA NOVA's main README should not turn into a third-party installer migration document.

- **Claude marketplace split**: production installs use the GitHub marketplace URL; private RC/dev validation uses an explicit local override.
- **Reason**: end users need Claude’s normal Git-backed update behavior, while validation must prove the exact local payload under test.

- **Claude refresh rule**: if `ha-nova@ha-nova` is already installed in Claude, use `claude plugin update`, not `claude plugin install`.
- **Reason**: re-pointing the marketplace alone does not prove Claude refreshes an existing cached plugin payload; the explicit Claude-native refresh verb is required to flush stale skills after setup/update.

## 2026-03-15: Windows Uninstall Truth + Step-4 Progress

- **Claude uninstall rule**: skip Claude CLI removal quietly when the plugin is already absent; fail loud when a present plugin cannot be removed for a real reason.
- **Reason**: uninstall must not emit false warnings for already-clean machines, but it also must not claim success when Claude integration cleanup genuinely failed.

- **Windows uninstall completion signal**: keep the async helper model, but have the helper print a final `HA NOVA removed` line on success.
- **Reason**: Windows still needs the helper to delete the running install root, but users need a clear end-state signal.

- **Windows uninstall ownership**: on Windows, client integration removal runs inside the helper, not in the parent command.
- **Reason**: if helper launch fails, the system must not be left half-uninstalled before the real uninstall even starts.

- **Setup Step-4 progress contract**: client installation in the wizard must always show visible progress feedback.
- **Reason**: parity with the old release and better UX; long-running work must never look frozen.

## 2026-03-15: Token Reuse Wizard + Page Flow

- **Resume token rule**: a rerun with a saved local relay token skips the token-choice page and goes straight back to verification.
- **Reason**: the new token-reuse UX is for fresh-device setup; same-device resume should stay as fast as the old release and must not force extra prompts before retrying verification.

- **Fresh token choice defaults**: when no local relay token exists, default to `Generate a new token`; when one exists on the current device, default to `Keep saved token`.
- **Reason**: first-run onboarding should stay low-friction, while already-configured devices should preserve the stable local token by default.

- **Wizard navigation contract**: interactive setup pages accept `back` and `exit`, while the first client page accepts `exit` and treats `back` as a no-op stay-on-page.
- **Reason**: users need to correct earlier input without restarting the wizard, but there is no meaningful page before the first client selection.

- **Page-flow parity rule**: each interactive wizard page clears and redraws on a real TTY only.
- **Reason**: real terminals should behave like paged wizard screens, while tests and non-interactive pipes need stable append-only output.

- **Wizard invalid-choice rule**: client and token selection pages must reprompt on unrecognized input instead of silently falling back to the default.
- **Reason**: silent coercion on security-sensitive or high-impact wizard choices can select the wrong client or token path without the user noticing.

- **Local validation harness rule**: keep one small foreground developer harness in `scripts/dev/` that rebuilds bundles, serves them, and optionally starts the fake HA/relay mock.
- **Reason**: the recurring failures were stale bundles, wrong filenames, and dead local servers, not missing automation of the actual userflow.

## 2026-03-15: Final Review Hardening

- **Non-interactive setup parity**: flag-driven `ha-nova setup` must normalize host/URL inputs through the same host-resolution contract as the interactive wizard.
- **Reason**: `--host http://...` must not persist broken derived URLs just because the user chose flags instead of the wizard.

- **Windows shell/go token interop**: on Windows, the Go runtime writes Credential Manager and a mirrored legacy DPAPI file; reads accept either source.
- **Reason**: the Go-first runtime stays on secure native storage, but legacy Git-Bash helpers must still see the same token during mixed migration/testing paths.

- **Harness truthfulness rule**: local validation harnesses and private-RC mocks derive their reported version from the served bundle, not from repo metadata defaults.
- **Reason**: mock `/health.version` must describe the artifact under test, otherwise readiness and same-version update proofs can drift.

- **Windows client-doc contract**: Codex/Gemini/OpenCode docs must stay product-focused and platform-aware, but must not imply broader native Windows proof than we actually validated.
- **Reason**: installer support and client-runtime support are different promises.

## 2026-03-15: Claude Marketplace Remote vs Local

- **Claude production source rule**: default installed-bundle onboarding registers the Claude marketplace from `https://github.com/markusleben/ha-nova`, not from the local install directory.
- **Reason**: end users should get Claude’s intended Git-backed marketplace update behavior instead of a frozen local-directory source.

- **Claude local-validation override**: local RC/dev flows explicitly set `HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1` and then use a staged local marketplace root.
- **Reason**: private bundle validation must prove the freshly built local payload, not whatever GitHub currently serves.

- **Marketplace template source rule**: `.claude-plugin/marketplace.json` uses the same-repository relative source `./`.
- **Reason**: Claude accepts relative same-repo sources for Git-backed marketplaces and rejects the absolute-path rewrite that caused the real Step-4 failure.

- **Non-interactive setup persistence order**: save the relay token before writing `config.json`, and roll the token back if config persistence fails.
- **Reason**: partial onboarding state with config-but-no-token is a worse failure mode than a rolled-back token write.

- **Unix reinstall state rule**: `install.sh` rewrites `state.json` on reinstall instead of preserving stale metadata.
- **Reason**: macOS/Linux reinstall must stay aligned with PowerShell installer behavior for version and PATH metadata.

- **Windows validation cleanup rule**: private Windows cleanup removes HA NOVA test Credential Manager entries as well as files.
- **Reason**: repeated desktop/headless validation must stay hermetic even if a prior token delete path failed.

## 2026-03-15: Final KISS Review Tightening

- **Non-interactive setup side-effect rule**: `ha-nova setup --non-interactive` must not open browsers or touch the clipboard.
- **Reason**: flag-driven/headless setup is the automation lane; desktop UX side effects belong only to the interactive wizard.

- **Non-interactive rollback rule**: if non-interactive setup fails before readiness is proven, roll token/config/state back to the pre-run snapshot.
- **Reason**: unlike the interactive wizard, the flag-driven path is not a human resume flow; failed automation should not silently mutate the machine.

- **Flag-host trust rule**: `--host` without an explicit `--ha-url` must fail if host resolution cannot prove a reachable Home Assistant base URL.
- **Reason**: silently guessing URLs in the flag path drifted from the visible wizard host-check and hid real misconfiguration.

- **Discovery parsing rule**: Windows ARP discovery must ignore interface/header lines and only treat actual neighbor rows as candidates.
- **Reason**: otherwise the host machine IP can be promoted ahead of real Home Assistant neighbors.

- **mDNS test determinism rule**: the mDNS discovery test uses an injectable availability gate instead of the live OS/`dns-sd` environment.
- **Reason**: parity tests must prove the discovery path deterministically on any maintainer or CI machine.

- **Bundle source ambiguity rule**: install-bundle builds must fail when flat and nested `dist/` artifacts coexist for the same target.
- **Reason**: mixed `dist/` layouts are exactly how stale-but-valid bundles get produced.

- **Legacy shell helper naming rule**: old macOS shell onboarding npm scripts are exposed only under `dev:legacy:onboarding:macos*`.
- **Reason**: first-class npm scripts should bias contributors toward the host-safe validation lane, not the old host-touching shell path.

- **Interactive completed-setup override rule**: if the machine is already fully set up, a rerun with only host/URL overrides persists the new config without forcing a fresh readiness proof first.
- **Reason**: that path is an edit-on-top-of-a-good-machine flow, not a failed-first-run recovery path.

- **Windows keyring authority rule**: Credential Manager is the primary source of truth; the legacy DPAPI sidecar is best-effort compatibility only.
- **Reason**: legacy cleanup/mirror failures must not turn a successful primary secure-store write/delete into a false hard failure.

## 2026-03-16: macOS mDNS Browse Indentation Fix

- **mDNS browse parsing rule**: macOS `dns-sd -B` browse rows may start with leading whitespace before the timestamp, and the parser must still recover the Home Assistant instance name.
- **Reason**: real `dns-sd` output on the maintainer machine used an indented `Add ... _home-assistant._tcp. Zuhause` row; rejecting that row caused discovery to miss the TXT-record IP and fall back to `homeassistant.local`.

## 2026-03-16: Claude Local Validation Cache Bust

- **Claude local-validation refresh rule**: local Claude marketplace installs must remove the currently installed `ha-nova@ha-nova` plugin, clean its cached payload, and then reinstall fresh instead of calling `claude plugin update`.
- **Reason**: Claude caches plugin payloads by version, and local validation keeps the released plugin version (`0.1.12`), so `update` can leave old skill content like `~/.config/ha-nova/relay` and `npm run onboarding:macos` alive behind the cache.

## 2026-03-16: macOS Uninstall Deletes Relay Token

- **macOS uninstall token rule**: `ha-nova uninstall` deletes the stored relay auth token from the macOS Keychain, matching `origin/main`.
- **Reason**: the product expectation is strict uninstall parity with the current shell release; reinstall after uninstall should start from a clean token state, even though that means we still need to isolate the separate prompt source.

## 2026-03-16: Uninstall Must Explain Itself

- **Uninstall feedback rule**: `ha-nova uninstall` must print concrete removal lines, including relay-token removal when it happens, before the final success line.
- **Reason**: the old shell uninstall was explicit about what it removed; the new Go uninstall had become too silent and left users guessing whether files, client integrations, or credentials were actually touched.

## 2026-03-16: Client Registry Scope

- **Registry KISS rule**: the first client registry is one checked-in JSON file and covers only real installable HA NOVA targets with three built-in adapter kinds: `plugin_marketplace`, `skill_tree`, `skill_flat`.
- **Reason**: today only Claude, Codex, OpenCode, and Gemini have proven install contracts; a single small file is contributor-friendly without turning four targets into a manifest system.

- **Future editor rule**: Cursor / VS Code are future adapter work, not initial runtime registry entries.
- **Reason**: their integration surfaces are broader than today's skill installers and need a real MCP/prompt/plugin contract before they belong in the wizard or install loop.

## 2026-03-16: Setup + Uninstall UX Parity Polish

- **LLAT walkthrough rule**: interactive setup always guides the user through the Home Assistant Long-Lived Access Token flow before verify, even when the relay token was already supplied.
- **Reason**: `origin/main` treated LLAT guidance as first-run setup, not only as failure recovery.

- **Verify persistence rule**: interactive setup must not persist `config.json` / `state.json` before the verify step finishes or explicitly ends as incomplete.
- **Reason**: backing out of verify should not silently leave a half-configured machine behind.

- **Uninstall preflight rule**: uninstall prints a short "This will remove" summary first and only claims final removal if something was actually removed.
- **Reason**: clear intent before deletion, no contradictory noop success messaging.

- **Relay-running uninstall note**: if uninstall can still reach NOVA Relay before cleanup, it prints a final note telling the user that the Home Assistant App is still installed/running.
- **Reason**: local uninstall and App uninstall are separate actions and the user should not have to infer that.

## 2026-03-16: Final Onboarding + Uninstall Hardening

- **Interactive LLAT test rule**: interactive wizard tests must feed the full LLAT walkthrough and assert the final exit code, not only partial output fragments.
- **Reason**: otherwise EOF regressions hide until the full package run.

- **Claude install truth rule**: Claude setup fails if `claude` is missing, and setup-state detection trusts the real Claude plugin record instead of stale `state.json`.
- **Reason**: onboarding/resume must not claim Claude is installed when no plugin registration happened.

- **Verify issue preservation rule**: declining retry in Step 3 keeps the real failure category (`relay_unreachable` vs `ws_degraded`).
- **Reason**: the incomplete banner must point users at the actual problem, not a generic WebSocket path.

- **Interactive persistence rollback rule**: if interactive setup writes `config.json` but fails to write `state.json`, config/state/token all roll back to the pre-run snapshot.
- **Reason**: partial persistence on save failures is worse than a clean rollback.

- **Uninstall completion rule**: relay-token deletion errors are reported at the end of uninstall, after the rest of the local cleanup still runs.
- **Reason**: a secure-store failure must not strand files/config/cache in a half-uninstalled state.

- **Windows background-note rule**: the parent Windows uninstall path prints the relay-running note before handing off to the helper.
- **Reason**: otherwise the new UX hint only appears on Unix even though preflight already knows the App is still running.

- **Generic `/ws` transport rule**: setup must only blame LLAT when the relay `/ws` response proves it; transport/proxy failures stay generic.
- **Reason**: a failed `/ws` probe without LLAT proof is not honest evidence of a bad Home Assistant access token.

- **Managed-uninstall scope rule**: uninstall removes known HA NOVA config/cache artifacts and only removes `~/.config/ha-nova` / `~/.cache/ha-nova` when they are empty afterward.
- **Reason**: preserve unknown user files while still fully cleaning installer-managed content.

- **Completed override truth rule**: rerunning setup on an already healthy machine with host/URL overrides must re-evaluate readiness against the new target instead of reusing the old complete state.
- **Reason**: saving an unverified override and showing “already set up” is a silent misconfiguration trap.

- **Token persistence timing rule**: interactive token changes are written only at the persistence point that already has rollback coverage.
- **Reason**: `exit`/`back` before verify must not silently mutate secure storage.

- **Non-interactive rollback rule**: non-interactive setup snapshots config/state before writing the relay token and rolls all three back if the initial state write fails.
- **Reason**: partial persistence on early save failures is just as harmful in scripted setup as in the interactive wizard.

## 2026-03-16: Claude Current Cache Layout

- **Claude local-cache cleanup rule**: local Claude validation clears `~/.claude/plugins/cache/ha-nova` as the canonical HA NOVA cache root instead of only deleting the older nested `~/.claude/plugins/cache/ha-nova/ha-nova` layout.
- **Reason**: current real Claude installs cache HA NOVA directly under `cache/ha-nova`, so cleaning only the nested legacy path leaves stale payloads behind and can trigger `ENOTDIR` during reinstall.

- **Claude bundle-local staging rule**: the Go installed-bundle local Claude override stages the plugin under `~/.config/ha-nova/claude-marketplace`, and if that installed bundle root contains a top-level regular file named `ha-nova`, that file is excluded from the staged plugin payload.
- **Reason**: Claude's directory-marketplace installer collides with a top-level `ha-nova` file and its own cache path `.../cache/ha-nova/ha-nova/<version>`, which reproduces the observed `ENOTDIR` specifically on installed bundle roots.

## 2026-03-16: Claude Uninstall Project Memory

- **Claude uninstall memory rule**: uninstall does not auto-delete Claude project-memory files under `~/.claude/projects/*/memory`; it only detects HA NOVA-related entries there and prints an explicit warning.
- **Reason**: Claude project memory is user data, not installer-owned state. Auto-deleting it is riskier than leaving it intact and telling the user why Claude may still reference removed skills.

## 2026-03-16: Final Parity Truth Cleanup

- **Interactive fast-path rule**: interactive setup skips the LLAT walkthrough only when both the Home Assistant host and the relay token were already supplied up front.
- **Reason**: that matches the old fully-preseeded setup path without dropping the LLAT guide for normal first-run flows.

- **Claude GitHub marketplace safety rule**: the normal end-user Claude path must not remove an existing HA NOVA marketplace registration before a replacement source has been proven installable.
- **Reason**: a failed GitHub marketplace refresh must not destroy an already working Claude install.

- **Update sync truth rule**: post-update client refresh must detect actually installed clients from disk, not only trust `state.json`.
- **Reason**: stale or missing state must not cause a real Claude install to be skipped during `update`.

- **Claude marketplace metadata rule**: the repo marketplace manifest includes `metadata.description` and `metadata.version`.
- **Reason**: keep the official Claude validator clean instead of accepting avoidable warnings in the published marketplace file.

- **Unix PATH ownership rule**: uninstall only removes the HA NOVA-managed PATH block marker, never a generic standalone `export PATH="$HOME/.local/bin:$PATH"` line.
- **Reason**: `~/.local/bin` is shared territory; removing a generic export can break unrelated tools after uninstall.

- **Go onboarding contract rule**: the onboarding contract suite executes focused Go tests for current wizard/readiness/uninstall behavior instead of only asserting test-file names or legacy shell references.
- **Reason**: the shipped runtime is Go-first; contracts must exercise that path directly.

- **Local Claude validation rule**: `HA_NOVA_CLAUDE_MARKETPLACE_LOCAL=1` is explicitly treated as a fresh local reinstall lane, not an in-place marketplace update path.
- **Reason**: local validation must prefer determinism over preserving stale Claude plugin/cache state.

- **Update rollback rule**: bundle replacement keeps the previous install root alive until post-update client sync succeeds; if client sync fails, HA NOVA rolls the runtime back before returning failure.
- **Reason**: updated runtime + stale client integrations is worse than aborting the update cleanly.

- **Claude marketplace self-heal rule**: if Claude already has a stale HA NOVA marketplace source configured, the normal GitHub install/update path now replaces it with the GitHub source and restores the previous registration if the replacement add fails.
- **Reason**: moving from local validation back to a real end-user install must self-heal stale local marketplace state without leaving Claude in a broken half-removed state.

- **Secure-store truth rule**: relay-token reads must distinguish “missing token” from “secure store unavailable”, and doctor/setup/uninstall messages must preserve that distinction.
- **Reason**: a locked Keychain, broken Secret Service, or Credential Manager error is not the same as “no token saved”; collapsing both states misleads resume, doctor, and uninstall notes.

- **Windows uninstall preflight rule**: Windows uninstall preflight names the installed CLI binary inside `~/.local/share/ha-nova`, not the Unix symlink path, and the helper prints partial removals before a final relay-token deletion error.
- **Reason**: uninstall output must stay truthful on Windows and still be useful when cleanup fails late.

## 2026-03-17: JQ Escape Hardening

- **Known jq escape recovery rule**: relay jq parsing retries once after normalizing bare `\.` into `\\.`.
- **Reason**: stale client memory and model-generated filters can still produce jq-incompatible regex dots; a narrow retry fixes the real failure without adding a generic jq rewrite layer.

- **Helper-domain filter rule**: skill examples for helper-domain matching must avoid jq regex escapes and use split-domain membership checks instead.
- **Reason**: helper-domain filtering does not need regex, so removing the escape-sensitive pattern is the simplest way to keep Windows and macOS client usage stable.

## 2026-03-17: Existing Relay Token Verify-First

- **Existing relay-token wizard rule**: `Keep saved token`, `Paste existing token`, resume-with-token, and explicit relay-token flag flows now skip the LLAT walkthrough and verify first.
- **Reason**: if the relay token already works on another device, the shortest truthful path is to prove the current relay/app state before reopening Home Assistant pages.

- **Diagnosis-driven repair rule**: reuse-token verify failures now branch into LLAT repair, relay-auth repair, or ambiguous app-side repair based on actual relay readiness signals.
- **Reason**: the wizard should not ask users to re-do both token setups when the probes can already prove which side still needs attention.

## 2026-03-17: Cross-Platform Skill Examples

- **Skill example contract rule**: active skill examples must teach one file-based relay workflow across macOS, Linux, and Windows.
- **Reason**: examples that fall back to Python, Node, `cat`, heredocs, or shell pipes drift into client/runtime-specific behavior and break otherwise-correct HA NOVA flows.

- **Complex jq rule**: long body-extraction and entity-filter examples must use `--jq-file`; saved JSON follow-up work must use `ha-nova relay jq --file`.
- **Reason**: this is the smallest reliable pattern across bash/zsh, PowerShell, and model-generated command synthesis.

- **Relay jq parity rule**: `ha-nova relay jq` now accepts `--jq-file <filter-file>` in addition to the inline filter argument.
- **Reason**: the docs should not need a special shell-quoted exception just because the post-processing step runs after the relay request instead of during it.

## 2026-03-17: Early Client Availability Truth

- **Early client-availability rule**: setup now resolves client availability before the user commits to client installation work.
- **Reason**: selecting a client that is not actually usable on the current machine created false success paths, especially for file-based clients.

- **Configured-vs-ready rule**: client truth is now split into configured, attached, runtime-detected, and ready-now instead of treating file footprints as full success.
- **Reason**: setup, resume, doctor, and update must speak the same truth about whether HA NOVA is merely staged or truly usable right now.

- **Native Windows rule**: native Windows setup only evaluates native client runtimes and does not probe WSL.
- **Reason**: WSL probing would add a second environment model and a lot of hidden failure surface; the truthful contract is to run HA NOVA in the same environment as the client.

- **No stage-only success rule**: missing Codex/OpenCode/Gemini runtimes no longer count as successful setup just because HA NOVA could have written files into standard skill directories.
- **Reason**: convenience here was causing misleading success banners and downstream doctor/update drift.

- **All-available rule**: setup selection now resolves `all` as “all available clients” and prints the exact subset when some clients are skipped.
- **Reason**: silently shrinking `all` would be another truth trap; the wizard must say exactly what it will configure.

## 2026-03-17: Client Registry Bundle Parity

- **No panic-on-missing-registry rule**: runtime paths must surface client-registry load failures as normal command errors, never as panics.
- **Reason**: setup/doctor/update are user commands; even a broken bundle must fail loud and readable instead of crashing the process.

- **Bundle parity rule**: install/update bundles must always ship `clients/registry.json`, and staged bundle validation must reject bundles that omit it.
- **Reason**: the client registry is now runtime-critical metadata, so bundle creation and bundle validation both need to enforce its presence.

## 2026-03-17: Starlight UI Refresh

- **Accent rule**: human-facing HA NOVA setup and installer chrome now use a warm amber accent instead of cyan.
- **Reason**: the previous cyan worked technically, but it did not fit the NOVA identity as well as a high-contrast starlight accent.

- **Compact menu rule**: enhanced setup menus now stay left-aligned and move disabled reasons onto a muted second line instead of inline em-dash text.
- **Reason**: this keeps the actual decision text visually dominant and makes long unavailable-client reasons easier to scan across macOS, Linux, and Windows terminals.

- **ANSI safety rule**: rich rendering now requires both TTY and ANSI/VT support; otherwise HA NOVA falls back all the way to plain mode.
- **Reason**: color-only capability checks were not enough because clears, redraws, and raw-menu repaint are unsafe on non-ANSI terminals.

- **Wizard spacing rule**: setup pages now use a consistent block rhythm: step line, paragraph block, optional list block, then menu/prompt block.
- **Reason**: the previous mixed `Fprintln` spacing made some screens feel visually glued together even though the underlying content was correct.

- **Plain setup rule**: setup falls back all the way to plain mode when input is not interactive, even if stdout is a TTY.
- **Reason**: screen clears, raw menus, and styled page rendering are not safe once setup can no longer read interactive input.

- **Plain step semantics rule**: plain mode keeps the full `Step n of m - Title` wording and only strips the chrome around it.
- **Reason**: plain mode should stay the same wizard with less ornament, not a semantically thinner variant.

- **Installer reinstall rule**: the Unix installer must preserve existing `installed_clients`, `client_install_modes`, and prior `path_managed=true` state on reinstall.
- **Reason**: a normal reinstall must not silently erase configured-client truth that doctor/update still rely on later.

- **Warning rendering rule**: update/version warnings now return structured notices and are rendered only at the command boundary through the shared human UI layer.
- **Reason**: preformatted warning strings were bypassing plain/styled rendering and reintroducing mixed prefixes, emoji, and stdout pollution.

## 2026-03-17
- Default: Windows uninstall keeps the visible helper attached for final status output, but launches temp self-delete cleanup detached/hidden with no inherited handles so the shell can return cleanly.
- Default: Windows uninstall prefers visible final status output plus an explicit Ctrl+C escape hint over suppressing helper output entirely; users asked to keep the end-state confirmation in the same console.
- Default: the Windows Ctrl+C hint belongs at the end of the helper output, not in the early parent message, because it is only relevant if the shell still has not returned after the final uninstall lines.
- Default: setup completion should list the actual resolved installed clients, not only a generic prompt target, so multi-client runs stay truthful.
- Default: Gemini flat-copied HA NOVA sub-skills use namespaced installed names (`ha-nova-...`) in both folder names and copied frontmatter names.
- Reason: Gemini was seeing prefixed folder/resource names but short shared skill names, which made it guess the wrong activation name before self-correcting.
- Default: excluded onboarding contracts must assert the same namespaced Gemini install names as the active installer (`name: ha-nova-...`), not the source-tree short names.
- Reason: the current product contract intentionally diverges between source skills (`write`) and Gemini-installed flat copies (`ha-nova-write`); stale tests were checking the wrong surface.
- Default: HA NOVA skill guidance now explicitly forbids PowerShell `&&` / `||` chaining, external `jq`, and heuristic domain expansion for simple entity-domain counts.
- Reason: Gemini on Windows followed the relay path successfully, but still wasted steps on PowerShell-5.1 shell chaining, non-contract `jq`, and overcomplicated lamp counting.

## 2026-03-17: Project Audit Follow-Up

- **Active-doc-first rule**: on release-eve cleanup, fix contradictions in active product docs and live skill prompts before touching large structural refactors.
- **Reason**: stale install/release guidance can ship user-facing breakage immediately, while deep CLI file splits would add rollout risk this late.

- **Rollout safety rule**: oversized CLI/runtime files found during the audit are tracked as post-release refactor targets unless a concrete defect forces a same-day change.
- **Reason**: tomorrow's rollout benefits more from consistent behavior and verified contracts than from a broad mechanical file split under time pressure.

## 2026-03-17: CLI Structure And Release Parity

- **Split-by-lifecycle rule**: large Go command files should be split by lifecycle or transport concern, not by arbitrary utility buckets.
- **Reason**: setup, update, uninstall, and bundle staging each have distinct failure modes and tests; splitting along those seams lowers drift without changing behavior.

- **Shared-prompt rule**: normal and wizard prompts use the same reader primitives with navigation as an option, not duplicated line/yes-no implementations.
- **Reason**: prompt copy-paste drift is especially risky in onboarding because tiny wording or nav differences break long interactive flows.

- **Release-lane parity rule**: release and release-candidate workflows use the same supported Node lane as CI unless there is an explicit product reason to diverge.
- **Reason**: publishing from a different Node lane than CI is unnecessary hidden risk.

- **Active-release-config rule**: when the real release workflow always skips a capability, remove the dead config instead of leaving a second inactive policy behind.
- **Reason**: dormant signing config next to `--skip=sign` is more misleading than helpful during a release audit.

## 2026-03-18: Setup Token Step Separation

- **Split-token-step rule**: the setup wizard treats the Relay Auth Token and the Home Assistant Access Token as separate steps whenever the LLAT walkthrough is actually needed.
- **Reason**: users were being asked to mentally switch token types mid-screen, which made onboarding feel more chaotic than it is.

- **Relay-reminder rule**: the LLAT step repeats the current Relay Auth Token once as reminder-only copy, with an explicit note that the user does not need to create a new relay token there.
- **Reason**: the relay token is easy to lose between screens, but repeating it without context would make the second step look like a duplicate task.

- **Secondary-recovery rule**: reminder content on a setup screen must stay visually subordinate to the primary task and should appear as an optional note block, not as a second main paragraph.
- **Reason**: novice users follow the first visually dominant instruction they see; secondary recovery help should reduce panic, not create a second competing task.

- **Prompt-block rule**: wizard action prompts such as `Press Enter ...` must render as their own indented block with breathing room above them.
- **Reason**: if the CTA sits flush against explanatory copy, it reads like a broken wrapped sentence instead of the next action.

## 2026-03-18: Release Docs Windows Parity

- **Platform-vs-lane rule**: release-facing docs must separate platform support from per-client validation lanes.
- **Reason**: “Windows support” is true for the installer/runtime path, but it becomes misleading if readers infer that every client lane is equally smoke-validated there.

- **Actionable-Windows-docs rule**: each client install doc must show the real Windows PowerShell installer command and the current ARM64 caveat, not just prose that says “run the normal installer”.
- **Reason**: release docs should be executable from the page itself, especially for the newly expanded platform lane.

## 2026-03-18: Update Detection Truth + Gemini Windows Claim

- **Validated-Windows-lane rule**: Gemini now counts as a Windows-validated client lane for this release.
- **Reason**: release-facing docs must reflect confirmed local validation, not older narrower wording.

- **Auto-update-truth rule**: release docs must distinguish the shared updater from client-specific startup banners.
- **Reason**: `ha-nova check-update` / `ha-nova update` work across installs, but the automatic SessionStart `UPDATE AVAILABLE` banner is currently a Claude-specific surface.

- **Linux-claim rule**: README/project docs may list Linux as supported only with an explicit note that current confidence there comes from installer/bundle coverage plus CI smoke until a real Secret Service-backed Linux run is completed.
- **Reason**: Linux is genuinely in the product and release automation, but that is still weaker than a live maintainer validation pass on an actual Linux desktop/session.

- **Quick-start disclosure rule**: when a platform shares an install command with a stronger validated platform, any lower-confidence caveat must appear directly under that shared command block.
- **Reason**: readers decide from the first executable snippet they see; a later caveat in another section is too weak to undo an early equal-confidence impression.
