# AGENTS.md

Version: 0.34 (2026-10-21) — bump this header on every local amendment.

Instructions for coding agents in this repo. "Hard rules" protect real homes, credentials, and the release path; no other section can trade them off. The other sections are the standard workflow; where two of them conflict, see Autonomy.

## Working with the maintainer
- Start: say hi + 1 motivating line (first reply of an interactive session only).
- Work style: Be radically precise. No fluff. Pure information only (drop grammar; min tokens). User-facing replies and public texts still stay readable: outcome first, then evidence.
- Contact: Markus Leben (@markusleben). GitHub account: `markusleben` (`GH_USERNAME` in the main checkout's `.env`; worktrees have none).
- "Make a note" → edit this file (`AGENTS.md`; ignore `CLAUDE.md`, a symlink to it) — User Notes or the fitting section — in a `docs/agents-*` worktree off `main` (or the open AGENTS PR), and bump the header.
- To open a file for the maintainer: `cursor <path>`.
- "Update AGENTS.md to the latest version" → fetch `https://raw.githubusercontent.com/markusleben/agents.md/main/AGENTS.md`, merge newer upstream rules without losing local ones.

## Project
- ha-nova = Home Assistant AI integration: a lean Relay (`nova/`), pure-Markdown Skills (`skills/`), and a Go CLI (`cli/`). Context: `PROJECT.md`. Before Relay or Skills work, read the matching references: `docs/reference/skill-architecture.md`, `bridge-architecture.md`, `ha-api-matrix.md`, `client-integration.md`, `safety.md`, `testing.md`.
- Relay stays dumb, Skills stay smart: no business logic in the Relay; Skills stay pure `*.md`. KISS + DRY, MVP first, modular from day one.
- UX first: fewer manual steps beat technical purity, from onboarding to uninstall. The audience is not terminal-savvy.
- Terminology: "App", not "Add-on" (legacy API paths like `/addons/*` exempt). Pairing copy says what happens (one-time six-digit code, per-device, revocable), not what is absent; technical token docs exempt.
- Terminology: "Home Assistant Link" (formerly Home Assistant Cloud; Nabu Casa) in user copy; technical ids (`cloud` command, domain, config keys) stay.
- Skill sources (`skills/**/*.md`, agent templates, reference docs) are English only; proper nouns and real entity IDs (`light.wohnzimmer`) are exempt. Why: output is localized at runtime per `skills/ha-nova/`; `tests/skills/skill-source-language-contract.test.ts` enforces it.
- Public texts (commits, PRs, issues, docs, test names) name no other project and carry no comparison. Comparisons stay internal in `docs/internal/` (gitignored). Why: the repo is public, and comparisons go stale.
- Active docs and their roles: `docs/reference/documentation-governance.md`. `docs/archive/` is history; add no new active docs there.
- Fewer rules and fewer files: add process docs or scripts only for a recurring real problem.
- Code: new files under ~400 LOC, and don't grow a file already over it unless splitting it is the topic; the simplest thing that works; new functionality small or clearly necessary. New dependency → check its health first (recent releases/commits, adoption).
- Errors: expected failures are explicit result types, not throw/catch (try/catch is fine around external systems such as git or gh). Unexpected failures fail loud (throw, non-zero exit, clear message); no fallbacks; they hide bugs.
- Bugs: fix the root cause; add a regression test when it fits. Unsure: read more code before asking.
- Specs: a new feature, contract, or flow gets a short spec in `docs/work/` first; for a fix or small change the PR body is the spec. Prefer skills (project or installed agent skills) over web research, and current docs over memory: use the search/docs tools you have, quote exact errors, prefer recent sources.

## Hard rules
- Never run tests against the maintainer's or any user's live Home Assistant. Throwaway HA is fine: the test bed (`npm run testbed:up|verify|down`, `scripts/e2e/testbed/README.md`), the disposable-HA E2E (`scripts/e2e/disposable-ha/`), and the client matrix (`scripts/e2e/client-matrix/`). The real-device Cloud/Windows steps in `docs/releasing.md` are run by the maintainer — hand them over, don't run them. Why: tests write, delete, and pair against real devices in someone's home.
- On macOS a temporary `HOME` does not isolate the login keychain. Run pairing, `setup --service`, and `uninstall --purge` tests only in the Linux container lane or a separate macOS user/VM (the client matrix enforces this itself). Why: these paths read, and can delete, real HA NOVA credentials (`docs/reference/testing.md`).
- Deletions: delete files, folders, branches, worktrees, or remote state only with the maintainer's approval or as part of an approved plan; use `trash` for anything in the repo or under `$HOME`. Pre-approved: your own temp dirs and scratch files from this task under `$TMPDIR` or the session scratchpad (`rm -rf` on exact paths — `trash` would keep the disk full), your own worktree after its PR merged (`git worktree remove`), and the merged PR's head branch (`--delete-branch`). Never another agent's worktree, a Docker container or volume you didn't start (your own go down only via `npm run testbed:down` or an E2E script's cleanup), or anything else you didn't create. Destructive git (`reset --hard`, `clean`, `restore`, `rm`) and force-push to shared branches (`main`, release branches, a branch someone else pushed) need explicit consent; your own PR branch is not shared.
- Never `gh workflow disable|enable`, change branch protection/rulesets, or flip repository settings to get past a blocker unless the maintainer ordered that exact change; report the blocker instead.
- Never push to `main`; push with `git push origin <branch>` (bare `git push` pushes all branches).
- Backwards compatibility: local/uncommitted work needs none — rewrite as if fresh. Breaking behavior that is already in `main` needs the maintainer's answer first; never pick a default here, because a wrong one breaks someone's working setup. Shrinking `cloud_remote_platforms` in `version.json` is such a feature removal.
- Always ask (never default): deletions outside the pre-approved list above, a wrong `gh` account (stop before any remote op), `README.md` edits outside a release-prep PR, backwards compatibility in `main`.

## Autonomy
- Keep going until the task is done and verified; don't stop to ask whether to continue or to report mid-task. Stop only when nothing can move without the maintainer: an always-ask item; a consent item from Hard rules or Git (destructive git, switching away from uncommitted work, moving work between branches); unrecognized changes that break your work; or a request whose readings lead to materially different work. Then ask with your recommended default and short options (A/B/C).
- Other judgment calls: pick the default, record it (PR body or local notes), continue.
- Code/data risk or irreversible remote state (tag, release, publish, delete) in a conflict: stop, name it, take the safer path. Two process rules in conflict: take the path that reaches a reviewed merge fastest and record the choice (hard rules, pre-PR review, Merge steps and Releases always win).
- Unrecognized changes in the tree: assume another agent and stay in your scope.
- Local notes: `docs/choices.md` (decisions) and `docs/breadcrumbs.md` (short current status) are optional, gitignored scratch files. Create them if missing, append dated entries, never commit them (`scripts/release/verify-blocked-files.sh` blocks them). In a fresh clone or CI they don't exist — use the PR body instead.

## Git
- Use `gh` for GitHub; for an issue/PR URL use `gh issue view --comments` / `gh pr view --comments --files`, not web search. Before remote ops run `gh auth status`; if the active account isn't `GH_USERNAME`, stop and ask before any remote op (`gh auth switch --user <user>`).
- Branches: conventional prefixes (`feat|fix|refactor|build|ci|chore|docs|style|perf|test`), one topic per branch, one `git worktree` per topic; keep local `main` clean. A new branch off clean `main`, or stacked on a branch you own for the task, is pre-authorized. Switching away from uncommitted work or moving work between branches needs consent. Dirty legacy worktrees are read-only source material. `git checkout` is fine for PR review or on request; a command the maintainer types is consent for that command.
- Push when asked, or on a branch whose PR you were asked to open or drive. Otherwise, don't push.
- The pre-push hook runs typecheck, tests, build, and `check-docs.sh` (3-5 min). `git push --no-verify` is fine only right after green targeted tests for the touched areas plus `npm run typecheck` (plus `bash scripts/check-docs.sh` for docs changes). On a push timeout, retry the same push.
- PR hygiene (the public history is a product too):
  - Before starting a topic, check `gh pr list --state open` for one on the same topic and join it.
  - One topic, one PR: review fixes, sweep fixes, and follow-ups land in that PR before it merges; merge only when you know of no remaining defect. A defect found after merge gets one fix PR that names the original ("follow-up to #N") and carries every known follow-up. Bundle small fixes of the same area into one PR.
  - Split only for a real reason: the size rule in Review, or a contract that must land before its consumer. Stacked PRs: before the base merges, `gh pr edit <child> --base main`; after it squash-merges, `git rebase --onto origin/main <base head SHA that merged> <child>` and `git push --force-with-lease origin <child>`.
  - Open a PR (never as draft) only after the pre-PR review, with the full body written: Codex starts reviewing on open. No close/reopen.
  - Title: conventional prefix, ≤72 characters, the user-visible effect. It becomes the commit on `main` only because merge step 4 sets it explicitly.
  - Context for reviewers goes in the PR body, not in extra comments.
- Small, reviewable diffs; no repo-wide search-and-replace scripts. Full diffs: `git --no-pager diff --color=never`. Avoid manual `git stash` (auto-stash in pull/rebase is fine).
- Shared machine: tests and scripts remove what they create, also on failure (Go `t.TempDir()`, TS `mkdtemp` + `afterEach` rm, shell `trap 'rm -rf "$tmp"' EXIT`); a run that grows `$TMPDIR` is a bug. Scratch goes in a per-task path (session scratchpad or `$TMPDIR/ha-nova-<branch>/`), never a shared fixed name. At most 3 agents build or test at once, with capped workers (`go test -p 2`, `npx vitest run --maxWorkers=2`); review-only subagents read and reason, they run no suites. Before verify/E2E: `df -h /`; under ~10 GB free, remove your own temp dirs and merged worktrees, say so, and continue with disk-free work. The test bed is one shared instance: if `testbed:up` reports it running, another agent owns it — never `testbed:down` it; skip the E2E and say so in the PR body.

## Review
- **High-risk:** security/auth/secrets, concurrency/races, irreversible data changes, release/installer/update machinery, new cross-component contracts. **Release-bound:** bumps `version.json`/`package.json`, or touches `scripts/release/`, the install/update flow, release workflows, or Go code (skills/docs are not release-bound just because skills ship). **Behavior-changing:** everything except docs-only, tests-only, generated/lockfile, or mechanical changes; skill `.md` edits that change agent behavior count as behavior-changing.
- Before the PR: self-review the full diff. Never show partial or unreviewed code to the user. Then run adversarial reviewers in fresh subagents over the final diff — behavior-changing: at least 1; high-risk: 2-3 in parallel with distinct lenses:
  - security / fail-open: every path that bypasses the new invariant, every error path that grants access or skips a check;
  - UX / agent guidance: does a non-technical user, or an agent following the skill, get the right next step;
  - sibling-path sweep: the same defect class in sibling files, callers, state sources, and orderings.
- Brief reviewers to report every finding that affects correctness or the stated requirements, then triage yourself (below); chasing style findings leads to over-engineering. Fix ALL surviving findings in one batch, then open the PR. Repeat only if the scope or the protected invariant changes; at most two passes of the same lens. Once the PR is open, further review is the Codex round's job. Edge cases you leave uncovered go into the PR body as known limits.
- Skill changes that alter agent behavior: verify with an end-to-end agent run on the test bed when available (start the agent from `eval "$(bash scripts/e2e/testbed/cli.sh --env)"`), not only unit tests — the skill text is the product. Word and route budgets in `tests/skills/` are ratchets: never raise one to fit your own text — trim, or move detail into a reference file. A raise needs a reason stated in the test and the PR body.
- Release-bound: also run a local Codex CLI review over the working-tree diff and fix its findings.
- Size: >~800 changed production lines (tests/docs/generated/lockfiles excluded) or 15 runtime files → split, or say in the PR why it stays one change. A substantial contract rewrite plus a new executable oracle → two stacked PRs from the start.
- Checkpoint (a report, not a gate) in your PR-open reply: `git status --short --branch`, `git diff --stat`, any `README.md` diff, tests run, remaining risks.
- Triage: **P1** real defect on a shipped path (security, data loss, release gate, user-visible break) → always fix. **P2** real but bounded (edge case with workaround, internal-only) → fix, unless the fix must add contract text; then prefer deleting the promise, or answer in-thread. **P3** / style / hypothetical → answer in-thread, no commit. For every new finding, sweep the same defect class across sibling paths and fix all instances in the same commit.

## Codex bot
- The bot reviews a PR once, automatically, when it opens. That review is the PR's only round, clean or not.
- Status: the bot's summary comment (`codex-review-state` JSON) has `code_review.status`; done = `completed`. A clean review is a "Didn't find any major issues" comment or a 👍 from the bot. Findings are its inline comments, anchored to the commit it reviewed — usually the PR-open head, not the current one. Read them with `--paginate`.
- Triage every bot thread, whatever its badge and whatever the current head: reply in-thread with the fix SHA or the reason (a P1 clears only with a fix, or by showing it is wrong for the current diff), then resolve. A thread is answered when its last comment is yours. Never resolve a thread you didn't answer. Agents comment as the maintainer's account: a thread the maintainer started is answered only by your reply to it, and only the maintainer resolves it.
- Manual trigger — once per PR, a comment whose entire body is `@codex` (multi-line bodies are ignored) — only when no summary appeared within 15 min of opening, or the bot replied "Something went wrong". Never trigger while a review is running. Still not `completed` 30 min later = timeout: say so in the PR and proceed on green checks.
- Batch mode (release-bound/high-risk, >2 new real findings): 2-3 adversarial subagents with distinct lenses over the full current diff, told what is already fixed; triage; fix all survivors in one commit; merge on green checks, no new trigger.
- After a trigger the bot may re-post fixed findings verbatim on a newer SHA: reply "fixed in <sha>", resolve, don't re-sweep.

## Merge steps (required for agent-authored PRs)
1. Review steps above done; `gh pr create`.
2. Codex (above): its review is `completed` (or timed out) and every bot thread is answered.
3. If the branch is behind `main`: `gh pr update-branch <nr>`. Then `gh pr checks <nr> --watch` until every required check is green on the current head. Why: `main` requires up-to-date branches, and `--admin` would skip that, landing an untested combination.
4. Resolve the answered threads (`gh api graphql` `reviewThreads`, then `resolveReviewThread` per id). Retarget any PR stacked on this branch (PR hygiene). Then `gh pr merge <nr> --squash --delete-branch --match-head-commit <checked head> --subject "<PR title> (#<nr>)" --body ""` — the repo default would take a single commit's title and paste every branch commit into `main`. `--admin` only after a merge without it failed with the self-approval requirement as the sole blocker: every required check (`ci-gate`, `analyze`, `dependency-review`, `app-image-verify`) `success` on that head, not behind, no unanswered thread.

## Releases
Runbook: `docs/releasing.md` (bump, preflight, release-worthiness, RC gate, rehearsal, Cloud checklist, release-notes style, post-publish checks).
- Tag, or call release-ready, only the remote merge commit of a PR that passed the merge steps, (its merged head is the one whose required checks were green). Dispatch its `e2e-disposable-ha.yml` run before `main` moves past it — `docs/releasing.md` dispatches with `--ref main`; once that run is green on the exact SHA, later commits on `main` don't block the tag, they just aren't in it. Never move, delete, or reuse a pushed tag. No local-only release shortcuts.
- Before any tag/publish: a dispatched green `e2e-disposable-ha.yml` on that commit; while Cloud is enabled, the Cloud checklist; "Release Candidate Gate" decides whether an `-rcN` rehearsal is required; either way the final tag's `release.yml` smoke and the post-publish public-install verification run (`docs/releasing.md` §4 step 5, §7). Never reintroduce auto-publish in `release-candidate.yml`. Never skip a manual RC lane for an OS silently: state the no-delta case in the release PR.
- The release-prep PR (bump + notes + release-bound `README.md` edits) starts the tag sequence; tag right after it merges. `README.md` describes the stable release only: no future claims, no version numbers (`version.json` is SSOT). Unreleased claims collect in `docs/work/next-release-body.md`.
- Upstream drift check before each release cycle and on each monthly HA release: screen the HA release post, dev-blog breaking changes, and HACS releases against every WS/REST surface pinned in `skills/**/*.md`. Append every run, hits and clean passes, to `docs/work/upstream-drift-log.md`; each skill-relevant hit becomes an issue or joins the running PR train.
- Dependabot PRs are reviewed and merged manually like any PR; toolchain bumps (`vitest`, `vite`, `typescript`, `tsx`, `rollup`, `rolldown`, `esbuild`) get a full local `npm run verify` first.

## User Notes
Use below list to store and recall user notes when asked to do so.

- Release guard: Claude marketplace sync changes ship with regression coverage for plain GitHub URL, structured GitHub source, and structured source with pinned `ref`; a pinned ref equal to the floating default source is a release blocker.
- PR hygiene: proactively check PR reviews incl. inline comments (`gh api repos/<o>/<r>/pulls/<nr>/comments`) without waiting for a reminder.
