# AGENTS.md

Version: 0.30 (2026-10-14) — bump this header on every local amendment.

Start: say hi + 1 motivating line.
Work style: Be radically precise. No fluff. Pure information only (drop grammar; min tokens).

## Project
- GitHub User: see `.env` -> `GH_USERNAME`
- Contact: Markus Leben (@markusleben).
- "Make a note" => edit AGENTS.md (Ignore `CLAUDE.md`, symlink for AGENTS.md).
- Editor: `cursor <path>`.
- New deps: quick health check (recent releases/commits, adoption).
- When asked to update AGENTS.md to the latest version: fetch `https://raw.githubusercontent.com/markusleben/agents.md/main/AGENTS.md`, check for a newer version, merge without losing local changes.

## Definitions
- **Release-bound:** the PR bumps `version.json`/`package.json`, or touches `scripts/release/`, the install/update flow, release workflows, or Go code.
- **High-risk:** security/auth/secrets, concurrency/races, irreversible data changes, release/installer/update machinery, new cross-component contracts. Skills and docs changes are NOT release-bound merely because skills ship in releases.
- **Relevant delta:** any code, tests, behavior-changing docs, scripts, workflow files, release metadata, installer/update flow, or release-notes change that alters the commit to merge or tag. Clearance is SHA-specific: any relevant delta after the last reviewed commit invalidates prior clearance (until required checks are green on it; a triggered Codex round reviews that head).
- **A review round:** one bot comment carrying `Reviewed commit:` with at least one NEW finding. Timeout rounds and verbatim re-post rounds count in no counter.

## Guardrails [DON'T SKIP – IMPORTANT]
- ALL skill files (`skills/**/*.md`, agent templates, reference docs) MUST be 100% English — no German anywhere; only proper nouns (attribution names, real entity IDs like `light.wohnzimmer`) are exempt. Output localization happens at runtime per `skills/ha-nova/`, never in skill source.
- Self-review all generated code BEFORE the PR checkpoint; at most two passes of the same lens across the mandated pre-PR reviews. Remaining edge-case depth goes into the PR body as a known limit. Post-PR, review is Codex's job. Never show partial or unreviewed code to the user.
- Deletes: use `trash`; `mv`/`cp` for moves/copies; NEVER delete files, folders, branches, or remote state unless explicitly approved or part of an approved plan. Destructive git ops (`reset --hard`, `clean`, `restore`, `rm`, force-push to shared branches) require explicit user consent.
- Bugs: fix the root cause; add a regression test when it fits. Keep files <~400 LOC. Simplicity first: no enterprise over-engineering; new functionality small OR absolutely necessary.
- NEVER run `gh workflow disable`/`enable`, change branch protection/rulesets, or flip repository settings to get past a blocker unless the user ordered that exact change — report the blocker instead.
- **Bare `@codex` only:** the trigger comment body must be exactly `@codex`, single line — the bot silently rejects multi-line bodies ("To use Codex here…") and the trigger never runs. Post context as a separate comment BEFORE the trigger.
- **Stale re-posts are not findings:** the bot sometimes re-posts an earlier round's findings verbatim against a newer SHA. Verify each finding against the current diff; if every item is a verbatim repeat of an already-fixed finding, the round is clean — resolve the threads, do not refute, count nothing.

## Research
- Always create a spec, even if minimal. Prefer skills over research; researched knowledge over memory when skills are unavailable. Exa for early websearch, Ref for specific docs; quote exact errors, prefer 2025-2026 sources.
- **Upstream drift check:** before every release cycle (and ad hoc on a monthly HA release), screen HA release post + dev-blog breaking changes + HACS releases against every WS/REST surface pinned anywhere in `skills/**/*.md`. Log every run — hits AND clean passes — append-only in `docs/work/upstream-drift-log.md`; each skill-relevant hit becomes an issue or joins the running PR train.

## Git
- Always use `gh` for GitHub. Before remote ops run `gh auth status`; if the active account differs from `GH_USERNAME`, ask before proceeding (`gh auth switch --user <user>`).
- Given an issue/PR URL: use `gh` (`gh issue view --comments`, `gh pr view --comments --files`), not web search.
- Conventional branches (`feat|fix|refactor|build|ci|chore|docs|style|perf|test`). One topic, one branch.
- Safe by default: `git status/diff/log`. Push only when the user asks — except the first push of a branch whose PR the user asked you to open, and pushes on a PR the user asked you to drive. Never push to `main`; always `git push origin <branch>` (bare `git push` pushes all branches).
- **Worktree simplicity:** keep local `main` clean; one `git worktree` per topic. Creating a fresh branch off clean `main` — or a stacked branch off a branch you own for this task — is pre-authorized. Switching away from uncommitted work, or moving work between branches, needs consent. `git checkout` is ok for PR review or on explicit request; a command the user types ("pull and push") is consent for that command. Dirty legacy worktrees are read-only source material.
- **Push fast-path:** targeted tests for the touched areas + explicit `npm run typecheck` green right before → `git push --no-verify` ok (skips the 3-5 min hook; note the hook also runs build + `check-docs.sh` — run `check-docs.sh` too on docs-touching diffs). Without BOTH conditions never `--no-verify`; on a push timeout retry the same push. Wherever this file says "targeted local verification" it means exactly these fast-path conditions.
- No repo-wide S/R scripts; keep edits small/reviewable. Big review: `git --no-pager diff --color=never`. Auto-stash during pull/rebase is fine; avoid manual `git stash`.

