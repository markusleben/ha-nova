# HA NOVA Core Blocks (MVP)

These reusable blocks are the canonical building units for write workflows.

- `B0_ENV`
  - Load NOVA environment once (`RELAY_BASE_URL`, `RELAY_AUTH_TOKEN`).
  - No proactive `doctor`/`ready` before first HA action.

- `B1_STATE_SNAPSHOT`
  - Fetch `get_states` exactly once via relay `/ws`.
  - Reuse snapshot in all downstream filters.

- `B2_ENTITY_RESOLVE`
  - Resolve target entities from shared snapshot.
  - Do not guess unknown entity IDs.

- `B3_ID_RESOLVE`
  - Resolve target config/runtime IDs and existence branch (`200`/`404`).

- `B4_BP_GATE`
  - For automation `create`/`update`: require valid best-practice snapshot.

- `B5_BUILD_AUTOMATION`
  - Build deterministic automation payload (`alias`, `trigger`, `condition`, `action`, `mode`).

- `B6_BUILD_SCRIPT`
  - Build deterministic script payload (`alias`, `sequence`, `mode`, optional fields).

- `B7_RENDER_DOMAIN_PREVIEW`
  - Render domain-first preview output.

- `B8_CONFIRM_TOKEN`
  - Require strict tokenized confirm (`confirm:<token>`).

- `B9_APPLY_WRITE`
  - Apply write via relay `/core` with explicit method/path envelope.

- `B10_VERIFY_WRITE`
  - `create`/`update`: require postcondition read-back.
  - `delete`: success status (`200`/`204`) sufficient by default; optional verify-absent read-back.

- `B11_DIAG_ONLY_ON_FAILURE`
  - Show technical diagnostics only on failure or explicit request.

## Constraints

- Normal path must avoid temporary helper scripts.
- For complex multiline shell commands, use `bash -lc`.
- Avoid shell-specific builtins in normal paths (e.g. `mapfile`).
