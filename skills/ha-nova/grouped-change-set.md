# HA NOVA Grouped Change Set (Non-Destructive)

Canonical path: `skills/ha-nova/grouped-change-set.md`

The only supported route for confirming several NON-destructive mutations of one
logical task with a single natural confirmation. The destructive sibling is
`skills/ha-nova/batch-safety.md` (typed confirmation code, immutable manifest);
this contract never carries a destructive operation.

Confirmation validity stays owned by `skills/ha-nova/SKILL.md` → Safety Baseline
→ Active Preview Confirmation. This file defines only the grouping mechanics:
scope, preview protocol, the single final action block, execution, and the
ledger.

## Purpose & Opt-In

- A grouped change set exists only where the owning skill's SKILL.md declares
  grouped support. Never infer it — not from this file, not from the context
  skill, not from user insistence.
- Group ONLY operations that belong to one user-requested logical task. Two
  unrelated wishes are two groups, even when convenient to merge.
- Mixed supported configuration families are allowed when they form one logical
  task (an automation update plus the helper it reads, a scene plus the script
  that activates it). This differs deliberately from the destructive batch
  invariant — destructive batches keep one resource family per manifest.
- Single-operation flows stay the default and continue unchanged.

## Boundaries

- **Cap: 10 operations.** Above it, split into explicit separate groups, each
  with its own previews and final confirmation.
- **No destructive operations.** Anything that requires a confirmation code
  (deletes, destructive batches, high-consequence runtime actions) is rejected
  from the group and keeps its own flow. A task mixing both keeps the task's
  dependency order: each code-gated operation runs in its own flow at its
  position (e.g. a delete that frees an ID before a grouped create), and the
  grouped set covers the non-destructive remainder — split into two groups
  when a code-gated step sits between grouped operations.
- Every operation must be individually previewable with the owning skill's
  canonical card. An operation whose preview cannot be produced leaves the
  group — and takes every operation that depends on it along (never apply a
  set whose semantic basis is missing); if the task cannot stand without it,
  stop and say so instead of grouping a fragment.

## Preview Protocol

1. Announce the group: the logical task, the operations in order, and the
   families involved. The set as a whole must satisfy the active user
   decisions (context skill → Decision Memory).
2. Render the full canonical Preview Card for EVERY operation (Cards contract
   unchanged — behavior narrative, changes block, pre-write check).
3. Intermediate previews carry NO options block and no repeated menu — the
   cards end with their save-status line (`⚠️  Nothing saved yet.`).
4. After the last preview, exactly ONE final action block closes the group with
   the canonical keywords `apply · show yaml · cancel` (`show yaml` re-renders
   any operation's full payload on request; then the final block is shown
   again).
5. Confirmation is the ordinary natural-language apply, bound to the exact
   displayed set (operations, order, payloads). Any change to any operation,
   its payload, or the order expires the confirmation; re-preview the changed
   operation and show a new final block.

## Execution

- Sequential, in previewed order; fail fast on the first unexpected result.
- Immediately before EACH operation, run the operation-specific pre-apply
  check; on foreign change, STOP the group:
  - update: the owning skill's drift check (write-safety → Drift check before
    apply) — re-read the target and compare against the previewed base;
  - create: verify the previewed ID/slug is still absent (no collision
    appeared since the preview);
  - service call / runtime action: re-check that the target entity still
    exists; a target gone from the registry stops the group, while
    `unavailable`/`unknown` follows the owning skill's preview rules
    (warning/info, not a block — the call may still work). Broad targets
    (`area_id`/`device_id`) re-expand to their member list before applying;
    membership drift against the previewed expansion stops the group — the
    confirmed set is literal.
- Verify each applied operation with the owning skill's existing rules before
  moving on.
- Never claim the group is atomic, transactional, or automatically revertible.

## Ledger & Partial Completion

Track every operation as planned / applied / skipped / failed. On failure,
cancellation, or a drift stop: report the exact applied subset, the failure or
drift, and what was not attempted — never continue silently into a
half-applied state. Recovery stays per-operation (update-revert snapshots,
normal delete flows for unwanted creates); resuming the remainder is a new
grouped set with fresh previews.

## Grouped Cards

Same rules as `skills/ha-nova/output-rules.md` → Cards. Per-operation Preview
Cards render unchanged minus the options line; the group closes with:

```
📝 Group: rename "Morning" scene set (3 operations, 2 families)
1. scene "Morning bright" — rename + new transition
2. script "Morning start" — points at the renamed scene
3. helper input_boolean.morning_guard — new friendly name
⚠️  Nothing saved yet — one confirmation applies all 3 in this order, one by one.
Options: apply · show yaml · cancel
```

The Result Card reports the ledger:

```
✅ Applied: 3 of 3 operations
Ledger: 3 applied · 0 skipped · 0 failed.
Ran one by one — not atomic; revert stays per operation (`revert`, snapshots).
```

On partial completion the ✅ line names the applied subset and the ledger shows
the stop reason; the card closes with the exact safe next step.

## Capability Matrix (v1)

| Skill | Grouped support | Non-destructive scope |
|---|---|---|
| `write` | yes | automation/script creates and updates within one logical task |
| `helper` | yes | storage and supported config-entry helper creates/updates |
| `scene` | yes | storage scene creates/updates (Editability Guard per target) |
| `organize` | yes | registry metadata updates (areas, labels, categories, entity/device metadata) |
| `service-call` | yes | batch service calls per its Guardrails grouped manifest; high-consequence calls (confirmation-code tier) excluded |
| all others | no | single-operation flows or the destructive batch contract |

## Exclusions

Never grouped, regardless of task shape:

- any operation requiring a typed confirmation code (deletes, destructive
  batches, high-consequence runtime actions)
- experimental/fallback writes and YAML file edits
- operations whose preview cannot be rendered canonically
- selectors or worksets expanded after confirmation — the group is literal
