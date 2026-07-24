# Availability Analysis

## Config-Entry State Categories

Use one state taxonomy everywhere: finding ownership, classification exclusion,
overall status, Integration rendering, and next-step priority.

- Attention/failure: `setup_error`, `setup_retry`, `migration_error`,
  `failed_unload`, `not_loaded`.
- Transitional context: `setup_in_progress`, `unload_in_progress`.
- Healthy: `loaded`.
- Any other literal state: context with no inferred failure.
- `disabled_by` overrides every state: intentionally disabled context, never
  attention.

Choose at most five attention entries in the order above, then safe domain and
hidden entry key by Unicode code point. This Integration-entry selection is
independent from the availability group-detail cap below. Transitional,
disabled, and unknown states remain context and classification evidence; they
never own an Integration finding or change overall status.

## Entity-State Analysis

Treat `unavailable` and `unknown` as entity states. Their count is never a
device count or an independent-problem count.

Build the availability evidence deterministically:

1. Select state rows whose literal `state` is `unavailable` or `unknown`.
2. Count those values separately. A row is restored only when
   `attributes.restored == true`; all other rows are current/non-restored.
3. Derive the entity domain only from the prefix before the first `.`. Keep
   `button`, `event`, `scene`, and `stt` in a separate low-signal/stateless
   subtotal. Never discard them from raw totals.
4. Join state `entity_id` to exact entity-registry `entity_id`. Use only
   `config_entry_id`, `device_id`, and `platform` from the matched row. Join
   `config_entry_id` to exact config-entry `entry_id`; join `device_id` to exact
   device-registry `id`. Before using any row, require entity IDs to match
   `^[a-z0-9_]+\.[a-z0-9_]+$` and every user-visible domain/platform to match
   `^[a-z0-9_]{1,128}$`. Otherwise mark that source malformed; never echo or
   derive a visible label from the invalid value.
5. Group by config entry when `config_entry_id` exists. Otherwise group by
   registry `platform`. Rows without either attribution stay `unattributed`;
   never invent a group from an entity name.
6. Give each group a safe base label from registry `platform`, then
   config-entry `domain`, then entity domain. When one group has several
   candidates, choose the smallest non-empty label by Unicode code point.
   Disambiguate every shared base label: config-entry groups receive localized
   generic ordinals (`entry 1`, `entry 2`, ...) assigned by internal
   `config_entry_id` code-point sort; a platform-only group receives a
   localized `no config-entry attribution` suffix. Apply the same rule when a
   platform-only group collides with one config-entry group. Never expose an ID
   or config-entry title.
7. Per group, count entity states, restored/current split, entity-state rows
   whose `device_id` exists in the device registry, and distinct matching
   device-registry IDs. Report `N known device-registry records; device
   attribution X/Y entity states`, never an exact device total. Deduplicate the
   overall matching-ID union. An unavailable device registry is a limitation,
   not zero; with an available registry, zero matches is valid evidence.
   Independently aggregate every matching device ID across all rows, including
   rows without integration attribution. Sort device subclusters by
   entity-state count descending, then hidden device ID ascending. Show only
   the three largest counts, their covered entity-state count/share, and
   omitted device-cluster/entity-state counts. Never show device IDs or names.
8. Attach config-entry state as literal evidence. Distinguish `loaded`,
   `setup_error`, `setup_retry`, `migration_error`, `not_loaded`, intentionally
   disabled (`disabled_by` set), missing entry metadata, and other states. A
   failed entry plus affected states is one finding: state is cause evidence;
   entity-state/device counts are impact.
9. Build one finding ledger shared by `Entities` and `Integrations`. An exact
   non-disabled attention/failure entry owns its joined group detail in
   `Integrations`; never repeat it in `Entities`. Transitional, disabled,
   unknown-state, loaded, and missing-entry-metadata groups stay contextual in
   `Entities`. First choose the five attention entries by the state-priority
   rule above. Group-detail candidates are every non-attention group plus the
   chosen attention entries' groups. Sort those candidates by entity-state
   count descending, unlocalized base label ascending by Unicode code point,
   then internal group key ascending as a hidden tie-breaker. Display at most
   five group details across both owners. Render contextual `Entities` details
   in group-selection order. Render chosen `Integrations` entries in the
   state-priority order above, attaching joined detail only when that group was
   selected; a chosen entry whose detail misses the shared cap still appears
   state-only. Raw totals include every row. State omitted group-detail and
   entity-state counts, plus any attention-entry omissions.
10. Report entity-registry match coverage, config-entry attribution, device
    source and row coverage, unattributed entity-state count, and count/share
    covered by the three largest and all displayed groups. `unattributed` is
    coverage only: never a displayed group or top-three numerator.

For classification only, exclude rows joined to an exact non-disabled entry in
the attention/failure set. Their cause is already known and must not make a
failed tracker integration look like benign inventory. The remaining rows are
the classification population. Keep raw totals unchanged. Use integer
cross-multiplication for thresholds; never compare rounded percentages.

Choose exactly one classification, in order:

- `mostly restored or tracker-style inventory`: at least 60% of all
  classification-population rows are restored or belong to `device_tracker`,
  `person`, `geo_location`, `button`, `event`, `scene`, or `stt`. Count the
  union once.
- `concentrated integration/device clusters`: at least 80% of the
  classification population has integration or exact device-registry
  attribution, and the better-covered axis among the three largest integration
  groups or device subclusters covers at least 60% of that population.
- `broad current availability problem`: at least 80% has attribution, at least
  60% is current/non-restored, and the three largest groups cover less than
  60%.
- `fully explained by integration failures`: the population is empty because
  every raw row was excluded by an exact attention/failure entry join.
- `insufficient registry evidence`: otherwise fails the rules above.

Classification is context, not another finding. Availability classification
alone never changes overall Home Status.

Output is aggregate-only. Never expose entity IDs, config-entry IDs, device
IDs, config-entry titles/account names, friendly names, addresses, hosts,
URLs, secret values, translation keys, or raw exception text.
Render unrecognized config-entry state strings as a localized generic unknown
state; never echo the raw value.

When availability rows exist, both registry sources are conditionally required
for full Home Status coverage. A missing or malformed entity or device registry
makes overall status `limited`; name each registry separately in Coverage.