## Before you open a PR
- **Adversarial review depth by risk:** high-risk categories → 2-3 parallel adversarial subagents over the final pre-PR diff, briefed "refute this change: find every code path that bypasses the new invariant, every selection/state source not covered, every ordering that strands state"; meaningful localized behavior change → 1 subagent; docs-only, tests-only, generated/lockfile-only, mechanical, or regression-covered localized fixes → skip. Fix ALL findings in one batch, THEN open the PR; repeat only when scope or the protected invariant changes.
- **Release-bound only:** additionally run a local Codex CLI review over the working-tree diff and fix its findings first.
- **Size signal:** >~800 changed production lines (tests/docs/generated/lockfiles excluded) or 15 runtime files → split the PR or document why it stays one change. When one issue needs both a substantial contract rewrite AND a new executable oracle, plan them as two stacked PRs from the start — each review surface stays small.
- **Local PR checkpoint:** show the user `git status --short --branch`, `git diff --stat`, public-claim diffs (`README.md`), tests run, remaining risks.

## Review triage and round cap
- **P1** (real defect on an executed/shipped path — security, data loss, release gate, user-visible break): always fix, regardless of repair size.
- **P2** (real but bounded — edge case with workaround, internal-only): fix — unless the fix must ADD contract text; that is a scope signal: prefer shrinking the contract sentence (delete the promise), or answer in-thread.
- **P3** / style / hypothetical-depth: answer in-thread, never a commit.
- **Finding-class sweep:** for every NEW finding, sweep the same defect class across every sibling path/file and fix all instances in the same commit. Never re-sweep for a re-posted finding.
- **Max ONE Codex round per PR**, then merge on green required checks. Routine PRs need none; release-bound or high-risk PRs may trigger one (a timeout is not a verdict — proceed on green checks).
- **Parallel batch (MANDATORY, release-bound/high-risk only):** the first round returning >2 new real findings switches to batch mode — run 2-3 adversarial subagents in parallel over the full current diff (distinct lenses, briefed with what is already fixed), triage their output by the P1/P2/P3 rules, fix everything surviving in ONE commit, trigger once. Never close/reopen a PR to reset a review loop.
- Poll review channels every 60-90 s after a trigger; use waiting time for the next independent work item.

## PR Merge Checklist (MANDATORY for agent-authored PRs)
- [ ] 0. Pre-PR block above done: self-review, targeted tests, size signal, checkpoint.
- [ ] 1. `gh pr create ...`
- [ ] 2. Release-bound/high-risk (optional): post context as a separate comment, then trigger once with `gh pr comment <nr> --body "@codex"` (bare, single-line). Verdict = bot comment whose `Reviewed commit:` equals `gh pr view <nr> --json headRefOid`; check reviews and inline comments with `--paginate`. Triage findings per Review triage; fix and push.
- [ ] 3. `gh pr checks <nr> --watch` until every required check is green.
- [ ] 4. Resolve ALL review threads (`gh api graphql` `reviewThreads` query, then `resolveReviewThread` per id), then `gh pr merge <nr> --squash --delete-branch`. `--admin` only when the sole blocker is the self-approval requirement of this solo-maintainer repo — never against a failing or absent check. Tag/release only the merge commit of the reviewed PR state.

