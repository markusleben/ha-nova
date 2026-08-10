# AGENTS.md

Version: 0.25 (2026-02-01)

Start: say hi + 1 motivating line.
Work style: Be radically precise. No fluff. Pure information only (drop grammar; min tokens).

## Project
- GitHub User: see `.env` -> `GH_USERNAME`

## Agent Protocol
- Contact: Markus Leben (@markusleben).
- “Make a note” => edit AGENTS.md (Ignore `CLAUDE.md`, symlink for AGENTS.md).
- Editor: `cursor <path>`.
- New deps: quick health check (recent releases/commits, adoption).
- When asked to update the `AGENTS.md` to the latest version:
  1. Fetch `https://raw.githubusercontent.com/markusleben/agents.md/main/AGENTS.md`.
  2. Check if a newer version exists and merge without losing local changes.

## Skill Files — English Only [DON'T SKIP – IMPORTANT]
- ALL skill files (`skills/**/*.md`, agent templates, reference docs) MUST be 100% English.
- No German text — not in examples, not in dispatch tables, not in comments, not in inline strings.
- The only exception is proper nouns (attribution names, entity IDs like `light.wohnzimmer` from a real HA instance).
- Output localization (translating headings/labels for the user) happens at runtime per the auto-loaded `skills/ha-nova/` skill — never baked into skill source files.

## Code Quality [DON'T SKIP – IMPORTANT]
- All generated code must be self-reviewed before being presented.
- Review until a pass returns no REAL defect after realness/severity triage; at most two passes. Remaining edge-case depth goes into the PR body as a known limit. "Until no further issues are found" is unreachable on spec-shaped work and was the same uncapped loop one layer down.
- Do not show partial or unreviewed code to the user.

