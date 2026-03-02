# Choices

## 2026-02-25

### User Decisions (Brainstorm Q1-Q10)
- Priority: balanced (`Demo + API + Skill`).
- DoD: combined DoD with reduced scope.
- Target client: client-agnostic.
- Security strictness in 1a: dev-first.
- Runtime quality: functional + safe error handling.
- `ha-onboarding.md`: ultra-lean.
- Onboarding auth: user-generated LLT mandatory (`HA_TOKEN`), supervisor token not onboarding credential.
- `ha-safety.md`: core-only.
- `ha-nova.md`: only active 1a/1b skills.
- Test strategy: lightweight automation (Vitest).
- Output format: a single plan file.

### Planning Defaults (Agent)
- Plan structure: vertical-slice approach (preferred over infra-first/skill-first).
- Error contract: unified JSON error envelope across all endpoints.
- Execution workspace: worktree-skill fallback used (repo without commit/HEAD), so feature branch `feat/phase-1a-foundation` was executed in the current workspace.
- GitHub owner for remote operations: canonical API login `markusleben` used (rename from `w0nk1`).
- Branch strategy after repo bootstrap: set `main` as default branch, merge PR #1 into `main`, then apply post-merge wiring fix directly on `main`.
- Phase 1b execution mode: skill-first (REST workflow skills + acceptance doc), no new relay endpoints in this step.
- Branch for 1b: `feat/phase-1b-rest-skills` from `main`.

## 2026-02-26

