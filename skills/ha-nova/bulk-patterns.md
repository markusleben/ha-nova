# HA NOVA Bulk Discovery and Audit Patterns

Shared contract for multi-target inventory and audit workflows.

Use this companion doc when a task targets more than one automation, script, or helper.

## Scope

- bulk inventory and shortlist building
- bulk audit preparation
- shared selector semantics for `prefix`, `domain`, `area`, and `label`

Not for:
- bulk writes
- batch service calls
- hidden background paging

Bulk review stays read-only. Do not offer Quick-Fix in bulk mode.

## Selector Semantics

### `prefix`

- Match case-insensitively against:
  - the `entity_id` suffix after the domain prefix
  - the display name / alias when available
- Prefer true prefix matching over loose substring matching when the user explicitly says "prefix".

### `domain`

- Exact domain filter only.
- For automation/script inventory, limit to the requested domain unless the user explicitly asks for a mixed shortlist.

### `area`

- Resolve the area name or `area_id` first.
- `config/area_registry/list` returns area objects with canonical key `area_id`; do not expect a generic `id` field.
- Use `search/related` on the resolved area as the canonical room/area shortlist source for automations, scripts, and entities.
- Treat direct registry `area_id` evidence as optional narrowing only when it is actually populated.
- The live `search/related` area response is an object keyed by type such as `automation`, `script`, `entity`, and `device`; do not assume an array payload.
- For automations/scripts, direct `area_id` is often sparse. If the user intent is room/area ownership rather than literal metadata assignment, prefer the area-related shortlist before any registry-only filter.
- Canonical projection by target family:
  - automation inventory/review -> `.data.automation`
  - script inventory/review -> `.data.script`
  - entity discovery -> `.data.entity`
  - `.data.entity` is only a fallback seed for automation/script derivation when the target-family arrays are absent
- Helper-in-area is not a first-class bulk selector contract. Do not imply room-owned helper discovery unless live helper-area semantics are explicitly defined.
- Never guess area inheritance from a display name.

### `label`

- Use full entity-registry data when label evidence is needed.
- `config/entity_registry/list` returns the entity array directly in `.data`; do not expect `.data.entities`.
- Match only real registry label assignments. Do not infer labels from names, areas, or helper titles.

## Discovery Pipeline

1. If the selector is `area`, resolve the area name through `config/area_registry/list` when needed, then query `search/related` with `item_type:"area"` first.
2. If the selector is not `area`, start with `config/entity_registry/list_for_display` for fast inventory and direct `domain` / `prefix` filtering.
3. Escalate to `config/entity_registry/list` only when `label` evidence or richer registry metadata is required.
4. Dedupe the resolved shortlist on canonical `entity_id` before sorting, counting, or workset trimming.
5. Save the resolved shortlist with `--out <result-file>` before any bulk review loop.
6. Keep ordering deterministic: sort by domain, then `entity_id`.

## Reference Filters

Use these jq idioms as the default starting point. Do not rewrite them into regex-heavy variants unless the selector actually needs regex behavior.

- For transient selector files, prefer one temp directory with fixed names such as `payload.json`, `filter.jq`, and `result.json`.
- Write final request payloads and jq programs directly. Do not create placeholder templates such as `REPLACE_ENTITY_ID` and patch them later.
- Do not mutate payload or jq files afterward with `perl -0pi`, `sed -i`, or similar in-place rewrite commands.
- After a shortlist or workset file is created, treat it as immutable and write later registry/config/collision outputs to dedicated filenames.
- Write transient JSON and jq files with the native file-writing flow for the current shell. The heredoc snippets in this doc are POSIX examples; on Windows/PowerShell, use the equivalent native file-writing form while keeping the same filenames and file contents.
- When the selector output is already small, let the first Relay call emit the wrapped inventory object directly instead of adding follow-up `ha-nova relay jq` passes only to recalculate counts.
- For Relay result-file inspection, prefer `ha-nova relay jq --file` over ad-hoc Node or Python parsers.
- When you need an unquoted scalar from a Relay result file, prefer `ha-nova relay jq -r --file <result-file> '<filter>'`.
- The single-quoted filter above is a POSIX example. On Windows/PowerShell pass the same filter with native argument quoting.
- After the wrapped inventory object is saved, inspect only wrapper fields such as `.matched`, `.displayed`, `.remaining`, `.values`, and `.rows` when present. Do not feed the wrapped result file back through the original shortlist jq program.
- For wrapper-field extraction, prefer separate simple field selectors or read the whole wrapper object once.
- If you need a follow-up jq file for wrapper inspection, use a dedicated filename such as `rows.jq` or `wrapper.jq`; do not reuse the original shortlist `filter.jq`.
- When later steps need candidate entity lists, persist them as files instead of embedding JSON arrays inside shell variables.
- If multiple Relay commands run in the same temp directory, keep shared filenames serial or give each concurrent probe its own dedicated payload filename.
- Avoid precedence-sensitive chained string-building filters for multiple fields.

### Prefix shortlist from `config/entity_registry/list_for_display`

Write the resolved lowercase prefix literal directly into the jq file and reuse this pattern:

