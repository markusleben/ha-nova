---
name: write
description: Use when creating, updating, or deleting Home Assistant automations or scripts through HA NOVA Relay. Resolves entities and reviews internally.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Write

## Scope

Mutations of `automation` and `script` configs: `create`, `update`, `delete`,
and `revert` of a verified update — plus one runtime exception: the paired
immediate service call of a DURATION request and `automation.turn_off` on an
expiry automation this flow just created (splitting them loses the pairing,
`../ha-nova/one-shot-automations.md`).

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`. An unreachable Relay answers exit 1 `RELAY_ERROR`
(then `ha-nova setup`). Multi-server: add `--server <name>` to every verb (or
prefix `HA_NOVA_SERVER=<name>`); name a non-default server once per response.

## Relay Contract

Typed verbs only — this skill sends no raw `ha-nova relay` config write:
- Read: `ha-nova find "<phrase>" --domain automation|script`; `ha-nova get <entity_id> --config` (current config + `config_id` from the registry `unique_id`, never the slug); `ha-nova get <entity_id> --related` (who uses an entity).
- Plan (read-only): `ha-nova plan write --domain automation|script [--id <config_id> | --create] --file <draft.json>`, `ha-nova plan delete <entity_id>`, `ha-nova plan revert [--target <config_id>] [--domain <d>]`.
- Execute: `ha-nova apply --plan <id> [--confirm <code>] [--no-snapshot]`.
- Duration halves: `ha-nova get` (restore value, read-back), `ha-nova plan call` + `apply` (`../service-call/SKILL.md` exit-code rules).

The draft is the full config as one JSON object in a client-private scratch
file. The CLI posts its bytes exactly, so key order is behavior: `variables`,
`trigger_variables`, and script `fields` evaluate top-down — keep an existing
item's order. Every update passes `--id <config_id>` from `get --config`.
Every create passes `--create`: the CLI refuses an existing id and an `id`
inside an automation draft, so a copy never overwrites its original (the CLI
mints automation ids; a script key comes from the alias, or `--id` only for a
key the user chose, and becomes `script.<key>`). An update whose `diff` has no
before side means the item is gone — STOP and ask.

## Flow

Multi-target logical changes: present the plan first per `../ha-nova/write-safety.md` → Multi-Target Changes. A non-destructive grouped change set (max 10 operations, `../ha-nova/grouped-change-set.md`) plans each item, shows the Phase 2 previews without per-preview options, takes one final confirmation, then applies each plan; a typed plan leaves the set, any exit 2 stops it.

Input-device remaps: run the capability preflight per `../ha-nova/input-capability-preflight.md` before drafting; the write stays blocked while the chosen gesture is only assumed or its evidence conflicts. Repurposing or cleaning up an input additionally runs `../ha-nova/consumer-discovery-preflight.md`; incomplete coverage is disclosed, never claimed as "unused".

A one-shot intent ("just today", "tell me when the laundry finishes") takes the self-disabling pattern in `../ha-nova/one-shot-automations.md` and skips unsolicited improvement offers — the Suggestion Block AND the Phase 5 test offer (a real-run test would undo the duration). A recovery, watchdog, self-healing, or retry intent reads `../ha-nova/recovery-workflows.md` before drafting; the preview names the attempt bound — and the persistent-fault path only where the intent promises watchdog/self-healing continuity: an explicitly one-shot recovery keeps an attempt bound of one.

Every draft follows `../ha-nova/smallest-solution.md`: the complete explicit outcome in the simplest safe design, native primitives when equally safe and clearer.

### Phase 1: Resolve

1. Bare "undo the last update": straight to `plan revert`; undo a delete: run its result's `restore` verbatim. update/delete/named revert: `ha-nova find` (exact `entity_id` wins; `--include-disabled` for a disabled item), then `ha-nova get <entity_id> --config` for the current config and `config_id` — `managed: yaml` or `CONFIG_NOT_FOUND` there means a YAML/package item: `ha-nova:yaml-config`, never `plan write`/`plan delete`. Ambiguity or no match: ask one blocking question for the exact entity. No second ambiguity question. Correct invalid premises.
2. Resolve every entity the draft uses with `ha-nova find`/`get`; never guess an id.
3. Automations: `bp_status` per `../ha-nova/best-practices.md` → BP Cache Policy (scripts and deletes: `n/a`).
4. Suggestion candidates per `../ha-nova/smallest-solution.md` → Write Suggestion Candidates.

### Phase 2: Preview + Confirm

1. Build the draft. Update: full-replacement merge (base=current, overlay=user changes); never rewrite unrelated structure, aliases, or formatting (key order included). Treat notification copy as user-authored content: preserve notification titles, messages, templates, metadata; rename/timing changes must not restyle, relocalize, or restructure existing text, and requested wording changes change only the requested copy. New or explicitly edited notification actions follow the canonical contract `../ha-nova/mobile-notification-composition.md`. Its recurring-workflow INTENT classification is not so limited: it runs before the write preview on EVERY recurring-workflow write — existing notification actions and trigger-only edits included, their payloads staying byte-for-byte untouched — and may add an exception-only Suggestion Block item.
2. BP gate (not for delete; `../ha-nova/write-safety.md`; refresh: `../ha-nova/best-practices.md`): fresh, or any status on a simple change → continue; stale/missing/invalid + complex → block.
3. Suggestions + Pre-Write Checks (skip for delete and revert):
   - **3a) Suggestions**: the candidates as the Suggestion Block (output-cards.md; max 2, smallest intervention first). Accepted numbers merge into the draft BEFORE the preview; no block when there is no candidate and no intent-classification item.
   - **3b) Static Checks**: `../review/checks.md` → Application, analytically on the draft — no relay calls (scripts: F-01..F-08; helper refs: H-01..H-08. Defer H-09/H-10 to Phase 4). One pre-write verdict line ("Pre-write check: no issues worth flagging before save." or "Pre-write check: this draft may not behave as intended."); 🔴 inline with a fix, 🟠🟡 as advisory below the preview, code-free. Advisories do not block the write and do not require extra confirmation:
     - R-18: a variable used above the sibling it reads — move that sibling up.
     - R-19: final else branch is only reached when the earlier entity-state branches are false — move `trigger.id` into `elif` or use `choose` + `condition: trigger`.
     - If R-23 matches: boolean-like template values compared to `"True"`/`"False"` — use the boolean.
     - If R-24 matches: `available_energy` may be current charge, not capacity — ask the user; never rewrite integration entity ids.
     Track findings by check type for dedup in Phase 4, except R-18.
   - **3c) Pre-Write Impact (update only)**: `ha-nova get <entity_id> --related` on the top entities the item acts on or reads — NEVER on the item's own `entity_id` (that is the delete direction). Show affected automations/scripts/scenes as advisory (read max 3 configs). An empty result names the scanned entities; a failed read is "consumer check inconclusive" — never a no-consumer claim.
   - **3d) Threshold Calibration (update; create for stored threshold setters)**: an update that changes a `numeric_state` `above`/`below`, its `for:`, a wait timeout, or a numeric template predicate on a physical-process sensor — or swaps the compared `entity_id`/`attribute`/`value_template`, or adds/changes an action setting a threshold-backing `input_number` — runs `../ha-nova/threshold-calibration.md` and carries its findings (observed ranges, longest ambiguous phase, data gaps, both failure directions) into the preview; bounded reads of existing traces are its explicit exception to the no-auto-trace rule. A create runs it only for a stored setter on a helper existing items use as a physical-process boundary (one `ha-nova get <helper> --related`); otherwise create keeps its 3c skip. Unrelated numeric edits skip it.
4. Plan: `ha-nova plan write` (delete: `plan delete`; revert: `plan revert`). Exit 3 is normal: a stored plan with `plan.confirm` `natural` or `code`. Exit 4: `CONFIG_NOT_UI_MANAGED` → YAML item, `ha-nova:yaml-config`; `CONFIG_ID_IN_USE` → first check whether an earlier apply created it (then never create another); otherwise automation: plan again without `--id`, script: ask for another name, plan again with `--id <new> --create`; `NO_UNDO_SNAPSHOT` → nothing to revert; `REVERT_UNSUPPORTED` → the newest checkpoint is a helper's — pass `--target <config_id>` (or `ha-nova:helper`). Exit 5 on delete: not found. Revert exit 2 `REVERT_DRIFT`: the item changed since its last verified update — nothing planned; show the live config and ask what to write instead.
5. Preview (fixed shape: `../ha-nova/write-safety.md`):
   - update/revert: the plan's `diff` rows **verbatim** in the Changes slot — never write it yourself; state the behavioral effect in the summary sentence (write-safety → Behavior narrative). create: summary. Name the `members[]` that set the tier. Close the card with the plan's `say` (it ends in `apply` · `show yaml` · `cancel` or the code; those, names, and state values stay literal); several plans in one preview keep each reason and ask once, naming each typed plan's code.
   - Delete preview MUST include the consumer-check result before confirmation, from the plan's `consumers` (who references the item's OWN `entity_id`): the consumers; `[]` is an explicit none among automations, scripts, and scenes; absent (`coverage.complete` false) is "consumer check inconclusive", never a no-consumer result. No menu — only `say`.
   - Batch delete of one family (automations OR scripts, never mixed) follows `../ha-nova/batch-safety.md`; each item keeps its consumer check, its own CLI code (none in a window or phone plan: follow `next_step`; the manifest lists them; at most 10 items, planned just in time), and absence verification.
6. Confirmation: create/update=natural, delete=one-time code — the plan's CLI-minted `plan.code` (none: follow `next_step`); a create/update/revert whose plan says `code` takes it too; delete is the one-time code, never a menu. A duration request takes the code when EITHER half grants physical access or is physically irreversible (`lock.lock` for an hour schedules an unattended `lock.unlock`). A cross-family destructive cleanup (`../ha-nova/grouped-change-set.md`) still needs each automation/script item's own CLI code. Relay `say` in full and add no other reason; never cite, link, or name this skill or any HA NOVA rule file. A code counts only as a reply that is only the code, after the preview; "yes" is invalid. After a "yes", repeat `say`'s ask (with its note). Never pass a code the user did not type. Pre-preview consent is draft-only; a changed draft needs a new plan and preview. An answer to a clarifying question is never a confirmation: end the turn with each new or changed plan's preview (except Phase 3 step 4's natural-confirmation duration call re-plan with action and tier unchanged).

### Phase 3: Apply + Verify

1. `ha-nova apply --plan <id>` (`--confirm <the user's exact text>` when typed; no `plan.code`: per `next_step`). Apply re-plans first: exit 2 means the live config changed since the preview — nothing written; show the new diff and ask again. Never retry after exit 6 or 7.
2. `verified` = the read-back equals the planned body (content and `variables`/`fields` order), the reload succeeded, the registry entity is live; `unverified` says why in `verification[].note` or `.observed` (a failed reload: saved, but whether it runs is unknown — say so and offer a reload through `ha-nova:service-call`); exit 7 is unknown — read `ha-nova get <entity_id> --config` before saying anything; `before_config` in the result is the way back. A notice of kind `undo` means the update is not revertible: say so. Do not report destructive success until verification proves the target is gone.
3. Deletes: the CLI saves the pre-delete config snapshot (`snapshot` in the result). `SNAPSHOT_FAILED` deleted nothing: offer pruning the snapshot store, a full safety backup (`ha-nova:backup`), or proceeding without one; only after the user picks the last, `apply --plan <id> --confirm <code> --no-snapshot`.
4. **Duration requests carry an immediate action too** — "sprinkler for 30 minutes" is the expiry automation PLUS the turn-on under one preview (`../ha-nova/automation-patterns.md`): plan both first (`plan write`, `plan call`) for the one preview. Apply and verify the automation, re-check deadline margin and restore value — if stale, disable the armed expiry immediately and remove it through the delete flow before re-previewing. The new automation watches the actuator, so the call plan drifts: re-plan it (`plan call`) and apply the new plan under the same confirmation only if action and tier are unchanged (typed: the user types its new code); otherwise it fails below. VERIFY the immediate action by reading the target back; on any failure — a non-zero exit (drift, expired plan, refusal, error) included — disable the automation at once (`ha-nova call automation.turn_off <entity_id>`) and clean it up through the delete flow before anything is re-previewed. Never hand the immediate half back to the user as a separate step.
5. Report the result. No raw curl/JSON in output.