## Guardrails
- ALWAYS review the written code before presenting it — under the two-pass cap above, not until the review runs dry.
- Use `trash` for deletes.
- Use `mv` / `cp` to move and copy files.
- Bugs: add regression test when it fits.
- Keep files <~400 LOC; split/refactor as needed.
- Simplicity first: handle only important cases; no enterprise over-engineering.
- New functionality: small OR absolutely necessary.
- NEVER delete files, folders, or other data unless explicitly approved or part of a plan.
- NEVER run `gh workflow disable`/`enable`, change branch protection/rulesets, or flip repository settings to get past a blocker unless the user explicitly ordered that exact change. Report the blocker instead. Disabled workflows deliver no events, so required checks silently never appear; `scripts/release/verify-required-workflows-active.sh` fails inside `cloud-source-gate` (every PR), the weekly release-pipeline audit, and the release preflight when a guarded workflow is not active. Known limit: a disabled `cloud-source-gate` cannot report itself — PRs then hang with the required check absent until the weekly audit or preflight catches it.
- Census production isolation (#446): tests, smokes, releases, and deployment verification never call the production census `/ping` or `/withdraw`; functional census checks run only against the isolated test Worker (`wrangler@4.113.0 deploy --env test` + `scripts/release/verify-census-functional.sh`). Enforced by `scripts/test/check-census-production-isolation.mjs`.
- Before writing code, strictly follow the research rules below.

## Research
- Always create a spec, even if minimal
- Prefer skills if available over research
- Prefer researched knowledge over existing knowledge when skills are unavailable
- Research: Exa to websearch early, and Ref to seek specific documention or web fetch.
- Best results: Quote exact errors; prefer 2025-2026 sources.
- **Upstream drift check (decision 2026-08-05):** before every release cycle — and ad hoc when a monthly HA release lands — screen upstream deltas against skill-pinned surfaces: the HA monthly release post, the HA developer-blog breaking changes, and HACS releases since the last logged check. Fixed question: "which new/changed features touch ANY WS/REST surface a skill pins?" — the authoritative surface inventory is every pinned WS/REST command anywhere in `skills/**/*.md` (SKILL files AND their reference docs — Relay Contract sections name the wrappers, Flow sections and references carry the concrete endpoints; screen all; never a copy in this rule; examples of the classes involved: update/backup/calendar/statistics/lovelace/config-entries/repairs/assist/service schemas, entity/device registry, energy prefs, person/zone/tag/auth, HACS WS commands). Log every run — hits AND clean passes — dated and append-only in `docs/work/upstream-drift-log.md`; each skill-relevant delta becomes an issue or joins the running PR train. Rationale: HA 2026.7's frontend-only "Update all" changed batch-update semantics the updates skill had to model; release-cadence screening catches this class before users do.

## Git
- Always use `gh` to communicate with GitHub.
- **Multi-Account:** Before remote ops (push, repo create, PR), run `gh auth status`. If the active account is not the `GitHub User` from Project above, ask user before proceeding.
- Use `gh auth switch --user <GitHub User>` to switch between GitHub accounts.
- GitHub CLI for PRs/CI/releases. Given issue/PR URL (or `/pull/5`): use `gh`, not web search.
- Examples: `gh issue view <url> --comments -R owner/repo`, `gh pr view <url> --comments --files -R owner/repo`.
- Conventional branches (`feat|fix|refactor|build|ci|chore|docs|style|perf|test`).
- Safe by default: `git status/diff/log`. Push only when the user asks — except on a branch with an open PR the user asked you to drive, where the checklist's push steps are pre-authorized for that PR. Never push to `main`.
- `git checkout` ok for PR review / explicit request.
- Branch changes require user consent — meaning switching away from a branch with uncommitted work, or moving work between branches. Creating a fresh branch off clean `main` for the task at hand is pre-authorized, and required by Worktree simplicity.
- Destructive ops forbidden unless explicit (`reset --hard`, `clean`, `restore`, `rm`, …).
- No repo-wide S/R scripts; keep edits small/reviewable.
- Avoid manual `git stash`; if Git auto-stashes during pull/rebase, that’s fine (hint, not hard guardrail).
- If user types a command (“pull and push”), that’s consent for that command.
- Big review: `git --no-pager diff --color=never`.
- **Worktree simplicity:** keep local `main` clean; do not do feature work there. Create a `git worktree` per topic — a new worktree is not a "branch change" and needs no separate consent. This is what makes "use waiting time for the next independent work item" legal during a review-loop poll.
- **One topic, one branch:** avoid mixed runtime/docs/release/installer branches unless the behavior truly cannot be separated.
- **Dirty legacy worktrees are read-only source material:** port changes explicitly into clean branches; do not PR or release directly from them.
- **README is stable release truth:** do not add future feature claims to `README.md`; keep planned or unreleased claims in active `docs/work/*` notes until release.
- **No concrete version-number claims in README:** version requirements live in `version.json` (SSOT) and surface via the built-in runtime warning; the README references the mechanism, never a number that can drift ahead of the stable tag.
- **Release-prep merge starts the tag sequence:** a merged release-prep PR (version bump + release notes + release-bound README edits) must be followed immediately by the RC/final tag flow on that commit; release-bound README changes go only into that PR to keep the main-ahead-of-stable window at minutes, not days.
- **README gate (enforced):** `readme-release-gate` fails any PR that touches `README.md` without bumping `version.json` in the same PR, unless a maintainer adds `readme-stable:approved` (corrections describing the CURRENT stable only). Unreleased feature claims collect in the active `docs/work/next-release-body.md` draft (version-free by design — the release-prep PR renames it to `<version>-release-body.md` once it picks the number, which no earlier PR may presume) until the release-prep PR.
- **Local PR checkpoint:** before creating any PR, show the user `git status --short --branch`, `git diff --stat`, public-claim diffs such as `README.md`, targeted tests run, and remaining risks.
- **KISS process rule:** prefer fewer rules and fewer files; add process docs/scripts only when they remove a recurring real problem.
- **Review clearance is commit-specific:** any new relevant delta after the last bot-reviewed commit invalidates prior review clearance.
- **Release-bound means:** the PR bumps `version.json`/`package.json`, or touches `scripts/release/`, the install/update flow, release workflows, or Go code. **High-risk means:** the categories named in the pre-PR review depth rule. Everything else is routine — skills and docs changes are NOT release-bound merely because skills ship in releases.
- **Relevant delta means:** any code, tests, docs-that-change-behavior, scripts, workflow files, release metadata, installer/update flow, or release notes change that alters the commit to merge or tag.
- **Push is not review:** a new push never inherits the previous clean review state.
- **Push fast-path (decision 2026-08-05):** targeted tests for the touched areas + explicit `npm run typecheck` green right before → `git push --no-verify` is ok (skips the 3-5 min pre-push hook). Without BOTH conditions: never `--no-verify` — vitest does not typecheck and a skipped hook once broke `main` (#297/#300); on a push timeout retry the same push.
- **A clean bot result is SHA-specific:** it applies only to the exact commit SHA it reviewed; any later SHA is unreviewed until the full cycle completes again.
- **Codex advisory rule:** `codex-review-gate` is advisory on `main`; do not treat it as a required branch-protection gate for routine PRs.
- **No local-only release shortcuts:** if a follow-up fix matters enough to keep, it must go through GitHub review before merge/tag/release.
- **Release/tag/publish gate:** never create/move a release tag, start RC/final publish, or call a commit release-ready unless the exact remote commit state intended for tag/release is represented by the latest fully reviewed PR state with no unreviewed deltas beyond it.
- **Release rehearsal gate (conditional, decision 2026-07-13):** before any tag/publish, ALWAYS run `HA_NOVA_RELEASE_AUDIT_REQUIRE_BYPASS=1 bash scripts/release/verify-release-pipeline.sh` (strict mode, admin `gh auth`) plus a dispatched green `e2e-disposable-ha.yml` on the commit being tagged. The `vX.Y.Z-rcN` tag-first rehearsal is REQUIRED only when the release changes delivery machinery (install scripts, `scripts/release/`, `.goreleaser.yml` beyond notes text, release workflows/ruleset, Go code, onboarding/update flow) — skills/docs-only releases may tag the final `vX.Y.Z` directly; the final tag's own `release.yml` smoke plus a local public-install verification after publish stay mandatory either way. When in doubt, run the RC. See `docs/releasing.md` → "Release Candidate Gate". Never reintroduce an auto-publish step in `release-candidate.yml`.
- **Local Codex diff review before every PR (decision 2026-08-04):** before the first push/`gh pr create`, run a local Codex CLI review over the uncommitted working-tree diff and fix its findings — the bot round on the PR then ideally stays a single formal clearance instead of a commit-burning fix loop.
- **Release-bound review hardening:** do local/self review before opening the PR. After the PR exists, use the fast path only: after each relevant fix, run targeted local verification, push immediately, and trigger `@codex` immediately. After PR creation, Codex bot + CI are the review path; do not add extra SERIAL local review gates between a fix and its push. The parallel batch at round five is the one exception and is mandatory (see Post-PR batch trigger).
- **Pre-PR adversarial review depth (decision 2026-07-28):** choose review depth by risk, not raw diff size. Run 2-3 adversarial subagents for security/auth/secrets, concurrency/races, irreversible data changes, release/installer/update machinery, or new cross-component contracts/invariants; these high-risk categories always take precedence. Run 1 adversarial subagent for meaningful but localized behavior changes. Outside the high-risk categories, skip subagent review for docs-only, tests-only, generated/lockfile-only, mechanical changes, or localized bug fixes with regression coverage.
- **Pre-PR size signal:** count production behavior only; exclude tests, docs, generated files, and lockfiles. More than ~800 changed production lines or 15 runtime files requires splitting the PR or documenting why it remains one change; size alone does not determine reviewer count.
- **Pre-PR adversarial batch execution:** review the final pre-PR diff once with the brief "refute this change: find every code path that bypasses the new invariant, every selection/state source not covered, every ordering that strands state" — fix ALL findings in one batch, THEN open the PR. Repeat only when scope or the protected invariant changes. Rationale: batching upfront cut repeated Codex rounds while avoiding review work driven by test/docs volume.
- **Post-PR batch trigger (MANDATORY, decision 2026-08-10):** after the FIFTH `@codex` round on one PR without a clean verdict, stop answering findings one at a time. Run 2-3 adversarial subagents IN PARALLEL over that PR's full current diff — distinct lenses (correctness/domain vocabulary, races and ordering, cross-file contract consistency), each briefed with the findings already fixed so they do not re-tread them — run what they return through the SAME realness/severity triage as the convergence cap below — the two rules govern different things, the batch decides how you FIND, the cap decides what you FIX — then fix everything that survives triage in ONE commit and trigger Codex once. This is not optional and not a judgement call: a bot round costs 10-15 minutes of latency and returns findings serially, while three agents return a round's worth of ground in one pass. Measured on #513/#527 (2026-08-09/10): ~40 rounds of serial fixing versus 30+ findings in a single parallel pass, including defects the serial loop had not reached in eight hours. Enforced by `scripts/dev/codex-round-guard.mjs`, wired as a PostToolUse hook in `.claude/settings.json`: it counts `@codex` triggers per PR and injects the reminder from round five. Counter state lives in `.git/codex-rounds.json`; reset a PR with `node scripts/dev/codex-round-guard.mjs --reset <nr>`.
- **Finding-class sweep rule (decision 2026-07-21):** never fix only the reported instance of a Codex/CI finding. Before re-triggering `@codex`, sweep the same defect class across every sibling path/file (same invariant, same selection sources, same lifecycle stage) and fix all instances in the same commit. A finding is a symptom of a class until proven unique.
- **Review-loop polling (decision 2026-07-21):** after `@codex` is triggered and checks are green, poll the review channels (reactions, reviews, inline comments, issue comments) every 60-90s instead of multi-minute sleeps; the bot answers in ~5-9 min and every extra idle minute is pure latency. Use waiting time for the next independent work item.
- **Review-loop convergence cap (decision 2026-08-04, KISS/MVP):** per-round fix commits do NOT bloat main (squash merge) and do NOT make reviews heavier (the bot always reviews the full diff, never the commit history) — never close and reopen a PR to "reset" a long review loop; that restarts the whole first review and discards SHA-clearance history and resolved threads. What DOES accumulate is CONTRACT TEXT: every fix that grows a spec/skill file enlarges the next round's attack surface. Therefore: after ~5 Codex rounds without a clean verdict on a contract-heavy PR, switch to triage mode — triage by REALNESS and SEVERITY, never by repair size: a factually real defect gets fixed no matter how large the repair (or the PR does not merge); only edge-case-DEPTH findings without a real defect become in-thread scope answers. A finding whose fix must ADD contract text is a scope signal: prefer shrinking the contract sentence over growing it (delete the promise instead of implementing it).
- **Contract-PR size split (decision 2026-08-04):** specification-like deltas (skill contract + executable oracle + fixtures) are one large review attack surface; when a single issue needs both a substantial contract rewrite AND a new oracle, plan them as two stacked PRs from the start so each review surface stays small.
- **Manifest-label rule:** if a PR changes `package.json`, `package-lock.json`, `nova/package.json`, or `nova/package-lock.json`, add `manifest-review:approved` immediately after `gh pr create` and before `@codex` / `gh pr checks --watch`.
- **Manifest-label invalidation rule:** after any later relevant SHA on a manifest-changing PR (push, force-push, rebase, or cherry-pick rewrite), re-apply `manifest-review:approved` on that current PR state before expecting `manifest-review-gate` to pass.
- **PR Merge / Release Commit Gate — MANDATORY CHECKLIST (do NOT skip any step):**
  The `codex-review-gate` workflow waits ~9 min for the Codex review bot. Bot signals: `eyes` reaction = review in progress, `👍` reaction = no findings, review comments = findings.
  - [ ] 1. `gh pr create ...`
  - [ ] 2. If the PR changes `package.json`, `package-lock.json`, `nova/package.json`, or `nova/package-lock.json`, add the maintainer label — but only after stating in the PR body WHICH manifest lines changed and why (deps added, removed, version-bumped; for a Dependabot-lane bump cite the lane rule). The gate exists to catch supply-chain drift; applying it as a reflex before anything inspected the change makes it green by default: `gh pr edit <nr> --add-label manifest-review:approved`
  - [ ] 2a. If any later relevant SHA lands on that PR, remove/re-add the label on the latest PR state before re-checking: `gh pr edit <nr> --remove-label manifest-review:approved || true && gh pr edit <nr> --add-label manifest-review:approved`
  - [ ] 2b. If the PR changes `README.md` without bumping `version.json`, it fails `readme-release-gate` unless it is a stable-truth correction — then add: `gh pr edit <nr> --add-label readme-stable:approved` (feature/version claims belong in the release-body draft instead). Like 2a, the label is SHA-specific: re-apply it after any later commit on the PR.
  - [ ] 3. For the initial PR SHA and for every later relevant SHA: run targeted local verification only, push immediately if needed, then immediately trigger Codex review/re-review: `gh pr comment <nr> --body "@codex"`.
  - [ ] 4. `gh pr checks <nr> --watch` — wait for ALL required checks; for release-bound/high-risk deltas also wait for `codex-review-gate`
  - [ ] 5. Check bot signal across all channels: `gh api repos/<o>/<r>/issues/<nr>/reactions` (👍 = clean), `gh api repos/<o>/<r>/pulls/<nr>/reviews` (PR-level review findings), `gh api repos/<o>/<r>/pulls/<nr>/comments` (inline findings), and issue/discussion comments on the PR.
  - [ ] 6. If findings OR any new relevant delta is introduced afterward → fix, run targeted verification, push immediately, then **trigger re-review**: `gh pr comment <nr> --body "@codex"` — pushes alone do NOT trigger re-review. Then go back to step 3 for the new SHA.
  - [ ] 7. Resolve ALL review threads before merge (branch protection blocks unresolved):
         `gh api graphql -f query='{ repository(owner:"<o>",name:"<r>") { pullRequest(number:<nr>) { reviewThreads(first:20) { nodes { id isResolved } } } } }'`
         Then for each unresolved: `gh api graphql -f query='mutation { resolveReviewThread(input:{threadId:"<id>"}) { thread { isResolved } } }'`
  - [ ] 8. For release-bound/high-risk deltas, only proceed after an actual Codex bot result for the current latest commit SHA; timeout alone is NOT enough.
  - [ ] 9. For release-bound/high-risk deltas, confirm the PR head SHA is still the same SHA that received the latest clean/current bot result. If SHA changed, or if Codex timed out/skipped and there is still no real/current bot result for that exact SHA, go back to step 3.
  - [ ] 10. `gh pr merge --squash --delete-branch` (use `--admin` only if branch protection blocks after all steps passed)
  - [ ] 11. For squash merge flows, tag/release only the remote merge commit produced from that reviewed PR state; any later delta requires a new PR/review cycle.

## Error Handling
- Expected issues: explicit result types (not throw/try/catch).
  - Exception: external systems (git, gh) → try/catch ok.
  - Exception: React Query mutations → throw ok.
- Unexpected issues: fail loud (throw/console.error + toast.error); NEVER add fallbacks.

## Backwards Compat
- Local/uncommitted: none needed; rewrite as if fresh.
- In main: probably needed, ask user.

## Critical Thinking
- Fix root cause (not band-aid).
- Unsure: read more code; if still stuck, ask w/ short options (A/B/C).
- Conflicts between rules about CODE or DATA risk: stop, call it out, pick the safer path.
- Conflicts between two PROCESS rules (review cadence, gates, ordering): do NOT stop. Pick the path that reaches a reviewed merge fastest, record the conflict in `docs/choices.md`, and continue. The safer-path bias exists for irreversible damage, not for latency — applied to review cadence it always selects "more gates", which is how a 40-round loop happens.
- Unrecognized changes: assume other agent; keep going; focus your changes. If it causes issues, stop + ask user.

## Completion and Autonomy Gate
- Assume "continue" unless the user explicitly says "stop" or "pause".
- Do not ask "should I continue?" or similar questions.
- If more progress is possible without user input, continue.
- BEFORE you end a turn or ask the user a question, run this checklist
-- Answer these privately, then act:
   1) Was the initial task fully completed?
   2) If a definition-of-done was provided, did you run and verify every item?
   3) Are you about to stop to ask a question?
      - If yes: is the question actually blocking forward progress?
   4) Can the question be answered by choosing an opinionated default?
      - If yes: choose a default, document it in `docs/choices.md`, and continue.