```jq
[
  .data.entities[]
  | select(.ei | startswith("automation."))
  | {entity_id: .ei, name: (.en // ""), area_id: (.ai // ""), suffix: (.ei | split(".")[1])}
  | select((.suffix | ascii_downcase | startswith("morning")) or (.name | ascii_downcase | startswith("morning")))
  | del(.suffix)
]
| sort_by(.entity_id)
```

- Prefer `split(".")[1]` for the entity-id suffix over regex escaping.
- Do not add exploratory temp-file probes or alternate filename guesses after the selector succeeds.

### Area shortlist from `search/related`

For automations:

```jq
(.data.automation // []) | sort
```

- This result shape is a plain JSON array of automation `entity_id` strings.
- Keep that array shape for workset trimming and count calculations unless you intentionally map it into row objects first.

For scripts:

```jq
(.data.script // []) | sort
```

Treat `.data.entity` only as a fallback seed when the automation/script arrays are absent.

### Label shortlist from `config/entity_registry/list`

Resolve the real `label_id` first, then match that exact registry label assignment:

```jq
[
  .data[]
  | select(.entity_id | startswith("automation."))
  | select(any((.labels // [])[]; . == "label_alpha"))
  | {entity_id, name: (.name // ""), area_id: (.area_id // "")}
]
| sort_by(.entity_id)
```

- Replace `"label_alpha"` with the resolved canonical `label_id` literal before running the filter.

### Inventory summary wrapper

When the deterministic shortlist already contains row objects, derive the display payload with this shape:

```jq
. as $rows
| {
    matched: ($rows | length),
    displayed: ($rows[0:20] | length),
    remaining: (($rows | length) - ($rows[0:20] | length)),
    values: ($rows[0:20] | map(.entity_id)),
    rows: ($rows[0:20])
  }
```

When the shortlist is still a plain JSON array of `entity_id` strings, use this wrapper instead:

```jq
. as $ids
| {
    matched: ($ids | length),
    displayed: ($ids[0:20] | length),
    remaining: (($ids | length) - ($ids[0:20] | length)),
    values: ($ids[0:20])
  }
```

- If a UI table needs row objects from a plain string shortlist, normalize the ids into row objects first and then use the row-object wrapper above.

## Inventory Rules

- Bulk inventory returns a compact table, not full YAML per target.
- Show:
  - exact filter used
  - total matched count
  - displayed rows
- Display at most 20 inventory rows in one response.
- If the user wants full config detail, continue with one selected target only.

## Audit Workset Rules

- Resolved targets `== 1`: stay in normal single-target review mode.
- Resolved targets `> 1`: enter aggregate multi-target review mode automatically.
- Audit at most 5 targets in one workset.
- One standalone bulk-review request may audit exactly one workset only.
- Materialize the current audit workset before any config, state, or collision reads.
- Never read configs for matched-but-non-audited remainder targets outside the current audit workset.
- Never continue automatically into a second workset inside the same response.
- Never resolve `unique_id` values or build a config snapshot for matched-but-non-audited remainder targets outside the current audit workset.
- If collision classification needs one extra target outside the matched remainder set, keep it explicit, read-only, outside the audited-item count, and clearly marked as related/collision evidence in the transcript.
  - Prefer explicit command or file naming such as `related-config`.
  - If the justification must live in command transcript output, emit an explicit marker line such as `COLLISION_EVIDENCE=<reason>`.
- If more than 5 targets match:
  - audit the first 5 in deterministic order
  - report `matched N / audited 5 / remaining R`
  - do not prefetch configs, states, or related-item evidence for the remainder
  - make continuation explicit; do not silently continue in the background

### Continuation Rounds

- A continuation round re-runs the SAME frozen selector from round 1 — never a reworded or re-resolved variant.
- Exclude already-completed items by exact `entity_id`: the completion ledger from earlier rounds is the cursor.
- Audit the next workset from the remaining IDs in the same deterministic order.
- Any selector change (different prefix, domain, area, label, or scope) is a NEW task with a fresh shortlist — not a continuation.

## Stale Automation Audit (explicit request only)

For "which of my automations never ran / are stale": one `GET /api/states` pull filtered to `automation.*` — no per-item config reads.

- Sort by `attributes.last_triggered` ascending; `null` means never triggered — report it as "never", not as an error.
- Report as a List Frame with exact counts: total automations, never-triggered count, disabled (`state: off`) count, and the oldest entries by last trigger.
- This is read-only inventory, not review: no per-item review runs, so the 5-target audit cap does NOT apply — the whole domain is one pull.
- `last_triggered` says nothing about WHY an automation is stale; offer single-target review or `ha-nova:diagnose` as the follow-up.

## Evidence and Output Rules

- Run the same review checks per item as in single-target review.
- Aggregate findings by repeated pattern, but always keep the affected item list.
- Keep a per-item evidence table with:
  - target id
  - overall status
  - top finding or clean result
- Cluster collisions by shared controlled entity or linked helper instead of repeating the same conflict block for every target.
- Do not emit full YAML for many targets in one response.
