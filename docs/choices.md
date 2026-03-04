# Design Choices

## 2026-03-04

### No legacy skill migration (v0.1.0 is first public release)
- No external users have ever installed older skill versions
- Installer simply overwrites managed skills; no backup/archive mechanism needed

### Uninstall script uses `rm -rf` instead of `trash`
- AGENTS.md guardrail says "use `trash` for deletes" — applies to agent-driven dev work
- `uninstall.sh` is an end-user distribution script; `trash` is not a standard macOS utility
- Compromise: only `rm -rf` known managed paths, `rmdir` for config dir (preserves custom files)

### Gemini CLI aliases to Codex install path
- Gemini CLI scans both `~/.gemini/skills/` and `~/.agents/skills/`
- Installing to both causes duplicate skill conflicts
- Solution: `gemini` is an alias for `codex` (both use `~/.agents/skills/`)

### `SUGGESTED_ENHANCEMENTS` kept in resolve-agent
- User-facing response format no longer has a "Suggested Enhancements" block
- Resolve-agent still generates `SUGGESTED_ENHANCEMENTS:` as internal data
- Write skill can incorporate suggestions into the preview narrative at its discretion
- Removing it would lose useful agent analysis; keeping it costs nothing

### Entity discovery uses `entity_registry/list_for_display` instead of `get_states`
- `get_states` returns ALL entities with full attributes (~10MB for 4800 entities)
- `entity_registry/list_for_display` returns compact format with abbreviated keys (~200KB)
- `get_states` kept in relay-api.md docs for single-entity state reads only