### Planning Defaults (Agent)
- Upstream token policy (superseded): dual auth with deterministic fallback (`HA_LLAT` > app option `ha_llat` > legacy `HA_TOKEN` > `SUPERVISOR_TOKEN`).
- Compatibility policy (superseded): keep `HA_TOKEN` required for existing relay auth behavior, mark `HA_TOKEN` as deprecated only for upstream LLAT resolution.
- Dev ergonomics policy: provide idempotent LLAT seed command (`npm run seed:llat`) that validates options before write.
- Supervisor API write policy: merge existing app options before posting update to avoid unintended option loss.
- Planning gate policy: no new plan without prior review of current official HA docs (developer docs + app docs + user docs).
- Runtime auth split policy (superseded): relay endpoint auth token and upstream token selection are separated (`RELAY_AUTH_TOKEN` preferred for relay auth; upstream fallback uses `HA_TOKEN` only when relay auth is explicitly separated).
- Runtime capability policy (superseded): if upstream auth is limited/none, relay stays up and returns explicit WS error instead of crashing startup.
- Phase-1a.3 planning policy: App packaging keeps shell runner thin (`app/run`), with all auth precedence in Node runtime only (KISS + DRY).
- Security baseline for packaging (superseded): require explicit `relay_auth_token` in app options; keep `ha_llat` optional persistent capability token.
- Terminology policy (user): prefer "App" wording in docs/UX communication; keep legacy "addon" only where technical HA interfaces enforce it.
- Architecture policy (user): MVP first, but always modular-by-design to minimize future adaptation cost.
- Skill policy (user): skills remain markdown-only (`*.md`), while relay stays lean and efficient.
- Clean-slate policy (user): no backward-compatibility fallbacks in this non-public phase; remove legacy token paths and aliases instead of preserving them.
- Env contract policy: relay auth requires `RELAY_AUTH_TOKEN`; no `HA_TOKEN` fallback.
- Naming policy (superseded): internal runtime/env naming uses `app_*` (`APP_OPTIONS_PATH`, `app_option_ha_llat`) while keeping `/addons/...` only for Supervisor API endpoints.
- Live validation policy: ship one deterministic HA E2E runner (`smoke:app:e2e`) that validates Supervisor options flow and relay `/health` + `/ws` in one command.
- Contributor deploy policy: provide two explicit HA deploy modes — `deploy:app:fast` for normal iteration and `deploy:app:clean` for aggressive cache-busting rebuilds.
- Contributor env policy: keep secrets in untracked `.env.local` (or `.env`), ship committed `.env.example`; script env files are convenience only and must not override explicitly exported shell vars.
- E2E policy refinement (superseded): `SUPERVISOR_TOKEN` is optional for runtime-only smoke and mandatory only for Supervisor option preflight / `--apply`.
- Language policy enforcement: keep all project documentation English-only, including reference and migration docs.
- Supervisor-first runtime policy (superseded): maximize operation without LLAT; treat `HA_LLAT` as optional fallback for non-App contexts.
- App options policy refinement (superseded): optional password fields must avoid `null` defaults/writes (use empty string or omit key) to prevent Supervisor validation/start failures.
- Capability wording policy (superseded): describe supervisor fallback as "limited WebSocket scope" (not generic limited API scope).
- Contributor bootstrap policy: provide a single bootstrap deploy command that handles sync, first install, Supervisor option validation/write, and app restart.
- Option write normalization policy: write `ha_llat` as string (possibly empty) during bootstrap option apply to overwrite stale `null` values in Supervisor options.
- Audience policy refinement (user): primary target is end-user onboarding simplicity; developer bootstrap tooling must remain clearly separated from normal user flows.
- Naming policy refinement (dev tools): keep developer-only scripts under `dev:*` npm namespace and separate script path (`scripts/dev/*`).
- Secret storage policy (user + research): macOS onboarding is Keychain-first (`security`), while `.env.local` remains contributor-only convenience.
- Onboarding UX policy (macOS): provide one interactive setup command with Keychain-backed secrets and explicit diagnostics.
- Separation policy refinement (user): end-user onboarding script must not include SSH/bootstrap; contributor bootstrap remains isolated under `scripts/dev/*`.
- Onboarding validation policy (user): setup must validate HA instance host and probe Relay health; on Relay failure, show explicit install/start guidance for the App.
- Onboarding resilience policy: if HA validation fails, allow explicit user override (`Continue with unverified host`) instead of hard-blocking setup.
- Dev deploy cache policy: auto-detect Supervisor metadata drift (`ingress`/`ports`) and auto-reinstall the App during `deploy:app:*`.
- Onboarding token UX policy refinement: on repeated macOS setup, empty relay token input reuses existing Keychain token before any auto-generation.
- Onboarding diagnostics policy refinement (superseded): classify Relay health failures by status (`401/403`, `404`, `000`) and surface `ha_ws_connected=false` as a non-blocking warning.
- Onboarding UX pattern policy: provide a public raw instruction entrypoint (`.codex/ONBOARDING.md`) so Codex can guide setup from one link/prompt.
- Onboarding UX pattern refinement: canonical one-link Codex entrypoint is `.codex/INSTALL.md`; `.codex/ONBOARDING.md` is alias-only.
- Onboarding command-surface policy: keep onboarding runtime commands minimal (`setup`, `doctor`, `env`) and remove client launch commands.
- Onboarding first-time policy: canonical `.codex/INSTALL.md` must include clone/install bootstrap for users without a local repo.
- Skill-install parity policy: Codex path must support one-time global skill install (`~/.agents/skills`) so daily startup uses plain `codex` without a custom launcher.
- Multi-client installer policy: one local installer must support Codex, Claude Code, and OpenCode skill-link targets (`install:skills`) with idempotent backups.
- Install-flow safety policy: canonical `.codex/INSTALL.md` must keep onboarding as setup+diagnostics only (no nested client launch flow).
- Skill portability policy: installed local skills are rendered as managed `SKILL.md` files with concrete repo root substitution instead of symlink-only paths.
- Skill contract policy (superseded): active skill docs use `RELAY_BASE_URL`/`RELAY_AUTH_TOKEN` and optional `HA_LLAT`; legacy `HA_TOKEN`/`RELAY_URL` terms are removed.
- Onboarding cleanup policy refinement: obsolete launcher flow is removed (`start`, `codex`, `claude`), keeping only `setup`, `doctor`, and `env`.
- Installer simplification policy: remove dedicated wrapper script and call `install-local-skills.sh` directly from npm shortcuts.
- Skill orchestration policy refinement: `ha-nova` is a strict router with mandatory `doctor` gate before any HA operation.
- Capability policy refinement for automation CRUD: prefer App-context Supervisor path when available; fallback to direct REST with `HA_LLAT`; otherwise stop and route to onboarding.
- Install UX standardization policy: Codex and Claude install docs must share the same section structure (`Quick Install`, `Verify`, `Troubleshooting`, `Security`).
- MVP validation policy: provide one contributor command (`smoke:app:mvp`) that enforces onboarding health gate and runs deterministic automation CRUD smoke with cleanup.
- Fresh-session readiness policy: add `onboarding:macos:quick` as default fast gate (doctor + installed skill marker) before heavy contributor checks.
- Quick-gate parsing policy: normalize installed skill marker parsing to strip HTML comment suffix (`-->`) before repo-root comparison.
- Live skill E2E policy: validate Codex behavior in black-box mode via `codex exec --json` instead of only shell smoke scripts.
- Live skill E2E realism policy: reject helper-smoke-script command execution and subagent delegation in this harness to match single-agent user behavior.
- Capability-aware E2E expectation policy (superseded): `e2e:skill:codex` used `E2E_EXPECT=auto` before mandatory-LLAT simplification.
- Live E2E delegation policy: subagent usage is allowed by default (`E2E_SUBAGENT_POLICY=allow`) to reflect realistic user sessions; strict single-agent mode remains opt-in (`deny`).
- Fast-path UX policy (user): the skill must always prefer the fastest viable capability path first.
- Session-check policy refinement: replace per-operation visible doctor flow with cached `ready --quiet` gate; show diagnostics only on failure.
- Entity discovery capability policy: `ha-entities` now prefers Relay `/ws` `get_states` and uses direct REST + `HA_LLAT` only as fallback.
- Auth policy override (user): `HA_LLAT` is mandatory baseline for MVP runtime and onboarding; no no-LLAT user path.
- Runtime policy override: remove `SUPERVISOR_TOKEN` fallback from upstream token resolution; runtime startup fails fast without LLAT.
- Onboarding policy override: `setup`, `doctor`, `ready`, and `env` must fail when LLAT is missing/invalid or Relay reports `ha_ws_connected=false`.
- Config-surface reduction policy (user): keep App/user settings minimal (no ws type filter option).

