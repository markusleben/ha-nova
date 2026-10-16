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
     `skills/review/checks.md`) → that file.
   - `task`: returns the dispatch index (`skills/ha-nova/index.md`) — the same
     table skill-mode agents route with — and asks for a second call with
     `name`. No keyword routing in Go: it would drift from the router's
     table and misroute (e.g. "why didn't my automation trigger" shares more
     words with service-call's "trigger automations" than with diagnose).
   - Read-only, idempotent, no Home Assistant access, no Relay call.
   - Only files in the allowlist built from the bundle's `skills/` tree are
     ever opened: `skills/<dir>/SKILL.md` and every other regular `*.md` below
     `skills/` (the `resources/list` set). Symlinks are not followed. Input is
     matched against the list as a string; it is never joined into a path.
2. **Response.** Plain Markdown text: an MCP header, then the file. The header
   maps the CLI to the tools once (`find/get/history/trace` → `ha_find/…`,
   `plan/call/apply` → `ha_plan/ha_call/ha_apply` with flags as arguments,
   read-only `relay core GET` / `relay ws` reads → `ha_read`; skip
   `check-update` and `relay health` — tool results carry `notices[]`;
   anything else needs a client with a shell) and says how to fetch cited
   files. Skill files stay unchanged (word budgets untouched). The prompts
   reuse the same header.
3. **Bound.** A skill response inlines `skills/ha-nova/output-rules.md` when
   the skill cites it and the response stays ≤ 40 KiB; other cited files are
   fetched with a follow-up `ha_skill` call by the path the skill names. A file is never truncated.
4. **Errors.** Unknown name → `isError`, code `UNKNOWN_SKILL`, `next_step`
   lists the skill ids and the `task` fallback. Neither argument →
   `INVALID_ARGUMENTS`.
5. **Instructions.** One added sentence: before building, changing, reviewing
   or diagnosing anything, call `ha_skill` for the matching skill and follow
   it; device control and lookups need only the tools. The tool description
   lists the skill ids.

## Out of scope

- No new HA-facing tool; writes beyond device control and automation/script
  plans still need a shell client.
- No README or release-notes claim: MCP stays experimental and unadvertised.

## Verification

- Go tests: schema, id/path normalization, unknown name, traversal and
  symlink refusal, size bound, instructions and header text.
- Internal measurement (gitignored `docs/internal/`): the Codex MCP bench
  column on the local test bed against skill-mode runs.
