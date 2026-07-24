# Health Availability Context Spec

Status: merged — #436; released in v0.21.3
Date: 2026-07-24
Issue: #433

## Goal

Home Status preserves raw `unavailable` and `unknown` entity-state counts while
explaining whether they represent restored inventory, a few concentrated
integration/device clusters, or a broad current availability problem.

## Evidence

Always read:

- `/api/states`
- `config_entries/get`

When at least one state is `unavailable` or `unknown`, also attempt:

- `config/entity_registry/list`
- `config/device_registry/list`

Join only by exact internal IDs:

- state `entity_id` -> entity-registry `entity_id`
- entity-registry `config_entry_id` -> config-entry `entry_id`
- entity-registry `device_id` -> device-registry `id`

Internal IDs are join keys only and never user-visible.

## Summary Contract

- Label raw totals as entity-state counts, never device counts or problem
  counts.
- Split `unavailable` and `unknown`, then split restored
  (`attributes.restored == true`) from current/non-restored states.
- Retain low-signal/stateless domains (`button`, `event`, `scene`, `stt`) in a
  separate contextual count; never silently drop them.
- Group attributed states by config entry. Use entity-registry `platform` as
  the safe integration label; never show config-entry titles.
- Accept visible domain/platform labels only when they match
  `^[a-z0-9_]{1,128}$`; entity IDs used for joins must match the Home Assistant
  entity-ID shape. Treat invalid values as malformed source data and never echo
  them.
- Within each config-entry group, count distinct known device-registry records
  and row coverage (`X/Y entity states`). Deduplicate the overall known-record
  union. Do not claim an exact device total.
- Distinguish `loaded` from `setup_error`, `setup_retry`, `migration_error`,
  `failed_unload`, `not_loaded`, transitional states, disabled entries, and
  unknown states. Only the explicit failure set is attention. A failed entry
  plus affected entity states is one finding.
- Choose at most five attention entries by failure-state priority, safe domain,
  and hidden entry key. Independently sort eligible group details by affected
  entity-state count descending, safe integration label ascending, then the
  internal group key as a hidden tie-breaker.
- Display at most five group details across both owner sections. State omitted
  group-detail/entity-state counts and attention-entry omissions. Entities use
  group-selection order; Integrations use failure-state priority and attach
  selected joined details.
- Show how many entity states and what share the displayed top groups cover.
- State registry coverage and unattributed counts. Missing registry/device data
  yields an explicit limitation, never an inferred device/problem count.
- Build one finding ledger. Joined impact for a non-disabled failed config
  entry is rendered once under Integrations and suppressed from Entities.
- Disambiguate same-label config-entry and platform-only groups without
  exposing IDs.
- Aggregate matching device-registry IDs independently of integration
  attribution. Show only the three largest device-subcluster counts and
  coverage, never IDs or names.

## Conclusion Rules

Choose exactly one evidence-bound classification:

- Exclude rows from the classification population only when joined to a
  non-disabled config entry whose state is `setup_error`,
  `setup_retry`, `migration_error`, `failed_unload`, or `not_loaded`.
- `mostly restored or tracker-style inventory`: the union of restored states
  and explicitly listed tracker-style/low-signal domains covers at least 60%
  of the classification population.
- `concentrated integration/device clusters`: at least 80% of the
  classification population has integration or exact device-registry
  attribution, and the better-covered top-three integration/device axis covers
  at least 60%.
- `broad current availability problem`: at least 80% is attributed, at least
  60% is current, and the three largest groups cover less than 60%.
- `fully explained by integration failures`: every row was excluded by an
  exact failed-entry join.
- `insufficient registry evidence`: attribution is too incomplete for another
  classification.

Use integer cross-multiplication at the exact 60% and 80% boundaries. The
classification does not replace raw counts and never changes overall Home
Status by itself.

## Privacy

User output may contain safe integration/platform domains and aggregate counts.
It must not contain entity IDs, config-entry IDs, device IDs, config-entry
titles/account names, friendly names, addresses, hosts, URLs, tokens, or raw
exception text.

## Verification

- Contract tests pin every evidence source, join, classification, ordering,
  cap, limitation, and privacy rule.
- Synthetic fixtures cover a failed high-fan-out entry, a loaded entry,
  restored tracker-style inventory, low-signal domains, unattributed states,
  equal-count ordering, same-label platform-only groups, missing metadata,
  partial/missing device-registry coverage, exact 60%/80% boundaries, and more
  than five groups, more than three device clusters, and more than five
  attention entries whose state priority conflicts with group size.
- Mixed loaded/failed fixtures assert exactly one shared five-group ledger.
  Boundary fixtures cover both sides of every 60% and 80% threshold. Registry
  source failures are tested separately.
- Synthetic output assertions reject every fixture entity ID, internal ID,
  account title, host, address, and token.