## 2026-02-27

### Planning Defaults (Agent)
- E2E capability-proof policy: keep `e2e:skill:codex` assertions path-neutral (`/config/automation/config/{id}`, `/services/automation/reload`) so both App-context Supervisor and direct REST traces satisfy the same CRUD contract.
- Onboarding UX clarity policy: setup text must always make LLAT mandatory semantics explicit (`required`, `must match`) and avoid optional-sounding degraded-WS wording.
- Runtime HA endpoint policy (LLAT-only model): default `HA_URL` must target Home Assistant Core (`http://homeassistant:8123`), not Supervisor proxy (`http://supervisor/core`), to keep upstream WS auth consistent with LLAT.
- Single-secret user policy: end-user client stores relay auth only; LLAT is configured only in App option `ha_llat`.
- Relay-only user routing policy: active end-user skills must not require client-side `HA_LLAT` and should route through App + Relay capability checks.
- Supervisor boundary policy: Supervisor API usage remains contributor/deploy internal and is excluded from end-user onboarding flow.
- Review hardening policy: every single-secret model change requires at least one logic review and one contract-test review before merge.
- Contributor e2e policy: `e2e:skill:codex` is contributor validation and must explicitly require shell-provided `HA_LLAT`.
- Deploy metadata sync policy refinement: `deploy:app:*` must auto-reinstall when Supervisor `options/schema` keys drift from `app/config.yaml` (prevents stale metadata after hot deploy).
- Onboarding doctor reliability policy: when `/health` reports `ha_ws_connected=false`, doctor performs a `/ws` ping warm-up before failing, to avoid lazy-connect false negatives.
- Onboarding UX policy refinement: setup must clearly state that LLAT is configured only in App option `ha_llat` (never prompted/stored in client onboarding).
- Onboarding progress-visibility policy: setup prints explicit in-progress markers (`[..]`) for host detection, endpoint validation, and relay health checks.
- Skill runtime UX policy override (user): skip proactive `ready/doctor` checks before first HA action; run diagnostics only when a real capability failure occurs.
- Entity read latency policy: for end-user read/list flows, call Relay `/ws` first in a single one-shot command and avoid separate `/health` preflight unless `/ws` fails.
- Read-flow micro-optimization policy: remove redundant follow-up `/health` checks after successful `/ws` warm-up and keep env bootstrap explicitly once per shell session.
- Simple-read UX policy: for trivial read-only prompts (first N IDs / counts), avoid subskill-file loading and return result-only output without progress narration.
- Live E2E robustness policy: evaluate JSONL transcript assertions as source of truth; non-zero `codex exec` exit alone must not fail if final status + evidence checks pass.
- Scenario E2E policy: keep a separate read-only, user-scenario Codex harness (`e2e:skill:codex:scenarios`) decoupled from contributor CRUD validation.
- Scenario contract policy: each scenario must define deterministic correctness checks (`entity_id` prefix + count + `count_mode`) in a declarative JSON file to avoid hardcoded one-off tests.
- Routing quality policy: scenario harness must fail when `/health` is called before `/ws`, when proactive onboarding diagnostics (`doctor/ready/quick`) appear before first HA read, or when helper-script shortcuts are used.
- MVP scenario-pack policy: default read-only P0 suite includes `switch`, `light`, `sensor`, and `binary_sensor` discovery scenarios.
- Inventory-variance policy: default read scenarios use `count_mode=up_to` to keep assertions stable across different homes while still validating routing and prefix correctness.
- Scenario-roadmap policy: negative/scope-boundary scenarios are P1 and require explicit harness support for expected-failure assertions before activation in default suite.

