# AGENTS.md

Version: 0.25 (2026-02-01)

Start: say hi + 1 motivating line.
Work style: Be radically precise. No fluff. Pure information only (drop grammar; min tokens).

## Project
- GitHub User: see `.env` <GH_USER>

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
- Output localization (translating headings/labels for the user) happens at runtime per the context skill — never baked into skill source files.

## Code Quality [DON'T SKIP – IMPORTANT]
- All generated code must be self-reviewed before being presented.
- Continue reviewing and fixing until no further issues are found.
- Do not show partial or unreviewed code to the user.

## Guardrails
- ALWAYS review the written code, don't present it to the user without! Review it so long, until the review won't find any more issues
- Use `trash` for deletes.
- Use `mv` / `cp` to move and copy files.
- Bugs: add regression test when it fits.
- Keep files <~400 LOC; split/refactor as needed.
- Simplicity first: handle only important cases; no enterprise over-engineering.
- New functionality: small OR absolutely necessary.
- NEVER delete files, folders, or other data unless explicitly approved or part of a plan.
- Before writing code, strictly follow the research rules below.

## Research
- Always create a spec, even if minimal
- Prefer skills if available over research
- Prefer researched knowledge over existing knowledge when skills are unavailable
- Research: Exa to websearch early, and Ref to seek specific documention or web fetch.
- Best results: Quote exact errors; prefer 2025-2026 sources.

## Git
- Always use `gh` to communicate with GitHub.
- **Multi-Account:** Before remote ops (push, repo create, PR), run `gh auth status`. If active account ≠ `Project.GitHub Account`, ask user before proceeding.
- Use `gh auth switch --user <GitHub User>` to switch between GitHub accounts.
- GitHub CLI for PRs/CI/releases. Given issue/PR URL (or `/pull/5`): use `gh`, not web search.
- Examples: `gh issue view <url> --comments -R owner/repo`, `gh pr view <url> --comments --files -R owner/repo`.
- Conventional branches (`feat|fix|refactor|build|ci|chore|docs|style|perf|test`).
- Safe by default: `git status/diff/log`. Push only when user asks.
- `git checkout` ok for PR review / explicit request.
- Branch changes require user consent.
- Destructive ops forbidden unless explicit (`reset --hard`, `clean`, `restore`, `rm`, …).
- No repo-wide S/R scripts; keep edits small/reviewable.
- Avoid manual `git stash`; if Git auto-stashes during pull/rebase, that’s fine (hint, not hard guardrail).
- If user types a command (“pull and push”), that’s consent for that command.
- Big review: `git --no-pager diff --color=never`.
- **Review clearance is commit-specific:** any new relevant delta after the last bot-reviewed commit invalidates prior review clearance.
- **Relevant delta means:** any code, tests, docs-that-change-behavior, scripts, workflow files, release metadata, installer/update flow, or release notes change that alters the commit to merge or tag.
- **Push is not review:** a new push never inherits the previous clean review state.
- **A clean bot result is SHA-specific:** it applies only to the exact commit SHA it reviewed; any later SHA is unreviewed until the full cycle completes again.
- **Codex advisory rule:** `codex-review-gate` is advisory on `main`; do not treat it as a required branch-protection gate for routine PRs.
- **No local-only release shortcuts:** if a follow-up fix matters enough to keep, it must go through GitHub review before merge/tag/release.
- **Release/tag/publish gate:** never create/move a release tag, start RC/final publish, or call a commit release-ready unless the exact remote commit state intended for tag/release is represented by the latest fully reviewed PR state with no unreviewed deltas beyond it.
- **Release-bound review hardening:** use the fast path during iteration: after each relevant fix, run only targeted local verification, push immediately, and trigger `@codex review` immediately; run the two independent subagent review passes in parallel while CI/Codex are already running. For final merge/tag clearance, Codex is still only a final net on top of the exact same SHA that already has two current clean subagent passes.
- **PR Merge / Release Commit Gate — MANDATORY CHECKLIST (do NOT skip any step):**
  The `codex-review-gate` workflow waits ~9 min for the Codex review bot. Bot signals: `eyes` reaction = review in progress, `👍` reaction = no findings, review comments = findings.
  - [ ] 1. `gh pr create ...`
  - [ ] 2. For each relevant fix iteration: run targeted local verification only, push immediately, then immediately trigger Codex re-review: `gh pr comment <nr> --body "@codex review"`.
  - [ ] 3. While CI/Codex are already running on that pushed SHA, run at least two independent subagent review passes on the exact current PR delta with distinct focuses; do this in parallel, not serially after the bot.
  - [ ] 4. `gh pr checks <nr> --watch` — wait for ALL required checks; for release-bound/high-risk deltas also wait for `codex-review-gate`
  - [ ] 5. Check bot signal across all channels: `gh api repos/<o>/<r>/issues/<nr>/reactions` (👍 = clean), `gh api repos/<o>/<r>/pulls/<nr>/reviews` (PR-level review findings), `gh api repos/<o>/<r>/pulls/<nr>/comments` (inline findings), and issue/discussion comments on the PR.
  - [ ] 6. If findings OR any new relevant delta is introduced afterward → fix, run targeted verification, push immediately, then **trigger re-review**: `gh pr comment <nr> --body "@codex review"` — pushes alone do NOT trigger re-review. Then go back to step 3 for the new SHA.
  - [ ] 7. Resolve ALL review threads before merge (branch protection blocks unresolved):
         `gh api graphql -f query='{ repository(owner:"<o>",name:"<r>") { pullRequest(number:<nr>) { reviewThreads(first:20) { nodes { id isResolved } } } } }'`
         Then for each unresolved: `gh api graphql -f query='mutation { resolveReviewThread(input:{threadId:"<id>"}) { thread { isResolved } } }'`
  - [ ] 8. For release-bound/high-risk deltas, only proceed after an actual Codex bot result for the current latest commit SHA; timeout alone is NOT enough.
  - [ ] 9. For release-bound/high-risk deltas, confirm the PR head SHA is still the same SHA that received the latest clean/current bot result and the latest clean subagent passes. If SHA changed, go back to step 3.
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
- Conflicts: stop. call out; pick safer path.
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
      - If yes: choose a default, document it in , and continue.