- When you choose opinionated defaults, document them in `docs/choices.md` as you work.
- Leave breadcrumb notes in thread and `docs/breadcrumbs.md` (root file stays short/current; long history lives in `docs/archive/breadcrumbs.md` — move entries older than the last release when you touch the file, or it only happens by luck (it is at 27 KB now)).
- When writing to `docs/choices.md` or `docs/breadcrumbs.md` categorize by date (tail)
- If you must ask the user:
-- Ask exclusively blocking question only.
-- Explain why it is blocking and what you will do once answered.
-- Provide your best default/assumption as an alternative if the user does not care.

## Useful Tidbits
- When using Vercel AI Gateway, use a single API key across the project, not individual providers.
- When using Convex, run `bunx convex dev --once` to verify, not `bunx convex codegen`.

## User Notes
Use below list to store and recall user notes when asked to do so.

- Project: ha-nova — Home Assistant AI Integration (Relay + Skills). See `PROJECT.md` for full context.
- Reference docs in `docs/reference/` are mandatory reading before working on Relay or Skills.
- Documentation governance: active product/reference/runbook docs live in `README.md`, `PROJECT.md`, `SUPPORT.md`, `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `docs/reference/`, `docs/releasing.md`, `docs/work/`, per-client install overlays (`.claude/.codex/.gemini/.opencode/.hermes/INSTALL.md`), `nova/DOCS.md`, `nova/README.md`, and `skills/**/SKILL.md`; legacy superpowers history now lives under `docs/archive/superpowers/` and must not receive new active docs.
- For graphics/diagrams, labels must stay consistent across all views (top view, side view, etc.).
- Relay stays dumb, Skills stay smart. No business logic in the server.
- Preferred terminology (2026+): use "App" instead of "Add-on", except where technical API paths force legacy terms (for example `/addons/*`).
- Pairing/onboarding copy rule (2026-08-02): describe pairing by what happens (one-time six-digit code, per-device connection, revocable anytime) — never define the product by the absence of tokens or legacy flows ("no token to copy" confuses users who never saw the old flow). Technical token documentation (standalone container LLAT, relay auth token, OAuth storage) is exempt.
- Priority: deliver a working MVP first, but keep the architecture modular from day one for later extension.
- Skills remain pure `*.md` files; no hidden business logic outside this model.
- Relay implementation must remain lean, clean, and efficient (KISS + DRY, clear responsibilities).
- **UX is king** — the guiding mantra for this project. Always prefer fewer manual steps for the user. When choosing between technical purity and user convenience, convenience wins. This applies from onboarding through skill performance to uninstall. Target audience is not necessarily terminal-savvy.
- PR hygiene (user requirement): proactively check GitHub PR reviews (including inline review comments via `gh api repos/<owner>/<repo>/pulls/<nr>/comments`) without waiting for a reminder.
- Release guard: Claude marketplace sync changes must ship with regression coverage for plain GitHub URL, structured GitHub source, and structured GitHub source with pinned `ref`; pinned refs are release blockers if they compare equal to the floating default source.
- Release notes preference: keep them short and user-centric. Prioritize `New Features`, optional `What To Watch` only for real behavior/breaking/action-needed changes, and selective `Bug Fixes` only for important user-facing fixes. Do not dump every minor fix into release notes.
- Release preflight requirement: before every release/RC/tag/publish flow, proactively audit open PRs with special focus on Dependabot and workflow/release-related PRs. Classify them as `blocker now` vs `separate later`. Never pull in a red or unreviewed workflow/release PR right before publish just because it is open.
- Release-worthiness rule (user requirement): not every merged change deserves an immediate new version or release. Default to batching docs/tests/process-only/internal maintenance into the next real user-facing release unless the change fixes the live release path, published artifacts, installer/update flow, or another issue users can actually hit in the shipped product.
- Dependabot fast-lane rule (user requirement, updated 2026-07-13): automate only the safe lanes. Dev-only npm minor/patch manifest updates, and github-actions minor/patch bumps that change only `uses:` lines (diff-guarded), may auto-approve/auto-merge after required checks pass; action majors, workflow logic changes, and release/installer/runtime/security changes stay manual.
- Toolchain-risk dev dependency rule (user requirement): keep `vitest`, `vite`, `typescript`, `tsx`, `rollup`, `rolldown`, and `esbuild` out of the Dependabot fast lane even when they arrive as dev-only npm minor/patch manifest bumps; those get manual review because they can drag wider toolchain/runtime churn.
- Codex advisory rule (user requirement): on `main`, Codex is a review signal and escalation layer, not a required branch-protection check; for release-bound/high-risk deltas, still wait for a real Codex result on the final SHA.
- Main-branch protection drift check (user requirement): when repo policy changes, verify the live `main` branch protection with `bash scripts/release/verify-github-main-protection.sh` so GitHub settings do not silently drift away from the documented contract.
- Codex review hygiene (user requirement): for release-bound PRs, wait for the real Codex bot response; do not treat workflow timeout as a clean review.
- Review invalidation hygiene (user requirement): if any relevant delta lands after the last reviewed commit, release readiness resets to zero until that exact new commit state completes a fresh PR + `@codex` + real bot response cycle.
- Subagent review hygiene (user requirement, amended 2026-08-10): Codex bot remains the final review instance for merge/tag clearance — a local pass never substitutes for it. What is banned is a SERIAL local gate between a fix and its push, which only adds latency. A PARALLEL adversarial batch is a different thing and is REQUIRED at the fifth round (see Post-PR batch trigger). The original blanket wording — "after PR creation, do not insert extra local subagent review gates" — is deleted: it forbade the one move that ends a long review loop, and it cost an eight-hour night on #513/#527.
- Review speed rule (user requirement): after each relevant push, trigger `@codex` immediately and let CI run; fix only real Codex/CI findings after the PR exists.
