---
name: write
description: Use when creating, updating, or deleting Home Assistant automations or scripts through HA NOVA Relay. Resolves entities and reviews internally.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Write


## Scope

Mutations only:
- domains: `automation`, `script`
- operations: `create`, `update`, `delete`
- and one runtime exception: the paired immediate service call of a DURATION
  request, plus `automation.turn_off` on an expiry automation this flow just
  created. Both belong to the write because splitting them loses the pairing
  (`skills/ha-nova/one-shot-automations.md`); nothing else runtime is in scope.

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify Relay:

```text
ha-nova relay health
```

If this fails, run onboarding: `ha-nova setup`.

## Relay Contract

File-based relay requests only: `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>` for config writes, `ha-nova relay ws --data-file <payload-file>` for reads/reloads, `--jq-file` / `--out` for filters and large reads.
- `ha-nova relay core --method GET --path /api/states/<entity_id> --out <result-file>` — the duration read-backs: the captured restore value before applying, and the target after the immediate action.
- `ha-nova relay core --method POST --path /api/services/<domain>/<service> --body-file <payload-file>` — ONLY for the two runtime calls named in Scope: a duration request's immediate action, and `automation.turn_off` on an expiry automation this flow created.

## Flow

Multi-target logical changes: present the plan first per `skills/ha-nova/write-safety.md` → Multi-Target Changes. Non-destructive worksets (max 10 operations) may confirm as one grouped change set per `skills/ha-nova/grouped-change-set.md` — Phase 2 previews stay, their per-preview options and confirmation collapse into the group's single final action block.

Input-device remaps (buttons, remotes, switches): run the capability preflight per `skills/ha-nova/input-capability-preflight.md` before drafting; the write stays blocked while the chosen gesture is only assumed or its evidence conflicts. Repurposing or cleaning up an input additionally runs `skills/ha-nova/consumer-discovery-preflight.md`; incomplete coverage is disclosed, never claimed as "unused".

