# MCP: skills over one read-only tool (`ha_skill`) — Spec (2026-10-16)

Status: active

## Problem

- `ha-nova mcp` (experimental, unadvertised) exposes the skills as MCP prompts
  and the shared references as resources. Prompts are user-invoked and models
  do not read resources on their own, so an MCP agent builds, reviews, and
  diagnoses with the typed tools alone — without the workflow a skill-mode
  agent follows.
- Skills name CLI commands (`ha-nova call …`, `ha-nova relay core GET …`); an
  MCP agent reading them needs the tool equivalent, without rewriting every
  skill.

## Behavior

1. **One tool, `ha_skill`.** Arguments: `name` or `task` (one of them).
   - `name`: a skill id (`write`, `ha-nova:write`, `ha-nova-write`,
     `skills/write/SKILL.md`) → that skill's `SKILL.md`; or a file a skill
     cites (`skills/ha-nova/write-safety.md`, `../ha-nova/session-bootstrap.md`,
     `skills/review/checks.md`, or a bare name such as `raw-routes.md` when
     exactly one listed file carries it) → that file.
   - `task`: returns the dispatch index (`skills/ha-nova/index.md`) — the same
     table skill-mode agents route with — and asks for a second call with
     `name`. No keyword routing in Go: it would drift from the router's
     table and misroute (e.g. "why didn't my automation trigger" shares more
     words with service-call's "trigger automations" than with diagnose).
   - Read-only, idempotent, no Home Assistant access, no Relay call.
   - Only files in the allowlist built from the bundle's `skills/` tree are
     ever opened: `skills/<dir>/SKILL.md` and every other regular `*.md` below
     `skills/` (the `resources/list` set), including the implicit reads
     (`output-rules.md`, `index.md`). Symlinks are not followed. Input is
     matched against the list as a string; it is never joined into a path.
2. **Response.** Plain Markdown text. A skill response starts with an MCP
   header that maps the CLI once: `find/get/history/trace` → tools;
   `plan/call/apply` → `ha_plan/ha_call/ha_apply` with flags as arguments
   (`--confirm`, `--file`, `--data-file`/`--set`, `revert --target`); read-only
   `relay core GET` / `relay ws` reads → `ha_read` within its allowlist
   (filters applied by the model); exit codes → `status`; skip the session
   bootstrap (results carry `notices[]`); client cache files count as
   missing; "context skill" → `ha_skill name=ha-nova`; anything else needs a
   client with a shell — say so and name the evidence not checked. A cited
   file comes with a one-line pointer to that header instead of repeating it.
   Skill files stay unchanged (word budgets untouched). The prompts reuse the
   header; this replaces the prompt preamble of
   `2026-10-14-typed-verbs-spec.md` §5.
3. **Bound.** A skill response inlines `skills/ha-nova/output-rules.md` when
   the skill cites it and the response stays ≤ 28 KiB (under a ~10k-token
   client output cap); other cited files are
   fetched with a follow-up `ha_skill` call by the path the skill names. A file is never truncated.
4. **Errors.** Unknown name → `isError`, code `UNKNOWN_SKILL`, `next_step`
   lists the skill ids and the `task` fallback. Neither argument →
   `INVALID_ARGUMENTS`.
5. **Instructions.** One added sentence: before building, changing, reviewing
   or diagnosing anything, call `ha_skill` for the matching skill (once per
   skill per session) and follow it; device control and lookups need only the
   tools. The tool description lists the skill ids.
6. **`ha_read` refusals.** A WS type outside the read allowlist is refused as
   `INVALID_ARGUMENTS` naming the allowed types and the shell-client path,
   not as a Relay error that reads like a pairing problem.

## Known limits

- The two largest skills (`ha-nova` ≈ 41 KB, `review` ≈ 36 KB) exceed a
  ~10k-token client output cap on their own; such clients cut them.
- Skill steps that need logs, template probes, diagnostics, or writes beyond
  device control and automation/script plans stay shell-only.
- The bundle directory is trusted: a file swapped between listing and reading
  (local write access) is not detected; a symlinked `skills/` root serves
  skills but no references.

## Out of scope

- No new HA-facing tool; writes beyond device control and automation/script
  plans still need a shell client.
- No README or release-notes claim: MCP stays experimental and unadvertised.

## Verification

- Go tests: schema, id/path normalization, unknown name, traversal and
  symlink refusal, size bound, instructions and header text.
- Internal measurement (gitignored `docs/internal/`): the Codex MCP bench
  column on the local test bed against skill-mode runs.
