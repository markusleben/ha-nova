# HA NOVA Batch Safety (Scoped Batch Manifest)

Canonical path: `../ha-nova/batch-safety.md`

The only supported route for confirming a destructive multi-target operation
with a single typed confirmation. Skill source stays English; localize labels
at runtime per `../ha-nova/output-rules.md`.

Confirmation validity stays owned by `../ha-nova/SKILL.md` → Safety
Baseline → Active Preview Confirmation. This file defines only the batch
mechanics: manifest, code format, caps, execution, and recovery.

Exception: automations and scripts — `ha-nova plan delete` binds each code to one delete, so their manifest lists one CLI code per item (at most 10 items, planned just in time) and the user types each (a window or phone plan has none: follow its `next_step`); never pass a code the user did not type.

## Purpose & Opt-In

- Batch mode exists only where the owning skill's SKILL.md declares batch
  support for a named resource family. Never infer batch support — not from
  this file, not from the context skill, not from user insistence.
- Single-target flows stay the default and continue unchanged. Batch mode is
  for a reviewed, fully enumerated workset of same-family targets.
- One resource family per manifest. Never mix families (e.g. automations and
  scripts, storage helpers and config-entry helpers, dashboards and resources)
  — split into separate manifests, each with its own preview and confirmation.
  The sole cross-family route is `../ha-nova/grouped-change-set.md` →
  Cross-Family Destructive Cleanup (one logical target, one manifest-bound
  code).

## Preconditions (all six required)

1. Every target is resolved to a stable identifier.
2. The complete target set is shown before confirmation.
3. Dependency/consumer impact is checked per target (each family's existing
   check, e.g. `search/related`).
4. The confirmation is bound to that exact set (see One-Time Code).
5. Execution is sequential and verified per target.
6. Any change to the set invalidates the confirmation.

If any precondition cannot be met, fall back to the single-target flow.

## Manifest Requirements

Write the manifest to a file the user can open (long lists never fit a chat
preview — a capped sample never authorizes the uncapped set). The manifest is
immutable after preview and includes:

- operation class (`delete`, `clear_retained`, `registry_remove`, ...)
- owning skill and resource family
- scope anchor (one MQTT device, one config entry, one dashboard, one issue
  group, one reviewed workset)
- every target's stable identifier and user-facing name
- exact endpoint/service and payload semantics per target
- total target count and the configured cap
- dependency/consumer-check result per target
- recovery path and backup requirement
- deterministic execution order

No selector, wildcard, prefix, label, area, query, or "all matching"
expression may be expanded after confirmation. The manifest lists literal
targets; anything discovered later belongs to a new manifest.

## One-Time Code

One one-time code from HA NOVA per manifest (context skill → Confirmation
Tiers): four random digits minted when the manifest is shown, typed by the
user, never a menu (context skill → Interactive Choices). Never reuse an
earlier code or take digits from the manifest or Home Assistant data. The code
belongs to that exact manifest — target set, payloads, and order; the
behavioral rule below is the real gate.

Any change to a target, payload, endpoint, scope, dependency result, or
execution order expires the confirmation; show a new preview with a new
manifest and a new code.

User-facing wording follows `../ha-nova/output-rules.md` → Localization:
the code is called the "one-time code from HA NOVA" (localized), never a "token".

## Caps

The preview always shows `count / cap`. Above the cap, refuse and split into
separate manifests, each with its own preview and one-time code:

- config-item families (automations, scripts, helpers, scenes,
  resources, cards within one dashboard, to-do lists, registry
  categories/labels/areas/floors, config snapshot blobs): **20**
- evidence-derived cleanup families (retained discovery topics, orphaned
  statistics IDs, orphan registry entries per config entry): **100**

## Execution

- Sequential, in manifest order, unless the owning API provides a proven
  single-call batch endpoint (e.g. `recorder/clear_statistics` for one issue
  group) — then the completion ledger derives from per-target verification
  after that call. Never claim the batch is atomic.
- Fail fast on the first unexpected result.
- Verify every completed target individually using the owning skill's
  existing verification rules.