A one-shot intent ("just today", "one time only", "tell me when the laundry
finishes") takes the self-disabling pattern in
`skills/ha-nova/one-shot-automations.md`,
and skips unsolicited improvement offers — the Phase 2 Suggestion Block AND
the Phase 5 test offer, because a real-run test of an expiry automation would
undo the duration the user just asked for. The user asked for something
temporary, not something to grow.

A recovery, watchdog, self-healing, or retry intent reads
`skills/ha-nova/recovery-workflows.md` before drafting; the preview names the
attempt bound and the persistent-fault path.

Every draft follows `skills/ha-nova/smallest-solution.md`: the complete
explicit outcome in the simplest safe design, nothing for hypothetical
future needs, native primitives when equally safe and clearer.

### Phase 1: Resolve (Agent)

1. Read `skills/ha-nova/agents/resolve-agent.md`.
2. Fill placeholders.
3. Dispatch. Extract `target_id`, `current_config`, `bp_status`, `suggested_enhancements`.
   - update/delete: resolve known `entity_id -> unique_id` with `config/entity_registry/get`; use `config/entity_registry/list_for_display` only for search/disambiguation
4. On ambiguity/no-match: ask one blocking question for the exact entity_id. No second ambiguity question. Correct invalid premises.
5. `create` IDs: automations=Unix timestamp, scripts=slug.

### Phase 2: Preview + Confirm (Main Thread)

1. Build config. For update: full-replacement merge (base=current, overlay=user changes).
   - Do not rewrite unrelated structure, aliases, or formatting for a narrow change.
   - Treat notification copy as user-authored content: preserve notification titles, messages, templates, metadata. Rename/timing changes must not restyle, relocalize, or restructure existing text; requested wording changes change only the requested copy.
2. BP gate (`skills/ha-nova/write-safety.md`): fresh/stale+simple->continue, stale+complex->block.
3. Suggestions + Pre-Write Checks (skip for `delete`):
   - **3a) Suggestions**: Render `suggested_enhancements` as the Suggestion Block (output-rules.md; max 2, smallest intervention first, numbered/menu — `skills/ha-nova/smallest-solution.md`). User accepts numbers or "skip" → merge accepted into config BEFORE preview. Skip when `SUGGESTED_ENHANCEMENTS: none`.
   - **3b) Static Checks**: Use `skills/review/checks.md` → Application (family matrix + evidence boundaries). Run S/R/P/M checks analytically on the draft YAML — no relay calls (scripts: F-01..F-08; helper refs: H-01..H-08. Defer H-09/H-10 to Phase 4).
     One pre-write verdict line before apply:
     - clean draft → localized equivalent of "Pre-write check: no issues worth flagging before save."
     - any flagged draft → localized equivalent of "Pre-write check: this draft may not behave as intended."
     🔴 findings → inline warning + fix. 🟠🟡 findings → advisory below preview. Keep wording code-free.
     Advisories do not block the write and do not require extra confirmation:
     - R-18: REST/UI write can break dependent variables in that block. Tell user to inspect traces after the next real run; do not auto-trigger/read them.
     - R-19: final else branch is only reached when the earlier entity-state branches are false. Move `trigger.id` into `elif` or refactor to `choose` + `condition: trigger`.
     - If R-23 matches: boolean-like template values are compared to string `"True"`/`"False"`; use the boolean directly or direct negation.
     - If R-24 matches: `available_energy` may be current charge, not capacity; ask user to verify maximum/nominal source. Never auto-rewrite integration-specific entity IDs.
     Track findings by check type for dedup in Phase 4, except R-18.
   - **3c) Pre-Write Impact (update only)**: run `search/related` (`item_type:"entity"`) on the top entities the automation/script acts on or reads — taken from its triggers/conditions/actions, NEVER on the item's own `entity_id` (that direction answers who references this item — the delete-check direction — not what else consumes its target entities). Read the response ONLY through the canonical filter — copy `skills/ha-nova/search-related-consumers.jq`; if unavailable (flat-copy installs), recreate exactly:
     ```jq
     if .ok and (.data | type == "object") then ((.data.automation // []) + (.data.script // []) + (.data.scene // [])) else error("search/related failed: \(.error.message // "unexpected response shape")") end
     ```
     Show affected automations/scripts/scenes as advisory (read max 3 related configs). Fail closed: an empty filter result means "no linked consumers found for the scanned entities" — name the scanned entities; a filter error or a scan that did not run renders as "consumer check inconclusive". NEVER turn a failed, skipped, or wrong-path scan into a no-consumer claim. Skip `create`/`delete`.
   - **3d) Threshold Calibration (update; create for stored threshold setters)**: when the update changes a `numeric_state` `above`/`below`, its `for:` duration, a `wait_for_trigger`/`wait_template` timeout, or a numeric predicate inside a wait or direct template trigger/condition, on a physical-process sensor — or swaps the compared `entity_id`/`attribute`/`value_template`, or adds/changes an action setting a threshold-backing `input_number` — run the history-based preflight per `skills/ha-nova/threshold-calibration.md` and carry its findings (observed ranges, longest ambiguous phase, data gaps, both failure directions) into the preview — bounded reads of already-existing traces are the preflight's explicit exception to the no-auto-trace rule. On create, the preflight runs only when the new item stores a setter action (`input_number.set_value`/`increment`/`decrement`, or a scene member assigning such a helper) on a helper that existing items consume as a physical-process boundary — resolve consumers via one bounded `search/related` on that helper; otherwise create keeps its 3c skip. Unrelated numeric edits skip it.
4. Preview (see `skills/ha-nova/write-safety.md` for the fixed shape):
   - update: **run** `ha-nova diff` (prefer `--out <diff-file>`), print the diff output **verbatim** in the Changes slot — never write it yourself; state the behavioral effect in the summary sentence (write-safety → Behavior narrative). create: summary. Create/update previews show `apply`, `show yaml`, and `cancel` options.
   - Delete preview MUST include the consumer-check result before confirmation: the affected consumers, an explicit no-consumer result (empty result through the canonical filter), or an explicit "consumer check inconclusive" line when the scan failed or did not run. Never present a failed or skipped scan as a no-consumer result. The delete consumer check scans the deleted item's OWN `entity_id` — who references it; the 3c NEVER-own-id rule applies only to the update-impact direction.
   - Delete previews do not show `apply`, `show yaml`, `cancel`, or any menu; ask only for the exact `confirm:<token>` or cancellation.
   - Batch delete of a reviewed same-family workset (automations OR scripts, never mixed) follows `skills/ha-nova/batch-safety.md`; per-item consumer checks and absence verification stay.