## Release & Tagging
- **Release/tag/publish gate:** never create/move a release tag or call a commit release-ready unless the exact remote commit is the merge commit of a PR that passed the PR Merge Checklist (self-review, green required checks, triaged Codex findings when a round ran) with no later deltas. No local-only release shortcuts.
- **Rehearsal gate:** before any tag/publish, a dispatched green `e2e-disposable-ha.yml` on the commit; while Cloud is enabled, the per-release Cloud checklist in `docs/releasing.md`. `docs/releasing.md` -> "Release Candidate Gate" decides whether an `vX.Y.Z-rcN` tag-first rehearsal is required (canonical trigger list) AND carries the remaining mandatory-either-way gates (the final tag's `release.yml` smoke, post-publish public-install verification). Never reintroduce auto-publish in `release-candidate.yml`.
- **Cloud platforms:** `cloud_remote_platforms` is `["darwin","linux","windows"]` (v0.25.1). Shrinking the platform list is a user-facing feature removal and ALWAYS a blocking user decision (Backwards Compat rule) — never an autonomous default. Manual RC lanes for an OS are never skipped silently: state the no-delta case in the release PR.
- **Release-prep merge starts the tag sequence** (bump + notes + release-bound README edits in one PR, tag immediately after). README is stable-release truth: no future claims, no concrete version numbers (`version.json` is SSOT). Unreleased claims collect in `docs/work/next-release-body.md` (version-free; release-prep PR renames it).
- **Release-worthiness:** batch docs/tests/process/internal maintenance into the next real user-facing release unless the change fixes the live release path, published artifacts, installer/update flow, or a shipped-product issue.
- **Release notes:** short and user-centric — `New Features`, optional `What To Watch` (real behavior/breaking/action-needed only), selective important `Bug Fixes`.
- **Preflight:** before every release/RC/tag flow audit open PRs (Dependabot + workflow/release PRs first): `blocker now` vs `separate later`; never pull in a red or unreviewed workflow/release PR right before publish.

## Dependabot
- Dependabot PRs are reviewed and merged manually like any routine PR; toolchain bumps (`vitest`, `vite`, `typescript`, `tsx`, `rollup`, `rolldown`, `esbuild`) get a full local `npm run verify` first.

## Error Handling
- Expected issues: explicit result types (not throw/try/catch). Exceptions: external systems (git, gh) → try/catch ok; React Query mutations → throw ok.
- Unexpected issues: fail loud (throw/console.error + toast.error); NEVER add fallbacks.

## Backwards Compat
- Local/uncommitted: none needed; rewrite as if fresh.
- In main: probably needed — ask the user. This is a blocking question; the Autonomy Gate's default-picking never applies (a wrong default breaks someone's working setup).

## Critical Thinking
- Fix root cause, not band-aid. Unsure: read more code; if still stuck, ask with short options (A/B/C).
- Conflicts about CODE/DATA risk or irreversible remote state (tag, release, publish, delete): stop, call it out, pick the safer path.
- Conflicts between two PROCESS rules: do not stop — pick the path that reaches a reviewed merge fastest, except rules marked MANDATORY/DON'T SKIP; record the conflict in `docs/choices.md`, continue.
- Unrecognized changes: assume another agent; keep going, focus your changes; if it causes issues, stop + ask.

## Completion and Autonomy Gate
- Assume "continue" unless the user says stop/pause; if more progress is possible without user input, continue — never ask "should I continue?".
- Before ending a turn: task fully done? definition-of-done verified? question actually blocking? If a default answers it: pick it, log it in `docs/choices.md` (dated, tail-appended), continue. Leave breadcrumbs in `docs/breadcrumbs.md`, dated, tail-appended (short/current; older-than-last-release entries move to `docs/archive/breadcrumbs.md` when you touch the file).
- Blocking questions only — explain why blocking and offer your best default. Always-blocking exceptions: deletions, wrong `gh` account, `README.md` edits outside a release-prep PR, Backwards Compat in main.

## Project Conventions
- ha-nova = Home Assistant AI Integration (Relay + Skills); full context in `PROJECT.md`. Reference docs in `docs/reference/` are mandatory reading before Relay or Skills work.
- Relay stays dumb, Skills stay smart — no business logic in the server; Skills remain pure `*.md`; Relay stays lean (KISS + DRY). MVP first, modular from day one.
- **UX is king:** fewer manual steps beats technical purity, from onboarding to uninstall; target audience is not terminal-savvy.
- Terminology: "App", not "Add-on" (legacy API paths like `/addons/*` exempt). Pairing copy: describe what happens (one-time six-digit code, per-device, revocable) — never by absence of tokens/legacy flows; technical token docs exempt.
- Documentation governance: active docs live in `README.md`, `PROJECT.md`, `SUPPORT.md`, `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `docs/reference/`, `docs/releasing.md`, `docs/release-validation.md`, `docs/work/`, per-client install overlays (`.claude/.codex/.antigravity/.opencode/.hermes/INSTALL.md`), `nova/DOCS.md`, `nova/README.md`, `skills/**/SKILL.md`; `docs/archive/superpowers/` receives no new active docs.
- KISS process rule: prefer fewer rules and fewer files; add process docs/scripts only for recurring real problems.

## User Notes
Use below list to store and recall user notes when asked to do so.

- Release guard: Claude marketplace sync changes ship with regression coverage for plain GitHub URL, structured GitHub source, and structured source with pinned `ref`; a pinned ref equal to the floating default source is a release blocker.
- PR hygiene: proactively check PR reviews incl. inline comments (`gh api repos/<o>/<r>/pulls/<nr>/comments`) without waiting for a reminder.
