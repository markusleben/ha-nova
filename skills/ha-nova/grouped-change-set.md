# HA NOVA Grouped Change Set (Non-Destructive)

Canonical path: `skills/ha-nova/grouped-change-set.md`

The only supported route for confirming several NON-destructive mutations of one
logical task with a single natural confirmation. The destructive sibling is
`skills/ha-nova/batch-safety.md` (typed confirmation code, immutable manifest);
a grouped set never carries a destructive operation. The one destructive
workflow defined in this file is Cross-Family Destructive Cleanup below — a
separate typed-confirmation tier, never part of a grouped set.

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
  - service call / runtime action: re-run the indirect-actuation gate from
    scratch when ANY earlier operation in this group wrote to a config this
    run reaches — the group is the one actor whose changes its own previews
    cannot have seen. "Add the unlock to my arrival script, then run it" is
    two ordinary-looking operations that together perform a gated action, and
    re-reading only the target's existence does not notice. A tier that rises
    stops the group; it is not a confirmation the user gave. Also re-check
    that the target entity still exists; a target gone from the registry stops the group, while
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

## Dependency-Bound Outputs (#595)

A non-destructive group may carry a downstream operation whose payload needs an
identity a predecessor creates — the config entry of a new helper, or the
entity ID uniquely linked to it. Guessing the future slug stays forbidden; the
group declares the dependency explicitly instead:

- The predecessor exposes exactly ONE narrowly defined verified output: the
  config entry it creates, or its uniquely linked entity ID. Nothing else.
- The downstream operation references it through a typed manifest slot in its
  payload template — a named placeholder with a declared result shape (type,
  domain), never a guessed literal.
- The preview shows the resolver (how the output will be read back after the
  create), the allowed result shape, the downstream payload template with the
  slot marked, the semantic effect, and the deterministic order.
- Confirmation binds to the operations, the resolver, the constraints, the
  payload template, and the order; a change to any of them expires it.
- Execution: verify the predecessor per its owning skill, resolve exactly one
  matching identity, instantiate ONLY the approved slot, then run the
  downstream owning skill's normal pre-apply drift and impact checks unchanged.
- STOP the group and require a fresh preview on: ambiguous or missing
  resolution, an unexpected type or domain, a collision, any payload change
  beyond the approved slot, or foreign drift. The stop happens BEFORE the
  downstream write; the ledger reports it per Ledger & Partial Completion —
  applied, failed, not attempted, never atomic.

MVP scope: at most 10 non-destructive operations; supported owning skills only
— no fallback or experimental writes; a derived value fills only the
explicitly previewed typed slot. Reference cases: config-entry helper create →
dashboard entity reference (see the matrix's `dashboard` row), helper create →
automation/script reference. Destructive or high-consequence operations never
join a dependency-bound set.

## Capability Matrix (v1)

| Skill | Grouped support | Non-destructive scope |
|---|---|---|
| `write` | yes | automation/script creates and updates within one logical task |
| `helper` | yes | storage and supported config-entry helper creates/updates |
| `scene` | yes | storage scene creates/updates (Editability Guard per target) |
| `organize` | yes | registry metadata updates (areas, labels, categories, entity/device metadata) |
| `service-call` | yes | batch service calls per its Guardrails grouped manifest; high-consequence calls (confirmation-code tier) excluded |
| `todo` | yes | item operations on ONE list (add, complete, rename, update); list creates/deletes stay single-operation |
| `dashboard` | downstream only | an entity reference on an existing dashboard as the DOWNSTREAM operation of a dependency-bound set (see Dependency-Bound Outputs); never a standalone grouped family |
| all others | no | single-operation flows or the destructive batch contract |

## Cross-Family Destructive Cleanup (#583)

A separate typed-confirmation workflow, never part of a non-destructive grouped
set: when ONE fully resolved logical cleanup target spans several supported
families (delete a helper plus the automations referencing it; remove a retired
device's group memberships, dashboard cards, and statistics), the whole cleanup
may be confirmed with ONE manifest-bound confirmation code instead of one code
per family.

- Build one immutable manifest per `skills/ha-nova/batch-safety.md` mechanics,
  extended across families: every operation with its stable target identifier,
  owning skill, exact endpoint/payload semantics, per-item dependency/consumer
  impact result, per-family recovery path (snapshots, YAML exports, backup
  gate), and deterministic execution order.
- ONE typed code binds to that exact manifest:
  `confirm:cleanup-<target>-<count>-<digest>` (digest per batch-safety's rule,
  computed over this manifest). Any change to any operation, target, payload,
  impact result, or order — or an expired confirmation — invalidates it; show
  a new preview with a new manifest and a new code.
- Execute sequentially through the owning skills; every operation keeps its
  current pre-apply check, snapshot/backup gate, drift check, verification,
  timeout handling, and recovery rules. Fail fast; one ledger of succeeded,
  failed, and not attempted. This is not an atomic transaction and must never
  be presented as one.
- Scope: exactly ONE logical cleanup target; at most 10 fully enumerated
  operations; no selectors expanded after confirmation; supported operations
  only, each with a canonical preview and verification path.
- Excluded regardless of manifest quality: multiple independent cleanup
  targets; user/account and owner/relay-account operations; backup deletion;
  Home Assistant Core/OS/App updates; high-consequence or physically
  irreversible actions; MQTT command/`set` topics; experimental writes without
  a guarded schema and verification path; whole integration removal until its
  guarded lifecycle path exists (#520).

Same-family destructive batches stay in `batch-safety.md` unchanged — this
workflow exists only when the one logical target genuinely spans families.

## Exclusions

Never grouped, regardless of task shape:

- any operation requiring a typed confirmation code (deletes, destructive
  batches, high-consequence runtime actions). ONE named exception: a duration
  request's immediate action and its expiry automation are two halves of a
  single write, not a group of two operations — they preview and confirm
  together at the HIGHER tier of the two halves — the counter-action is the
  restore, and a restore can be the grant (`lock.lock` for an hour schedules an
  unattended `lock.unlock`)
  (`skills/ha-nova/one-shot-automations.md`). Separating them would leave a
  scheduled turn-off for something that was never turned on.
- experimental/fallback writes and YAML file edits
- operations whose preview cannot be rendered canonically
- selectors or worksets expanded after confirmation — the group is literal