- Keep a completion ledger: succeeded, failed, not attempted.
- Do not blindly retry timeouts; verify state first via the owning skill's
  rules (e.g. maintenance's re-read-instead-of-retry rule).
- Where the owning API permits, verify an unrelated-resource invariant (e.g.
  sibling entities of an untouched device still present, total count of the
  family matches expectation).

## Partial Failure & Resume

- On failure or cancellation, stop and show the ledger: the exact applied
  subset, the failure, and what was not attempted — never continue silently
  into a half-applied state.
- Resume operates only on a new manifest for the remaining targets, with a
  new preview and a new one-time code — the target set changed.

## Recovery

- Run the proportional backup gate before broad or hard-to-reconstruct
  deletes (`../ha-nova/recovery-coverage.md` → Safety-Mechanism Availability; offer `ha-nova:backup`).
- Export or snapshot reconstructable configs (e.g. automation/script YAML to
  a user-visible file) before deleting, when feasible.
- State plainly when there is no automatic rollback. A backup or snapshot
  never weakens the typed confirmation requirement.

## Batch Cards

Same rules as `../ha-nova/output-cards.md` → Cards (fixed emoji
vocabulary, two spaces after 🗑️/⚠️, labels localized at runtime).

**Batch Delete Card** (the code prompt is always the last line). The line
after the 🗑️ title states in plain language what the workset does today and
that this stops on delete — a bare count plus manifest path is not a preview:

```
🗑️  Delete: 8 timer helpers (reviewed cleanup workset)
All eight are kitchen timers that stop working when deleted.
Manifest: <path the user can open> — 8 / 20 (cap)
Impact: no consumers found for 7; "Tea timer" is referenced by script "Wake up" (will break).
Recovery: a config snapshot of each is saved first; a restore makes a new copy, so references stay broken.
Runs one by one — stops at the first error.
⚠️  Nothing deleted yet.
Type <four digits> to delete all 8 — a plain yes can't confirm this.
```

**Batch Result Card**:

```
✅ Deleted: 8 of 8 timer helpers
Ledger: 8 succeeded · 0 failed · 0 not attempted.
Checked per item: gone from Home Assistant. Unrelated helpers untouched: total went 42 → 34, exactly the 8 deleted.
```

On partial failure the ✅ line names the applied subset instead, the ledger
shows the failure, and the card closes with the exact safe next step (usually
the offer of a remaining-targets manifest).

## Capability Matrix (v1)

| Skill | Batch support | Families / rationale |
|---|---|---|
| `ha-nova` | yes | exact config snapshot blobs only (`config-snapshots` family); categories may mix because they remain one generic blob resource family |
| `write` | yes | automations OR scripts (never mixed); per-item consumer check; each item keeps its own `ha-nova plan delete` code (the CLI binds a code to one delete — the manifest lists them all and the user types each, none in a window or phone plan; no single batch code); the CLI takes each item's config snapshot |
| `helper` | yes | one helper family per manifest; storage and config-entry families never mixed |
| `mqtt` | yes | retained discovery cleanup for ONE resolved device (topics only from `mqtt/device/debug_info`); command/`set` topics excluded |
| `maintenance` | yes (already grouped) | statistics clears per issue group via the API's single-call group clear (ledger from per-ID re-validation); orphan registry removal per config entry, sequential; all existing gates unchanged |
| `scene` | yes | storage scenes only (Editability Guard per target) |
| `dashboard` | yes | resources and cards are separate families; a card batch is one merged save (single-call path); dashboards delete one at a time (a `plan delete` code binds one) |
| `todo` | yes | list deletion with per-list open-item counts; item removal keeps its existing natural-confirmation flow |
| `organize` | yes | one registry family per manifest; only with complete related-item impact per target |
| `device` | no | destructive steps (device removal, orphan rows) stay with the HA UI and `maintenance` |
| `migrate` | no | retiring an old rule is one delete through `write` or `yaml-config` |
| `yaml-config` | no | file edits are single-document operations with their own backup flow |
| `esphome` | no | one device file per edit, through the yaml-config backup flow |
| `energy` | no | preference saves are corrective single-document writes, not enumerable deletes |
| `service-call` | no | actuating calls are excluded; its grouped manifest is a separate non-destructive tier (`../ha-nova/grouped-change-set.md`) |
| `backup` | no | backup deletion stays single-target |
| `updates` | no | Core/OS/App updates stay single-target |
| `admin` | no | user/person/account deletion stays single-target |
| `assist` | no | pipeline deletion stays single-target |
| `fallback` | no | experimental writes never batch |

## Exclusions (v1)

Keep these single-target regardless of manifest quality:

- user or account deletion; owner or relay-account operations
- whole integration removal
- Home Assistant Core/OS updates
- backup deletion
- arbitrary service calls that actuate devices (see `ha-nova:service-call` —
  its grouped manifest is non-destructive and separate from this contract:
  `../ha-nova/grouped-change-set.md`)
- MQTT command/`set` topics
- mixed resource families (sole route: the cross-family cleanup workflow, see
  Purpose & Opt-In)
- selectors evaluated only at execution time