## 2026-02-28

### Planning Defaults (Agent)
- GitHub baseline policy: add full community-health + `.github` starter pack before any structural repo refactor.
- License default: use MIT for MVP velocity and broad compatibility.
- CI default: single required check job name `ci-gate` (typecheck + test + build), with `pull_request` + `merge_group` triggers.
- Security default: include CodeQL workflow + dependency review + Dependabot weekly updates.
- Release default: add `.github/release.yml` categorization only; defer release publishing automation to a later phase.
- Ownership default: set initial CODEOWNERS fallback to `@markusleben`.
- Release automation policy refinement: enable `release-please` in minimal single-package mode for changelog/tag/release PR automation on `main`.
- PR automation policy: add label-gated Codex watchdog (`autofix:enabled`) with one auto-review request per head SHA and one auto-fix request per failed workflow run.
- Code scanning policy refinement: grant `actions: read` in CodeQL workflow permissions to avoid workflow-run metadata access failures.
- Scenario-pack continuation policy: implement P1 as behavior-first extensions on existing harness (no rewrite), keeping P0 contract stable while adding `expected_status`, `expected_error`, `forbid_patterns`, and `must_contain_text`.
- Scope-boundary validation policy: represent non-MVP write requests as explicit boundary-message scenarios with forbidden write-pattern checks, instead of adding write execution to the P1 suite.
- Scenario-runtime stability policy: raise default scenario `max_duration_sec` from 45 to 90 to avoid false negatives from prompt/context startup overhead while keeping a bounded latency guard.
- Prompt guardrail policy refinement: onboarding pre-action doctor restriction is now conditional (allowed only when explicitly requested by scenario prompt) to make forced-negative doctor scenarios testable.
- WS proxy simplification policy (user): remove local WS type filtering entirely; keep only auth + request-shape validation and upstream error mapping.

## 2026-03-01