- When you choose opinionated defaults, document them in `/docs/choices.md` as you work.
- Leave breadcrumb notes in thread and `/docs/breadcrumbs.md`.
- When writing to `/docs/choices.md` or `/docs/breadcrumbs.md` categorize by date (tail)
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
- For graphics/diagrams, labels must stay consistent across all views (top view, side view, etc.).
- Relay stays dumb, Skills stay smart. No business logic in the server.
- Preferred terminology (2026+): use "App" instead of "Add-on", except where technical API paths force legacy terms (for example `/addons/*`).
- Priority: deliver a working MVP first, but keep the architecture modular from day one for later extension.
- Skills remain pure `*.md` files; no hidden business logic outside this model.
- Relay implementation must remain lean, clean, and efficient (KISS + DRY, clear responsibilities).
- **UX is king** — the guiding mantra for this project. Always prefer fewer manual steps for the user. When choosing between technical purity and user convenience, convenience wins. This applies from onboarding through skill performance to uninstall. Target audience is not necessarily terminal-savvy.
- PR hygiene (user requirement): proactively check GitHub PR reviews (including inline review comments via `gh api repos/<owner>/<repo>/pulls/<nr>/comments`) without waiting for a reminder.
- Release guard: Claude marketplace sync changes must ship with regression coverage for plain GitHub URL, structured GitHub source, and structured GitHub source with pinned `ref`; pinned refs are release blockers if they compare equal to the floating default source.
- Release notes preference: keep them short and user-centric. Prioritize `New Features`, optional `What To Watch` only for real behavior/breaking/action-needed changes, and selective `Bug Fixes` only for important user-facing fixes. Do not dump every minor fix into release notes.
- Release preflight requirement: before every release/RC/tag/publish flow, proactively audit open PRs with special focus on Dependabot and workflow/release-related PRs. Classify them as `blocker now` vs `separate later`. Never pull in a red or unreviewed workflow/release PR right before publish just because it is open.
- Release-worthiness rule (user requirement): not every merged change deserves an immediate new version or release. Default to batching docs/tests/process-only/internal maintenance into the next real user-facing release unless the change fixes the live release path, published artifacts, installer/update flow, or another issue users can actually hit in the shipped product.
- Dependabot fast-lane rule (user requirement): automate only the safe lane. Dev-only npm minor/patch manifest updates may auto-approve/auto-merge after required checks pass; workflow/release/installer/runtime/security changes stay manual.
- Toolchain-risk dev dependency rule (user requirement): keep `vitest`, `vite`, `typescript`, `tsx`, `rollup`, `rolldown`, and `esbuild` out of the Dependabot fast lane even when they arrive as dev-only npm minor/patch manifest bumps; those get manual review because they can drag wider toolchain/runtime churn.
- Codex advisory rule (user requirement): on `main`, Codex is a review signal and escalation layer, not a required branch-protection check; for release-bound/high-risk deltas, still wait for a real Codex result on the final SHA.
- Main-branch protection drift check (user requirement): when repo policy changes, verify the live `main` branch protection with `bash scripts/release/verify-github-main-protection.sh` so GitHub settings do not silently drift away from the documented contract.
- Codex review hygiene (user requirement): for release-bound PRs, wait for the real Codex bot response; do not treat workflow timeout as a clean review.
- Review invalidation hygiene (user requirement): if any relevant delta lands after the last reviewed commit, release readiness resets to zero until that exact new commit state completes a fresh PR + `@codex review` + real bot response cycle.
- Subagent review hygiene (user requirement): for release-bound or high-risk changes, use at least two independent subagent review passes with distinct focuses on the final merge/tag SHA so Codex review remains a final net for release clearance, even if Codex was also triggered earlier during fix iteration.
- Review speed rule (user requirement): do not serialize Codex and subagent review unnecessarily. After each relevant push, trigger `@codex review` immediately; run subagent reviews and CI in parallel; only the final merge/tag decision must wait for all clean signals on the same SHA.