5. Confirmation: create/update=natural, delete=typed confirmation code `confirm:<token>`. A duration request takes the typed code when EITHER half grants physical access or is physically irreversible — the tier follows the runtime action, not the config write, and the counter-action is a runtime action too: `lock.lock` for an hour schedules an unattended `lock.unlock`. Active Preview Confirmation is required; delete is the typed confirmation code, never a menu. Pre-preview consent is draft-only; payload/diff changes need preview.

### Phase 3: Apply + Verify (Agent)

1. Updates: if the conversation paused since the preview or the target may have changed externally, run the drift check first (`write-safety.md` → Drift check before apply).
2. Deletes: capture the auto config snapshot of the current read-back first — `skills/ha-nova/config-snapshots.md` (on capture failure follow its capture-failure stop; mention restore in the result).
3. Read `skills/ha-nova/agents/apply-agent.md`.
4. Fill with confirmed payload.
5. Dispatch. Expect: success, write_status, verification.
6. **Duration requests carry an immediate action too** — "sprinkler for 30
   minutes" is the expiry automation PLUS the turn-on under one preview
   (`skills/ha-nova/automation-patterns.md`). Apply and verify the automation,
   then re-check deadline margin and the captured restore value. If either is
   stale, disable the armed expiry immediately and remove it through the delete
   flow before re-previewing. Only then issue the immediate service call and
   VERIFY it took effect by reading the
   target back — Home Assistant accepting the call is not the device having
   changed, and reporting "running for 30 minutes" over a valve that never
   opened leaves an expiry armed against nothing; on a failed
   action, disable the automation at once and clean it up through the delete
   flow. Never hand the immediate half back to the user as a separate step.
7. Report result. No raw curl/JSON in output.
   - Do not report destructive success until verification proves the target is gone.

Fallback: If agent dispatch unavailable, execute inline.

### Phase 4: Post-Write Review (MANDATORY)

Do NOT invoke `ha-nova:review` separately.

1. Re-read by `target_id` (do NOT re-resolve by slug):
   - automation: `ha-nova relay core --method GET --path /api/config/automation/config/<target_id> --jq-file <filter-file> --out <result-file>`
   - script: `/api/config/script/config/<target_id>`
   - `<filter-file>`: copy `skills/ha-nova/config-body-filter.jq`; if unavailable (flat-copy installs), recreate exactly:
     ```jq
     if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end
     ```
   - for create/update: reload again only if Phase 3 reported `reloaded: false` or ran inline without reload; resolve actual `entity_id` by matching `unique_id == <target_id>`, then read `/api/states/{entity_id}` to confirm runtime presence
   - if the actual `entity_id` differs, report it and point to `skills/ha-nova/safe-refactoring.md`; do not silently assume the requested slug won
2. S/R/P/M/F checks (narrowed):
   - Compare read-back vs draft as normalized objects, not raw JSON strings; key order is irrelevant. Ignore metadata (`id`,`unique_id`,`created_at`,`modified_at`,`editor`,`enabled`).
   - HA may normalize keys during write (`trigger`→`triggers`, `action`→`actions`, `condition`→`conditions`). Account for plural aliasing when comparing — these are not real diffs.
   - Core fields differ beyond aliasing → full checks per `skills/review/checks.md` → Application. If they match, skip covered checks but re-run storage-sensitive R-18 subset.
   - **Dedup**: findings from Phase 2 Step 3b that the user saw MUST NOT repeat. Track by check type.
    - Exception: if R-18 still matches on persisted read-back config, report it again as a persisted runtime risk.
    - `R-19` follows normal dedup; R-23/R-24 do too. If already shown pre-write, do not repeat unless it becomes a new category.
    - If persisted R-18 remains, inspect traces after the next real run. Do not auto-trigger or auto-read traces outside an accepted Phase 5 test plan.
    - If actions reference helpers: always run H-01..H-11.
