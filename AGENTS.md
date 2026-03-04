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

## Guardrails
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
- **PR Merge:** Do NOT use auto-merge. The Codex review bot needs ~3 min to post findings after CI passes. Workflow:
  1. `gh pr create ...` — create the PR.
  2. Wait for CI checks to pass.
  3. Check for bot review comments: `gh api repos/<owner>/<repo>/pulls/<nr>/comments`.
  4. Resolve any findings (fix or acknowledge).
  5. Then merge: `gh pr merge --squash --delete-branch`.

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
- Legacy project: `ha-mcp-addon` (88K LOC MCP server, separate repo, being replaced by HA NOVA).
- Reference docs in `docs/reference/` are mandatory reading before working on Relay or Skills.
- For graphics/diagrams, labels must stay consistent across all views (top view, side view, etc.).
- Relay stays dumb, Skills stay smart. No business logic in the server.
- Preferred terminology (2026+): use "App" instead of "Add-on", except where technical API paths force legacy terms (for example `/addons/*`).
- Priority: deliver a working MVP first, but keep the architecture modular from day one for later extension.
- Skills remain pure `*.md` files; no hidden business logic outside this model.
- Relay implementation must remain lean, clean, and efficient (KISS + DRY, clear responsibilities).
- Before public release, do not add backward-compatibility paths or fallback behavior. Implement the clean target model.
- UX is a primary success metric: onboarding must feel simple, guided, and low-friction for non-technical users.
- PR hygiene (user requirement): proactively check GitHub PR reviews (including inline review comments via `gh api repos/<owner>/<repo>/pulls/<nr>/comments`) without waiting for a reminder.