### Phase 4: Post-Write Review (MANDATORY)

Do NOT invoke `ha-nova:review` separately.

1. Reviewed config: the draft when `verified`, else `ha-nova get <entity_id> --config`. If `verification[].entity_id` differs from the requested slug, report it and point to `../ha-nova/safe-refactoring.md` — do not silently assume the requested slug won.
2. Checks: verified → skip what Phase 2 covered, re-run R-18 on the read-back; a differing read-back → full checks per `../review/checks.md` → Application. **Dedup**: findings the user saw in 3b MUST NOT repeat (by check type) — except R-18 still matching the persisted config: report it again as a persisted runtime risk with the reorder fix. Do not auto-trigger or auto-read traces outside an accepted Phase 5 test plan. `R-19` follows normal dedup, as do R-23/R-24. Actions referencing helpers: always H-01..H-11.
3. Collision scan: `ha-nova get <entity_id> --related` on the written item (3c fail-closed rule); read max 3 related configs.
4. Output (localized; `../ha-nova/output-cards.md` → Findings, Empty States): **Findings** real issues only; **Collision check** only when related items exist (a failed scan is "consumer check inconclusive", not empty); **Advisory** only when non-empty. Omit any section with nothing to report — never print an empty "none" bucket; nothing at all → collapse to one scope-honest confirmation line (write-safety → Verification Honesty). Never emit `Questions to consider`, `Suggestions`, or `Instant help` post-write; never repeat an item across **Findings** and **Advisory**. Sole exception: the one-line Replication Line after a verified create (`output-cards.md`).
5. Update-Revert: an update (a revert too) lands on the undo stack automatically — reflect the save receipt in the result — the `undo` receipt (`replaced`: only this newest update stays revertible; evicted targets are no longer revertible) and offer `revert` (write-safety → Update-Revert). Creates → cleanup via normal HA NOVA delete flow with preview, one-time code, and absence verification. Deletes → offer `restore` (one plan + apply), else Home Assistant Backups.