3. Collision scan: `{"type":"search/related","item_type":"entity","item_id":"<entity_id>"}` via `ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>`, where `<filter-file>` is the same canonical `search-related-consumers.jq` (and the same fail-closed rule) as Phase 2 step 3c; read max 3 related configs.
4. Post-Write Review output (localized; see `skills/ha-nova/output-rules.md`) — report only what has substance; scans still run, only empty output is suppressed:
   - **Findings**: real issues only. **Collision check**: only when related items exist (list them + the verdict); a failed scan is NOT empty output — report it as "consumer check inconclusive". **Advisory**: only when non-empty. Omit any section with nothing to report — never print an empty "none" bucket.
   - If nothing is worth reporting, collapse to one scope-honest confirmation line (write-safety → Verification Honesty).
   - Never emit `Questions to consider`, `Suggestions`, or `Instant help` post-write; never repeat an item across **Findings** and **Advisory**.
5. Update-Revert: updates only → **run `ha-nova snapshot save`** and offer `revert` (see `skills/ha-nova/write-safety.md`); reflect the save receipt in the result — a `replaced` receipt means only this newest update stays revertible, and evicted targets are no longer revertible. Creates → cleanup via normal HA NOVA delete flow with preview, `confirm:<token>`, and absence verification. Deletes → name the captured config snapshot as the restore path (`skills/ha-nova/config-snapshots.md`); when no snapshot was captured (store missing/full), Home Assistant Backups.

### Phase 5: Test Offer (create/update only)

After the Post-Write Review output, build a Test Plan Card per `skills/ha-nova/test-run.md`: classify trigger type and action risk, pick ONE recommended option (real run included where acceptable — with the devices it will switch named), show at most 3 options plus `skip`. Never execute anything unconsented; the user's option choice is the confirmation bound to that exact card (single confirmation — no second prompt), except for high-consequence members, where the card confirmation never replaces the typed confirmation code. Runs follow the service-call runtime rules (`mqtt` triggers via `ha-nova:mqtt`; logic check via `POST /api/template`), then the automatic post-run follow-up in test-run.md (trace, state verify, restore). After one skip, de-escalate to a single line for later writes. Skip this phase for deletes.

## Output Format

Apply `skills/ha-nova/output-rules.md`.

Previews, delete prompts, post-write results, and the Phase 5 test offer render as the Cards defined there (Test Plan Card mechanics: `skills/ha-nova/test-run.md`); the diff mechanics stay in `skills/ha-nova/write-safety.md`.

See `skills/ha-nova/SKILL.md` → Response Format.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Destructive cleanup still requires `confirm:<token>`, even for items created earlier in the same session.
- Agents must use Relay only; no MCP, no direct HA API
- Every write MUST end with the Post-Write Review slot; use terminal-friendly labels where Markdown headings add noise.

## Guardrails

- Never use raw `get_states` — use targeted registry/config reads
- Max 3 related configs in collision scan

## References

Always load:
- `skills/ha-nova/relay-api.md`, `skills/ha-nova/payload-schemas.md`, `skills/ha-nova/best-practices.md`, `skills/ha-nova/write-safety.md`, `skills/ha-nova/smallest-solution.md`
- Agent templates: `skills/ha-nova/agents/resolve-agent.md`, `skills/ha-nova/agents/apply-agent.md`
- Review checks: `skills/review/checks.md` (self-contained catalog + Application)

On demand — read only when the trigger applies:
- `skills/ha-nova/automation-patterns.md` — drafting new branching, timing, or flow-control logic
- `skills/ha-nova/one-shot-automations.md` — a one-shot, "only today", or duration-bound request (this skill owns both halves of a duration)
- `skills/ha-nova/recovery-workflows.md` — recovery, watchdog, self-healing, or retry intent
- `skills/ha-nova/template-guidelines.md` — the draft contains Jinja templates
- `skills/ha-nova/safe-refactoring.md` — rename, migrate, or orphan-cleanup tasks
- `skills/ha-nova/update-revert.md` — the user asks to revert, undo, or restore a verified update (create cleanup stays out — see write-safety)
- `skills/ha-nova/config-snapshots.md` — capturing the pre-delete snapshot, or the user asks to restore from a config snapshot
- `skills/ha-nova/test-run.md` — Phase 5 test offer: feasibility, options, post-run follow-up
- `skills/ha-nova/input-capability-preflight.md` — remapping an input device (button, remote, switch)
- `skills/ha-nova/consumer-discovery-preflight.md` — repurposing or cleaning up an input's existing consumers
- `skills/ha-nova/threshold-calibration.md` — an update changes a physical-process threshold, `for:` duration, wait timeout, or the compared signal
