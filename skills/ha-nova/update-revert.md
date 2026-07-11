# HA NOVA Update-Revert Execution

Load this file only when the user actually asks to `revert` an update.
Snapshot capture and the revert offer live in `skills/ha-nova/write-safety.md`
→ Update-Revert; this file owns the execution.

## Execute `revert`

1. Run `ha-nova snapshot show` — the **only** source of `before_config`. Never
   reconstruct the previous config from memory or context. The bare command
   returns the newest update; for an earlier target in a multi-target change,
   select it: `ha-nova snapshot show --target <target_id>` (add
   `--domain <domain>` if the same id exists in both domains). `ha-nova
   snapshot show --list` lists what is still revertible (newest first).
2. Re-read the current live config of `target_id` (same read-back path as
   Phase 4) into a file.
3. Drift check — MUST carry the same `--target`/`--domain` selection as
   step 1 (a bare `verify` compares against the NEWEST snapshot and would
   drift-check an earlier target against the wrong `expected_after`):

   ```text
   ha-nova snapshot verify --target <target_id> --against <live-file>
   ```

   Only when reverting the newest update may the `--target` flag be omitted.

   - `match` (exit 0) → the config is still exactly as written; safe to restore.
   - `drift` (exit 2) → it changed since the write (e.g. an external UI edit).
     Do NOT overwrite blindly. Show what differs and ask the user how to proceed.
4. On `match`, restore the `before_config` from step 1 through the skill's own
   write path, then verify it like any other write:
   - `ha-nova:write` (automation/script): run the apply phase with
     `OPERATION=update`, `TARGET_ID=<target_id>`, `PAYLOAD=before_config`.
   - `ha-nova:helper` storage family: rebuild a schema-valid `{type}/update`
     payload from `before_config` — set `{type}_id` from its stored `id` and
     include only writable update fields (drop read-only `id`/`entity_id`; see
     `skills/ha-nova/helper-schemas.md` → Common Rules). Sending the raw
     `before_config` list item fails: it lacks the required `{type}_id`. Re-issue
     that `{type}/update` WS call, then re-read the list item to confirm.
   - `ha-nova:helper` config-entry family: no auto-revert (options-flow writes
     are multi-step) — point to HA Backups.

Use `ha-nova snapshot show` to read the stored record (including
`before_config`) back when you need it.

## Honesty

- The store holds the last 5 updated targets; older entries are gone, and a
  re-update of a target replaces that target's snapshot (one step back per
  target, never two).
- `revert` restores the captured config through the normal write path; it does
  not replay HA's exact byte-for-byte formatting. For a guaranteed point-in-time
  restore, Home Assistant Backups is the source of truth.