### Planning Defaults (Agent)
- Automation write-quality policy: enforce one mandatory automation best-practice refresh per session before `create`/`update` planning/execution.
- Source-authority policy for automation refresh: use only official Home Assistant docs/integration docs/release notes as authoritative references.
- Write gate policy refinement: if best-practice refresh fails, block automation `create`/`update` and return explicit remediation steps.
- Routing policy refinement: automation write intents must load `ha-automation-best-practices` together with `ha-automation-crud` and `ha-safety`.
- KISS refinement: route automation `delete` directly to `ha-automation-crud` + `ha-safety`; keep best-practice refresh skill only on `create`/`update`.
- Release automation timing policy (user intent): keep `release-please` manual-only (`workflow_dispatch`) until release phase starts; avoid per-merge failure noise on `main`.
- Relay MITM simplification policy (user): `/core` enforces only method + `/api/...` path validation/hygiene and no automation-specific allowlist.
- Live E2E policy refinement: codex harness validates end-user relay `/core` CRUD evidence and must not require client-side `HA_LLAT`.
- Recovery semantics policy: automation reload is optional recovery-only behavior, not a mandatory CRUD success criterion.
- Path-hardening refinement: `/core` rejects single- and double-encoded dangerous path tokens (`%2e`, `%2f`, `%5c`, `%25..`) before forwarding.
- E2E evidence-quality refinement: `/core` usage proofs in live harness must come from executed command records, not raw transcript substring matches.
- Bootstrap deploy reliability refinement: `dev:app:bootstrap` must rebuild the app image after rsync (`ha apps rebuild` fallback `ha apps update`) so synced source changes are actually running.
- Bootstrap token policy refinement: `HA_LLAT` env is optional in `dev:app:bootstrap`; when missing, reuse existing app option `ha_llat` and fail only if both are missing.
- Codex live E2E robustness refinement: evidence extraction for CRUD/core must parse both `command` and `aggregated_output` to handle PTY transcripts.
- Response UX contract policy (superseded by canonical schema below): early draft used `Outcome/State/Key Data/Gate/Next`.
- Question minimization policy refinement: ask at most one blocking question only when safety/ambiguity/required-input prevents deterministic progress; otherwise proceed with opinionated defaults.
- Response schema refinement (superseded by 2026-03-02 domain-first contract): canonical labels are `Outcome`, `Current State`, `Impact`, `Gate`, `Next`, with compact 3-block shape for trivial reads.
- Write confirmation policy refinement: mutation paths require tokenized confirmation (`confirm:<token>`) and reject free-text confirmations.
- Write integrity policy refinement (superseded by 2026-03-02 delete verification refinement): `Changes applied` may be returned only after explicit postcondition verification `passed=true`.
- Cross-session refresh cache policy: automation best-practice snapshots are persisted at `${HOME}/.cache/ha-nova/automation-bp-snapshot.json` and reused while valid.
- Parallel execution preference policy: when client/IDI exposes parallel-subagent capability, independent read stages should use subagent fan-out before sequential fallback.
- Parallel execution enforcement policy: for automation create/update, independent pre-write reads are mandatory in parallel when capability exists; sequential fallback is only for unsupported clients.
- State parsing robustness policy: ws `get_states` consumers must filter object states only and avoid ad-hoc jq schema probing in normal flows.
- Orchestration policy refinement: parallel execution now applies to all independent tasks (not only reads), with fan-out/fan-in and deterministic sequential fallback.
- Shell portability policy refinement: quoting guidance is shell-dependent and standardized to bash-compatible snippets (macOS/Linux/Windows via WSL or Git Bash).
- Subagent trigger policy refinement (superseded by threshold refinement below): earlier rule used `2+ independent tasks`.
- Live E2E policy refinement: harness supports `E2E_SUBAGENT_POLICY=require` and fails when expected subagent delegation is missing.
- Orchestrator consistency policy refinement: align runtime bootstrap/auth safety language between `.agents/skills/ha-nova/SKILL.md` and `skills/ha-nova.md` to prevent executable drift.
- Parallelism policy refinement (superseded by threshold policy for `>=3` substantial units): keep parallel execution mandatory where supported, but keep subagent dispatch outcome-oriented (`preferred for substantial units`, native parallel calls allowed for lightweight units).
- E2E boundary policy refinement: keep mechanism-specific orchestration checks in harness/tests; product skill should define stable behavior, not test-oracle mechanics.
- Harness stability refinement: make onboarding quick gate optional via `E2E_REQUIRE_QUICK_GATE=1` to avoid harness-vs-skill policy conflicts.
- Orchestrator KISS refinement: remove endpoint-specific automation call-budget/recovery rules from top-level skill and keep them in `ha-automation-crud`.
- Single-source drift control: mirror `skills/ha-nova.md` from `.agents/skills/ha-nova/SKILL.md` to keep orchestrator routing/safety/bootstrap rules identical.
- E2E evidence hardening: require ordered CRUD subsequence `PGPGDV` via relay `/core` and explicit verify-absent evidence (`GET` + `404`).
- Direct-REST guard refinement: detect bypass only for automation-config route patterns without `/core`, to reduce false positives from unrelated API curl usage.
- E2E contract resilience: prefer semantic regex invariants over exact sentence matches in harness contract tests.
- Routing safety refinement: device-control intents in top-level orchestrator must route through `ha-control` plus `ha-safety` for write intents.
- E2E evidence authenticity refinement: classify CRUD sequence only from successful `command_execution` curl `/core` entries and enforce ordered subsequence `PGPGDV`.
- E2E transcript robustness: normalize codex JSONL with `jq -Rrc 'fromjson? | select(type == "object")'` before all assertions to avoid malformed-line aborts.
- E2E HTTP-proof policy: count CRUD evidence only from successful curl `/core` command-execution events with explicit HTTP status patterns.
- Hierarchical orchestration policy: enforce a mandatory preflight gate (`independent_units_count`, `subagent_capable`, `fan_out_required`) before first Relay read in non-trivial flows.
- Canonical automation DAG policy: create/update uses strict Phase A parallel fan-out (`A1` best-practice snapshot, `A2` entity resolution from one state snapshot, `A3` id existence check) then Phase B sequential write lifecycle.
- One-state-snapshot policy: full `get_states` is fetched once per flow and reused for filtering unless explicit stale-state reason is stated.
- Response evidence policy (superseded by 2026-03-02 user-focused visibility policy): every non-trivial response must include `Subagent fan-out used: yes/no (reason)`.
- Contract clarity refinement (superseded by 2026-03-02 user-focused visibility policy): orchestration gate fields are computed internally and surfaced only via compact orchestration evidence line by default.
- Contract consistency refinement: mutation status line is mandatory for mutation/debug flows; trivial single-unit read shortcut may omit mutation/orchestration lines.
- Harness evidence hardening refinement: treat only successful curl `/core` command-execution events with `ok:true` + status patterns as valid CRUD evidence.
- Harness sequence refinement: enforce strict ordered CRUD token pattern `^P+G+P+G+D+V+$`.
- Harness correctness refinement: require exactly one final status line in literal `ok` form and forbid `/core` redirect-to-file patterns.
- Subagent threshold refinement (user): mandatory subagent fan-out now applies only at `>=3` substantial independent units; below threshold use native parallel calls in the main agent.
- Orchestration gate refinement: add `substantial_independent_units` as a first-class preflight field to derive `fan_out_required` deterministically.
- Read-shortcut refinement: for non-trivial reads (`independent_units_count >= 2`), bypass shortcut and run full orchestration gate with threshold-based native-vs-subagent selection.
- Substantial-unit rubric refinement: classify units as substantial when they involve full-state discovery, refresh/validation that can block writes, or existence checks that alter write-path branching.
- Drift/legacy guard refinement: contract tests must assert mirror-file equality for `ha-nova` skill docs and explicitly reject reintroduction of legacy `2+ independent` subagent wording.

