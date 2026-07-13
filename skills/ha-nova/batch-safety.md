# HA NOVA Batch Safety (Scoped Batch Manifest)

Canonical path: `skills/ha-nova/batch-safety.md`

The only supported route for confirming a destructive multi-target operation
with a single typed confirmation. Skill source stays English; localize labels
at runtime per `skills/ha-nova/output-rules.md`.

Confirmation validity stays owned by `skills/ha-nova/SKILL.md` → Safety
Baseline → Active Preview Confirmation. This file defines only the batch
mechanics: manifest, code format, caps, execution, and recovery.

## Purpose & Opt-In

- Batch mode exists only where the owning skill's SKILL.md declares batch
  support for a named resource family. Never infer batch support — not from
  this file, not from the context skill, not from user insistence.
- Single-target flows stay the default and continue unchanged. Batch mode is
  for a reviewed, fully enumerated workset of same-family targets.
- One resource family per manifest. Never mix families (e.g. automations and
  scripts, storage helpers and config-entry helpers, dashboards and resources)
  — split into separate manifests, each with its own preview and confirmation.

## Preconditions (all six required)

1. Every target is resolved to a stable identifier.
2. The complete target set is shown before confirmation.
3. Dependency/consumer impact is checked per target (each family's existing
   check, e.g. `search/related`).
4. The confirmation is bound to that exact set (see Confirmation Code).
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

## Confirmation Code

Format (typed verbatim, never a menu — context skill → Interactive Choices):

```
confirm:batch-<operation>-<family>-<count>-<digest>
```

Example: `confirm:batch-delete-automations-8-a1b2c3d4`. The digest is the
first 8 hex characters of the SHA-256 of the manifest file — compute it with
`shasum -a 256 <manifest-file>` (POSIX) or `Get-FileHash` (PowerShell). The
digest binds the code to the exact target set, payloads, and order; it is a
binding discriminator, not a security boundary — the behavioral rule below is
the real gate.

Any change to a target, payload, endpoint, scope, dependency result, or
execution order expires the confirmation; show a new preview with a new
manifest and a new code.

User-facing wording follows `skills/ha-nova/output-rules.md` → Localization:
the code is called the "confirmation code" (localized), never a "token".

## Caps

The preview always shows `count / cap`. Above the cap, refuse and split into
separate manifests, each with its own preview and confirmation code:

- config-item families (automations, scripts, helpers, scenes, dashboards,
  resources, cards within one dashboard, to-do lists, registry
  categories/labels/areas/floors): **20**
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
  new preview and a new confirmation code — the target set changed.

## Recovery

- Run the proportional backup gate before broad or hard-to-reconstruct
  deletes (write-safety → Safety-Mechanism Availability; offer `ha-nova:backup`).
- Export or snapshot reconstructable configs (e.g. automation/script YAML to
  a user-visible file) before deleting, when feasible.
- State plainly when there is no automatic rollback. A backup or snapshot
  never weakens the typed confirmation requirement.

## Batch Cards

Same rules as `skills/ha-nova/output-rules.md` → Cards (fixed emoji
vocabulary, two spaces after 🗑️/⚠️, labels localized at runtime).

**Batch Delete Card** (the code prompt is always the last line):

```
🗑️  Delete: 8 automations (reviewed cleanup workset)
Manifest: <path the user can open> — 8 / 20 (cap)
Impact: no consumers found for 7; "Morning routine" is referenced by script.wakeup (will break).
Recovery: YAML export written; no automatic rollback after delete.
Runs one by one — stops at the first error.
⚠️  Nothing deleted yet.
To delete all 8, reply exactly: confirm:batch-delete-automations-8-a1b2c3d4
```

**Batch Result Card**:

```
✅ Deleted: 8 of 8 automations
Ledger: 8 succeeded · 0 failed · 0 not attempted.
Checked per item: config gone, entity gone. Unrelated automations untouched: total went 42 → 34, exactly the 8 deleted.
```

On partial failure the ✅ line names the applied subset instead, the ledger
shows the failure, and the card closes with the exact safe next step (usually
the offer of a remaining-targets manifest).

## Capability Matrix (v1)

| Skill | Batch support | Families / rationale |
|---|---|---|
| `write` | yes | automations OR scripts (never mixed); per-item consumer check; YAML export before delete when feasible |
| `helper` | yes | one helper family per manifest; storage and config-entry families never mixed |
| `mqtt` | yes | retained discovery cleanup for ONE resolved device (topics only from `mqtt/device/debug_info`); command/`set` topics excluded |
| `maintenance` | yes (already grouped) | statistics clears per issue group via the API's single-call group clear (ledger from per-ID re-validation); orphan registry removal per config entry, sequential; all existing gates unchanged |
| `scene` | yes | storage scenes only (Editability Guard per target) |
| `dashboard` | yes | dashboards, resources, and cards are three separate families; a card batch is one merged save (single-call path) |
| `todo` | yes | list deletion with per-list open-item counts; item removal stays the existing sub-token flow |
| `organize` | yes | one registry family per manifest; only with complete related-item impact per target |
| `yaml-config` | no | file edits are single-document operations with their own backup flow |
| `energy` | no | preference saves are corrective single-document writes, not enumerable deletes |
| `service-call` | no | actuating calls are excluded; its grouped manifest (see its Guardrails) is a separate non-destructive tier |
| `backup` | no | backup deletion stays single-target |
| `updates` | no | Core/OS/App updates stay single-target |
| `admin` | no | user/person/account deletion stays single-target |
| `fallback` | no | experimental writes never batch |

## Exclusions (v1)

Keep these single-target regardless of manifest quality:

- user or account deletion; owner or relay-account operations
- whole integration removal
- Home Assistant Core/OS updates
- backup deletion
- arbitrary service calls that actuate devices (see `ha-nova:service-call` —
  its grouped manifest is non-destructive and separate from this contract)
- MQTT command/`set` topics
- mixed resource families
- selectors evaluated only at execution time