### Phase 5: Test Offer (create/update only)

After the review output, build a Test Plan Card per `../ha-nova/test-run.md`: one recommended option (a real run where acceptable, naming the devices it switches), at most 3 options plus `skip`. Never execute anything unconsented; the user's option choice is the single confirmation bound to that card, except for high-consequence members, where the card confirmation never replaces the one-time code. Runs follow the service-call rules, then test-run.md's follow-up (trace, state verify, restore). After one skip, de-escalate to a single line for later writes. Skip this phase for deletes.

## Output Format

Apply `../ha-nova/output-rules.md`.

Previews, delete prompts, results, and the test offer render as the Cards in `../ha-nova/output-cards.md` (Test Plan Card mechanics: `../ha-nova/test-run.md`). A create summary names the item (alias + config id; `entity_id` once verified), the entities it uses, then `Triggers`/`Conditions`/`Actions` (scripts: `Fields`/`Sequence`) in short descriptions, and `mode`; the full YAML only on `show yaml`.

## Safety

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary`; everything else waits for the user's confirmation of the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes or other destructive operations need the user's one-time code from HA NOVA, or their window click or phone tap; "yes" or any other reply is invalid.
- Confirmation binds to the displayed preview; any change to target, payload, endpoint, or scope voids it (context skill → Active Preview Confirmation).
- Relay a plan's `say` in full, localized; never drop or shorten sentences.
- Anything before the preview ("do it", answers to questions) never confirms it; only a reply to the preview does.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- Any HA write this skill does not cover: STOP, invoke `ha-nova:fallback` first; never probe unfamiliar write endpoints.

- Destructive cleanup still requires the one-time code, even for items created earlier in the same session.
- No third-party MCP server, no direct HA API, no raw `ha-nova relay` config write.
- Every write MUST end with the Post-Write Review slot.

## Guardrails

- Never use raw `get_states`.

## References

Always load:
- `../ha-nova/payload-schemas.md`, `../ha-nova/best-practices.md`, `../ha-nova/write-safety.md`, `../ha-nova/smallest-solution.md`
- Review checks: `../review/checks.md` (catalog + Application; evidence boundaries on demand as it says)
- A delete needs only `../ha-nova/write-safety.md` of these.

On demand — read only when the trigger applies:
- `../ha-nova/automation-patterns.md` — new branching, timing, or flow-control logic
- `../ha-nova/one-shot-automations.md` — one-shot or duration-bound requests
- `../ha-nova/recovery-workflows.md` — recovery, watchdog, or retry intent
- `../ha-nova/template-guidelines.md` — the draft contains Jinja templates
- `../ha-nova/safe-refactoring.md` — rename, migrate, or orphan cleanup
- `../ha-nova/update-revert.md` — revert, undo, or restore a verified update
- `../ha-nova/config-snapshots.md` — restore from a config snapshot
- `../ha-nova/test-run.md` — Phase 5 test offer
- `../ha-nova/input-capability-preflight.md` — remapping an input device
- `../ha-nova/consumer-discovery-preflight.md` — repurposing an input's consumers
- `../ha-nova/threshold-calibration.md` — threshold, `for:`, wait-timeout, or compared-signal changes