## 2026-03-02

### Planning Defaults (Agent)
- Response visibility policy refinement (user): default output is user-focused; hide orchestration internals in normal success paths.
- Response schema embedding policy (user, superseded by same-day domain-first contract): keep top-level `Outcome/Current State/Impact/Gate/Next` and embed fixed domain schemas inside `Impact`.
- Domain schema policy (user): first mandatory schemas are `automation` and `script`.
- Automation impact contract refinement (superseded by same-day domain-first contract): require `Automation Name`, `Automation Goal`, `Entities Used`, `Behavior Summary`, `Change Scope`.
- Script impact contract refinement (superseded by same-day domain-first contract): require `Script Name`, `Script Goal`, `Inputs/Variables`, `Actions/Entities Used`, `Change Scope`.
- Orchestration evidence visibility refinement: do not expose `A1/A2/A3`, fan-out decisions, or gate counters unless diagnosing failures or explicitly requested by user.
- Fast-pass modularity policy refinement: define reusable write-flow blocks (`B0`..`B11`) and compose intent-specific fast passes to avoid duplicated logic.
- Domain-first response contract policy (latest): replace legacy default heading structure in normal write flows with domain fields + single `Next Step`.
- Shell stability policy refinement: in complex multi-line command paths use `bash -lc`; avoid shell-specific builtins and temporary helper scripts in normal flow.
- Subskill topology policy refinement: split `ha-nova` into router + `core/*` + `modules/*` files so growth happens by adding modules, not by expanding the router.
- Lazy discovery policy refinement: intent resolution must load only the mapped module set from `core/discovery-map.md`; no preload of all modules in normal flow.
- Compatibility migration policy refinement: keep legacy top-level domain skills as temporary shims while source-of-truth moves to `skills/ha-nova/modules/*`.
- Read mapping policy refinement: `automation.read` and `script.read` must include resolve modules to support deterministic single-item reads.
- Companion routing policy refinement: automation write intents must explicitly include `ha-automation-best-practices` and `ha-safety` alongside lazy module loading.
- B4 scope refinement: best-practice gate block (`B4_BP_GATE`) applies to automation `create`/`update` only in MVP.
- Lazy discovery alias policy refinement (superseded by explicit list mapping refinement below): `*.list` intents are explicit aliases of `*.read` mappings to prevent read/list drift.
- Routing path normalization policy refinement: use repo-root-qualified module/core paths in router mapping for deterministic file resolution.
- Read/list block-scope refinement: `*.list` reuses read modules but skips `B3_ID_RESOLVE`; `B3` remains mandatory only for single-item read flows.
- Discovery-map path resolution refinement: use repo-root-qualified module paths in `core/discovery-map.md` to avoid ambiguous relative resolution from `core/`.
- Explicit list mapping refinement: `*.list` intents map directly to `modules/*/read.md` (without `resolve.md`) to avoid implicit `B3_ID_RESOLVE`.
- Intent classifier refinement: lazy-discovery intent set is explicitly `create|update|delete|read|list`.
- Foundation hardening planning default (2026-03-02): execute a 1-day minimal-scope hardening pass in order `canonical intent contract -> test decomposition -> semantic assertions -> token contract hardening -> duplication reduction`.
- Canonical mapping default (2026-03-02): introduce `skills/ha-nova/core/intents.md` as single source of truth for intent companions/modules before further modular growth.
- Canonical intent contract decision (2026-03-02): `skills/ha-nova/core/intents.md` is the only normative source for `required_companions[]` and `modules[]`; router/discovery are references/protocol only.
- Test architecture decision (2026-03-02): split skill contracts into domain suites (`ha-nova`, `ha-entities`, `ha-safety`) plus one cross-skill integration suite; keep `ha-nova-skill-contract.test.ts` as compatibility shim only.
- Semantic assertion decision (2026-03-02): validate intent mappings via parsed exact sets from canonical matrix; avoid line-break-dependent `toContain` checks for routing semantics.
- Confirm token hardening decision (2026-03-02): require explicit TTL (10m), one-time-use replay rejection, and binding to method/path/target + preview digest.
- De-duplication decision (2026-03-02): keep normative response/safety/verification contract text only in `core/contracts.md`; router references core contract instead of duplicating fields.
- Follow-up hardening default (2026-03-02): execute residual-risk closure as 3 small PRs (token validator tests -> intent dispatcher simulation -> shim strengthening), test-only helpers first, no runtime behavior change.
- Follow-up PR-1 implementation decision (2026-03-02): encode confirmation-token semantics as executable test helper (`validateAndConsumeConfirmToken`) with explicit remediation flags and consumption lifecycle checks.
- Follow-up PR-2 implementation decision (2026-03-02): add dispatcher simulation from canonical intent matrix and enforce full intent-key coverage parity (`parseIntentMatrix` keys == expected keys).
- Follow-up PR-3 implementation decision (2026-03-02): compatibility shim must include semantic anchor checks across split suites, not file-existence checks only.
- Runtime utility extraction decision (2026-03-02): move confirmation-token validation and intent-dispatch resolution into `src/skills/contracts/*`; tests consume runtime utilities instead of duplicating helper logic.
- Drift-reduction decision (2026-03-02): centralize canonical expected intent semantics in `tests/skills/helpers/expected-intent-matrix.ts` and reuse across dispatcher/contract suites.
- Shim hardening refinement (2026-03-02): compatibility shim uses executable semantic smoke checks (matrix parity + dispatcher resolution + stale-token contract) instead of string-anchor-only checks.
- Live E2E sequence-gate default (2026-03-02): accept at most one optional pre-create read evidence token before CRUD write/read flow via `^[GV]?P+G+P+G+D+V+$` to prevent false negatives when existence checks run first.
- Consent tier refinement (2026-03-02): writes use tiered confirmation: `create/update` accepts natural confirmation phrases bound to active preview; `delete/destructive` remains token-only.
- Preview binding refinement (2026-03-02): both natural and token confirmations require active `preview_id` + digest binding to prevent stale-preview applies.
- Response contract refinement (2026-03-02): default write output is compact block-first v1 (`Automation|Script`, `Entities`, `Behavior`, `Open Decisions`, `Next Step`).
- Orchestration latency refinement (2026-03-02): simple flows (`<=2` substantial units) default to serial fast-path; subagents only when expected latency savings exceed spawn/wait overhead.
- Delete verification refinement (2026-03-02): status-only delete success is disallowed; verify-absent read-back is mandatory.
- Automation YAML compatibility refinement (2026-03-02): canonical write shape is plural top-level keys (`triggers`, `conditions`, `actions`) while read/verify normalizes singular+plural keys for backward compatibility.
- Subskill packaging policy (2026-03-02): installable HA NOVA task skills use hyphen-only names (no colon in technical skill names) for cross-client compatibility.
- Resolve granularity policy (2026-03-02): keep resolve as shared internal module and expose one optional expert subskill (`ha-nova-resolve-targets`) for explicit pre-write resolution.
- Multi-client installer policy refinement (2026-03-02): `install:codex-skill`, `install:claude-skill`, `install:opencode-skill`, and `install:skills` install top skill plus all HA NOVA task subskills.
- Skill-system refactor default (2026-03-02): consolidate HA NOVA operations to 5 installable skills (`ha-nova`, `ha-nova-write`, `ha-nova-read`, `ha-nova-entity-discovery`, `ha-nova-onboarding`) and remove intent-split CRUD micro-skills.
- Write-flow orchestration default (2026-03-02): enforce `Resolve (Agent) -> Preview/Decide (Main) -> Apply/Verify (Agent)` for automation/script mutations, with serial inline fallback when agent dispatch is unavailable.
- Update merge default (2026-03-02): updates are full replacement payloads built from `current_config` + explicit user changes; unspecified fields are preserved from existing config.
- Best-practice gate default (2026-03-02): stale snapshot is advisory for simple automations and hard-blocking only for complex automations (`>=3` triggers or `>=3` actions).
- Installer migration safety default (2026-03-02): legacy skill directories are archived to timestamped backups during install; no destructive legacy cleanup by default.
- Router portability default (2026-03-02): routing documentation in `ha-nova` uses skill names, not client-specific filesystem paths.
- Subskill bootstrap default (2026-03-02): every operational HA NOVA subskill (`write`, `read`, `entity-discovery`, `onboarding`) must self-resolve `NOVA_REPO_ROOT` with repo-root fallback before path-based references or command snippets.
- Write-skill context efficiency default (2026-03-02): use phased lazy-loading (`resolve` refs first, `best-practices` only on BP evaluation, `apply` template only after confirmation) and inject only minimal `relay-api` excerpts into subagent prompts.
- Agent dispatch explicitness default (2026-03-02): `ha-nova-write` must document concrete subagent dispatch mechanics (tool type, placeholder substitution, and relay-excerpt injection rules) to avoid trial-and-error execution.
- Agent credential handling default (2026-03-02): never inject `RELAY_AUTH_TOKEN` into agent prompt text; inject only `RELAY_BASE_URL` and let agents read relay token from macOS Keychain service `ha-nova.relay-auth-token` at runtime.
- Relay CLI wrapper default (2026-03-02): agent-dispatched HA calls must go through `~/.config/ha-nova/relay`; no direct `curl` contract excerpts in agent prompts.
- Write bootstrap simplification default (2026-03-02): `ha-nova-write` bootstrap validates `~/.config/ha-nova/relay health` instead of session `eval`-based env injection.
- Agent-template placeholder reduction default (2026-03-02): remove `{RELAY_BASE_URL}` and `{RELAY_API}` from resolve/apply templates; keep only domain/operation/user/payload identifiers.
- Public-release bootstrap default (2026-03-02): all operational subskills (`write`, `read`, `entity-discovery`, `onboarding`) must use relay-cli bootstrap (`~/.config/ha-nova/relay`) and must not use repo-root/eval bootstrap patterns.
- Resolve output refinement default (2026-03-02): replace generic preview fielding with actionable `SUGGESTED_ENHANCEMENTS` in resolve output and write preview contract.
- Apply consistency default (2026-03-02): create/update flows include explicit post-write domain reload (`automation.reload` / `script.reload`) and expose `reloaded` status in apply result.
- Router alignment default (2026-03-02): router bootstrap must follow relay-cli-first pattern (no repo-root bootstrap), and response block naming must use `Suggested Enhancements` consistently with write-skill preview contract.
- Resolve guidance default (2026-03-02): `SUGGESTED_ENHANCEMENTS` section includes concrete common-pattern examples (for cover/light/motion/presence) to reduce ambiguous LLM outputs.
