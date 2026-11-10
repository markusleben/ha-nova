# Upstream Drift Log

Status: active

Append-only record of upstream-release screenings against skill-pinned
surfaces (rule: AGENTS.md → Releases → upstream drift check). Every run is
logged — hits AND clean passes — so the next run knows where the last one
stopped. A full screen's heading says "pre-release screen" or "deep-audit
screen" (`## YYYY-MM-DD — pre-release screen …`); `npm run verify:drift-log`
counts only those, not ad hoc checks or corrections.

## 2026-08-05 — HA 2026.7 + HACS 2.0.5 (initial entry)

- Window screened: HA 2026.7 release post (2026-07-01) + frontend PRs
  #52215/#52353/#52530; HACS releases through 2.0.5.
- HIT — HA 2026.7 "Update all": frontend-only per-group button; ONE
  multi-target `update.install` (entity_id list, no new API), concurrent,
  sets NO backup flag, first-failure-only reporting, never includes
  core/OS/Supervisor; HACS update entities participate. Modeled in
  `skills/updates/SKILL.md` (mirror the guardrails, deliberately not the
  call shape) via PR #506.
- CLEAN — HACS 2.0.5 WS command surface matches the pinned map added in
  PR #506 (`skills/hacs/hacs-commands.md`, landing with the same train;
  source-verified 2026-08-04 and runtime-verified read-only against the
  reference instance).
- NOT SCREENED — the HA developer-blog breaking-change check for 2026.7
  did not run in this pass; it carries into the next window.
- Next window starts WITH HA 2026.8 AND includes the carried-over 2026.7
  developer-blog backlog (everything after the 2026.7 release post and
  HACS 2.0.5 is unscreened).

## 2026-08-09 — HA 2026.8 + carried-over 2026.7 dev-blog backlog (HACS unchanged)

- Window screened: HA 2026.8 release post (2026-08-05) + backward-incompatible
  list; HA dev-blog 2026-06-15 through 2026-07-31 (the 2026.7 backlog carried
  from the 2026-08-05 entry); HACS releases (none since 2.0.5, GitHub-verified);
  cross-checked against core source @ tag 2026.8.0 and 11 read-only probes on a
  live HA 2026.8.0 / HACS 2.0.5 / Relay 0.9.0 instance. Run as charter C2 of
  the 2026-08-09 skill audit (`docs/work/2026-08-09-skill-audit.md`).
- HIT — dev-blog 2026-07-21 device-registry single-config-entry (Core 2026.8):
  devices now own exactly ONE config entry; `config_entries`/
  `config_entries_subentries`/`primary_config_entry` are compat shims until
  2027.8 (WS rows still carry both old and new fields — live-verified); WS
  `config/device_registry/remove_config_entry` survives but removing the owning
  entry now REMOVES THE DEVICE. `skills/fallback/SKILL.md` detach section still
  frames it as severing one of several relationships (audit C2-01). No skill
  reads the deprecated fields (grep-clean). New WS commands
  `config/device_registry/list_composite_splits` + `list_linked_devices`
  exist; composite-split aftermath is unmodeled anywhere (issue #520).
- HIT — dev-blog 2026-07-03 media-source search: WS `media_source/search_media`
  (`search_query`, optional `media_content_id`, `media_filter_classes`;
  `can_search` flag) ships in 2026.8; `skills/media/SKILL.md` covers only
  `media_player/search_media` + browsing (audit C2-02, issue #519).
- HIT — 2026.8 "Developer Tools" → "Tools" rename: single stale UI pointer at
  `skills/ha-nova/best-practices.md:128` (audit C2-03).
- HIT — 2026.8 energy: `BatterySourceType` gained optional `capacity` (kWh,
  weights combined SoC); missing from the optional-field list in
  `skills/energy/energy-reference.md:49` (audit C2-05); grid schema and KPI
  formulas unaffected (source- and pin-verified).
- CLEAN — vacuum `battery_level` removal (8 integrations): health's low-battery
  detector reads `device_class: battery` sensor entities — exactly the
  replacement surface; it never read the vacuum attribute; no fixture pins it.
- CLEAN — `search/related` on 2026.8 source: item types still include NO
  `zone` type (`skills/admin/SKILL.md:38` holds); dashboards/templates still
  not indexed (organize/safe-refactoring/consumer-discovery claims hold);
  live entity probe returned area/floor/device/config_entry/integration keys,
  consistent with relay-api.md's open-ended keyed-object contract.
- CLEAN — 2026.8 ":8123-less" addresses: new HA OS installs only; repo's
  `:8123` mentions are Container-install defaults (`docs/reference/
  relay-container.md`, `bridge-architecture.md`) where 8123 remains correct.
- CLEAN — 2026.8 entity-ID rename UX: frontend-only; registry API
  (`config/entity_registry/update` + `new_entity_id`) unchanged; organize/
  safe-refactoring rename flows unaffected.
- CLEAN — dev-blog 2026-06-15 device-tracker changes: deprecations
  (`battery_level`, `location_name` on trackers) land 2027.7; new `in_zones`
  attribute is additive; no skill pins either; admin zone registry surface
  untouched. Re-screen before 2027.7.
- CLEAN — dev-blog 2026-07-22 standard button event types: additive
  `ButtonEventType` constants on event entities; nothing deprecated;
  relay-api's `event_types`-attribute guidance and best-practices Zigbee
  patterns unaffected.
- CLEAN — dev-blogs 2026-06-23/06-30 (frontend components, unit enumerators,
  async_initialize_triggers), 2026-07-05 (Modbus), 2026-07-20 (AI policy),
  2026-07-31 (frontend components): no skill-pinned WS/REST surface touched.
- CLEAN — remaining 2026.8 BI items (AirNow, Gardena BT, Edifier, Ohme,
  Paperless-ngx, ScreenLogic, UniFi Protect, Volvo On Call): integration-
  specific; no skill pins them.
- CLEAN — live shape probes @ 2026.8.0: `/api/error_log` still 404 on HA OS
  (diagnose/matrix claim verified); `list_for_display` rows
  (ai/di/dp/ec/ei/en/hb/hn/ic/lb/pl/tk) match ei/en/ai pins; `repairs/
  list_issues` → `.data.issues[]` matches health pins; `recorder/
  list_statistic_ids` fields match maintenance metadata pins (incl.
  `unit_class`, `mean_type`); `recorder/validate_statistics` shape matches the
  issue matrix; `hacs/info` → 2.0.5 matches the pinned map;
  `system_health/info` initial/update/finish events per health pins (initial
  nests `.data.<domain>.info` — audit C2-06 fixes the example path).
- CLEAN — version floors: relay 0.9.0 live == `version.json`
  `min_relay_version`; `recorder/update_statistics_metadata` `unit_class`
  "breaks in 2026.11" claim verified against dev-blog 2025-10-16; legacy
  template removal in 2026.6 verified shipped (R-25 modeling correct;
  matrix:181 tense-stale — audit C2-04).
- Sources: home-assistant.io/blog/2026/08/05/release-20268/;
  developers.home-assistant.io/blog/2026/07/21/device-registry-single-config-entry,
  /2026/07/03/media-source-search, /2026/06/15/device-tracker-changes,
  /2026/07/22/button-standard-event-types, /2025/10/16/recorder-statistics-api-changes;
  home-assistant.io/blog/2026/06/03/release-20266/; core @ 2026.8.0
  (search, config/device_registry, helpers/device_registry, media_source/http,
  energy/data); github.com/hacs/integration/releases.
- Next window starts AFTER HA 2026.8 + HACS 2.0.5; watch items: device
  registry compat shims harden 2027.8; device-tracker deprecations land
  2027.7; `unit_class` omission breaks 2026.11.

## 2026-08-12 — HA 2026.8.1 patch + developer blog + HACS

- Window screened: Home Assistant Core 2026.8.1, developer-blog posts after
  the 2026-08-09 screening, and HACS releases after 2.0.5.
- CLEAN — Core 2026.8.1 changes no pinned WS response or request shape. The
  REST service-call fix in core PR #178377 retains the shielded task to prevent
  mid-run garbage collection; it does not change the `/api/services/*` contract.
- CLEAN — no new developer-blog post after the July 22 entry and no HACS
  release after 2.0.5.
- Existing HIT remains tracked: `media_source/search_media` is issue #519.
- Sources: github.com/home-assistant/core/releases/tag/2026.8.1;
  github.com/home-assistant/core/pull/178377;
  developers.home-assistant.io/blog/; github.com/hacs/integration/releases.
- Next window starts AFTER HA 2026.8.1 + HACS 2.0.5.

## 2026-08-20 — verification pass (issue triage session)

- RESOLVED — the 2026-07-03 `media_source/search_media` HIT: `skills/media/SKILL.md`
  has carried both `media_player/search_media` and `media_source/search_media`
  with `can_search` gating since the #541 audit train; the "#519 tracks it"
  note above is closed by verification, no skill delta was needed.
- Scope note: this was a targeted verification of one stale entry, not a full
  upstream screen; the last full screen remains the 2026-08-12 entry above.

## 2026-08-27 — pre-release screen (v0.25.0 cycle): HA 2026.8.2/2026.8.3 + developer blog + HACS

- Window screened: Core 2026.8.2 (2026-08-14) and 2026.8.3 (2026-08-21),
  developer-blog posts after 2026-07-22, HACS releases after 2.0.5.
- CLEAN — 2026.8.2 and 2026.8.3 are integration fixes and dependency bumps
  only; no pinned WS/REST contract change.
- HIT (minor, doc accuracy) — dev-blog 2026-08-19 "Device Registry WebSocket
  API Changes": `config/device_registry/remove_config_entry` is DEPRECATED
  (successor `config/device_registry/remove`; old command supported until
  ~2027.8). We pin it only as EXTERNAL documentation in
  `skills/fallback/SKILL.md` (HA NOVA never calls it). The additive device
  fields (`config_entry_id`/`config_subentry_id`) are already the preferred
  read in `skills/fallback/relay-ready.md` with the `config_entries`
  fallback — no delta needed there. Fix: one-sentence deprecation note in
  fallback SKILL.md, folded into the v0.25.0 release-prep PR.
- CLEAN — dev-blog 2026-08-24 "More Device Registry Deprecations" targets
  custom-integration Python internals (`via_device`, direct registry
  access), not the WS client contract.
- CLEAN — no HACS release after 2.0.5.
- Note: 2026.9.0b0 (2026-08-26) exists; the stable 2026.9 screen is the next
  window.
- Sources: github.com/home-assistant/core/releases (2026.8.2, 2026.8.3);
  developers.home-assistant.io/blog/ (2026-08-19, 2026-08-24);
  github.com/hacs/integration/releases.
- Next window starts AFTER HA 2026.8.3 + dev-blog 2026-08-24 + HACS 2.0.5.

## 2026-09-10 — deep-audit screen: HA 2026.9.0/2026.9.1 + developer blog + HACS

- Window screened: Home Assistant Core 2026.9.0 and 2026.9.1, developer-blog
  posts after 2026-08-24 through 2026-09-07, and HACS releases after 2.0.5.
- HIT — Core 2026.9 child devices are returned by
  `config/device_registry/list` with a reduced shape and `parent_device_id`;
  their null `area_id` inherits the parent area. Current health/capability
  normalization drops `parent_device_id`, and fallback's device-only event
  resolution still reads the omitted legacy `config_entries` array. Join the
  existing device-registry capability issue #520; add parent/child fixtures.
- HIT — the new admin-only requirement for `update.install`, `update.skip`, and
  `update.clear_skipped` needs permission-specific guidance for standalone
  relays using a non-admin LLAT. App/Supervisor system context is unaffected.
- HIT — the current device-registry matrix and maintenance reference omit the
  new admin-only `config/device_registry/remove` command; the legacy
  `remove_config_entry` command remains deprecated until 2027.9. Keep direct
  removal external unless product scope changes; correct the factual reference.
- HIT — Home Assistant's templating documentation was split into
  `source/_docs/templating*`; the repository's pinned raw source now returns
  HTTP 404. Resolution: the vendored snapshot and its generator are retired
  (branch `perf/context-reduction`); reviews fetch the official templating
  page only when the reviewed config contains Jinja.
- CORRECTION of the 2026-08-27 entry: the legacy
  `config/device_registry/remove_config_entry` command is removed in 2027.9
  (not ~2027.8); 2027.8 is the removal date of the legacy device fields
  `config_entries`/`config_entries_subentries`/`primary_config_entry`.
- Landed: the 2026.9 child-device shape (`parent_device_id`, reduced row,
  singular `config_entry_id`, inherited area) is modeled once in
  `skills/ha-nova/membership-resolution.md` → Device rows and pointed at from
  health, capability-answer, availability-analysis, and fallback; the
  admin-only update actions are in `skills/updates/SKILL.md`; the matrix and
  maintenance reference carry `remove`, `list_linked_devices`, and
  `list_composite_splits` (branch `fix/ha-2026-9-skill-drift`, #520).
- CLEAN — persistent-notification event semantics, LLM tool-name prefixes,
  Configurator deprecation, Modbus Python helper changes, OAuth2 helper
  exceptions, and additive selector types do not alter a pinned Relay/skill
  contract.
- CLEAN — Core 2026.9.1 fixes no additional pinned request/response surface.
- CLEAN — no HACS release after 2.0.5.
- Sources: home-assistant.io/blog/2026/09/02/release-20269/;
  developers.home-assistant.io/blog/2026/08/19/device-registry-websocket-api-changes/;
  developers.home-assistant.io/blog/2026/08/31/deprecate-configurator/;
  developers.home-assistant.io/blog/2026/09/02/modbus-get-hub-deprecation/;
  developers.home-assistant.io/blog/2026/09/04/device-and-state-class-selectors/;
  developers.home-assistant.io/blog/2026/09/07/oauth2-central-error-handling/;
  github.com/hacs/integration/releases.
- Next window starts AFTER HA 2026.9.1 + dev-blog 2026-09-07 + HACS 2.0.5;
  watch the legacy device-registry removal command for 2027.9.

## 2026-10-15 — pre-release screen (v0.26.0 cycle): HA 2026.9.2–2026.10.1 + developer blog + HACS

- Window screened: Core 2026.9.2 (2026-09-11), 2026.9.3 (2026-09-18),
  2026.9.4 (2026-09-27), 2026.10.0 (2026-10-07), 2026.10.1 (2026-10-10);
  developer-blog posts after 2026-09-07 through 2026-10-12 (no later post);
  HACS releases after 2.0.5.
- HIT — 2026.10 MQTT: `mqtt.publish` and `mqtt.dump` now require an
  administrator account, like `mqtt.reload` (core #182045). The App's
  Supervisor system context is unaffected; a standalone relay with a
  non-admin `HA_LLAT` now fails on publish. Landed: admin-only line in
  `skills/mqtt/SKILL.md` → Error Handling (word budget allows one line),
  mirroring the 2026.9 note in `skills/updates/SKILL.md` (release-prep PR
  `chore/release-0.26.0`).
- HIT (minor) — 2026.10 State conditions: combining `for` with a state
  attribute, a list of states, or a state taken from another entity now
  fails validation (core #174083). No skill example used the combination.
  Landed: one-line constraint under "State Condition with Attribute" in
  `skills/ha-nova/automation-patterns.md` (same PR). The live
  `/conditions/state/` doc page still says lists work with `for`; the
  release note and PR win.
- HIT (P3, deferred) — dev-blog 2026-09-11: lawn mowers gain `stop` and
  activity `idle` ("stopped, but neither docked nor paused") in 2026.10.
  `skills/entity-discovery/SKILL.md` "is everything OFF" lists `lawn_mower`
  only in `cleaning`/`mowing`/`returning`/`paused`/`error`; an `idle` mower
  in the yard reads as put away. Optional one-word fix, not in this train.
- CLEAN — 2026.10 trigger-ID editor change maps to the existing trigger
  condition; `condition: trigger` + `id:` guidance stays valid.
- CLEAN — dev-blog 2026-09-15 `DeviceEntry.config_entries` enforcement
  concerns Python properties only; WS `config/device_registry/list` keeps
  `config_entries`/`config_entries_subentries`/`primary_config_entry`.
  2026.10.1 #185361 keeps the deprecated attributes working in templates.
- CLEAN — 2026.10.1 #185161: automations without an ID no longer store
  traces (previously under `automation.None`); matches `trace/list`/`trace/get`
  keyed by `item_id` = unique_id.
- CLEAN — 2026.10 authentication legacy-mode removal: `config/auth/list`
  shape unchanged; the Relay uses LLAT/Supervisor context.
- CLEAN — Synology DSM reboot/shutdown admin-only, Telegram `download_file`
  allowlist, Google Sheets empty-range shape, integration removals/sensor
  renames: no pinned surface.
- CLEAN — dev-blog 2026-09-17, 2026-09-26 (LLM `ToolResult`), 2026-09-28,
  2026-09-30 (Probatio error wording; no skill matches voluptuous text),
  2026-10-06, 2026-10-09, 2026-10-12: integration-internal or additive.
- CLEAN — 2026.9.2/2026.9.3/2026.9.4: integration fixes and dependency
  bumps; no pinned WS/REST contract change.
- CLEAN — no HACS release after 2.0.5.
- Watch (2026.11): `temperature_unit` state attribute on
  climate/water_heater (additive); `entity_id` registry renames update the
  entity in place (old state removed, rewritten under the new ID).
- Sources: home-assistant.io/blog/2026/10/07/release-202610/;
  github.com/home-assistant/core/releases (2026.9.2, 2026.9.3, 2026.9.4,
  2026.10.1); github.com/home-assistant/core/pull/174083;
  github.com/home-assistant/core/pull/182045;
  github.com/home-assistant/core/pull/185161;
  developers.home-assistant.io/blog/2026/09/11/lawn-mower-stop-action-idle-activity;
  developers.home-assistant.io/blog/2026/09/15/device-entry-config-entries-deprecation;
  developers.home-assistant.io/blog/2026/09/17/Use-config-entry-exc-in-migration;
  developers.home-assistant.io/blog/2026/09/26/llm-tool-result;
  developers.home-assistant.io/blog/2026/09/28/central-config-flow-abort-reasons;
  developers.home-assistant.io/blog/2026/09/30/probatio-validation-engine;
  developers.home-assistant.io/blog/2026/10/06/restore-state-last-states-removal;
  developers.home-assistant.io/blog/2026/10/08/climate-native-temperature;
  developers.home-assistant.io/blog/2026/10/08/water-heater-native-temperature;
  developers.home-assistant.io/blog/2026/10/09/uom-selector;
  developers.home-assistant.io/blog/2026/10/12/new-entity-lifecycle-hooks;
  github.com/hacs/integration/releases.
- Next window starts AFTER HA 2026.10.1 + dev-blog 2026-10-12 + HACS 2.0.5;
  watch the 2026.11 items above, the deferred lawn-mower `idle` bucket, and
  the legacy device-registry removal command for 2027.9.

## 2026-10-16 — ad hoc check (device-sibling access guard): target expansion

- Scope: Home Assistant's own area/device target expansion
  (`homeassistant/helpers/target.py` `async_extract_referenced_entity_ids`)
  against the CLI's `--area`/`--device` and stored-target expansion.
- HIT — since 2026.9.0 a device target also covers its direct child devices
  (`_add_referenced_device`, core #178666; present in 2026.9.0, 2026.10.0,
  2026.10.1). The CLI matched the exact device only, so a stored press on a
  parent device could miss a child's gate button. Landed: device targets
  expand to direct children via `config/device_registry/list`
  `via_device_id` (`fix/device-sibling-access-guard`).
- CLEAN (now matched) — indirect expansion skips hidden entities and, with
  the default `primary_entities_only`, entities with an entity category;
  area expansion through a device skips entities assigned to another area.
- Sources: github.com/home-assistant/core/blob/dev/homeassistant/helpers/target.py;
  github.com/home-assistant/core/pull/178666; tags 2026.9.0, 2026.10.0,
  2026.10.1.

## 2026-10-16 — correction: child device rows

- The child-device expansion landed with `fix/device-sibling-access-guard`
  read `via_device_id`, which is the hub link of a regular device. Home
  Assistant 2026.9+ child devices are separate `ChildDeviceEntry` rows that
  carry `parent_device_id` (`helpers/device_registry.py`
  `async_entries_for_parent_device`, tag 2026.10.1; contract in
  `skills/ha-nova/membership-resolution.md` → Device rows). Fixed in
  `fix/child-device-parent-field`.
- HIT — since 2026.8 a device shared by several integrations is split into
  one device per config entry; the old (composite) id is no longer listed,
  but targets naming it still reach every split device and its children
  (`helpers/target.py` `_resolve_referenced_devices`,
  `config/device_registry/list_composite_splits`). Landed in the same branch.
- Note — Shelly multi-channel and ESPHome sub-devices link with
  `via_device_id`, not `parent_device_id`; the sibling check does not follow
  hub links (every Z-Wave/Zigbee node links to its controller).

## 2026-10-17 — pre-release screen (v0.26.0-rc2 cycle): HA 2026.10.2 + developer blog + HACS

- Window screened: Core 2026.10.2 (2026-10-16); 2026.10 release post
  re-read (backward-incompatible list unchanged since the 2026-10-15 entry;
  patch sections 2026.10.1/2026.10.2); developer-blog posts after 2026-10-12
  through 2026-10-16, plus 2026-10-10 (listed now, missing from the
  2026-10-15 entry); HACS releases after 2.0.5.
- HIT (P2) — 2026.10.2 #186165 "Allow a list of states for state
  conditions with a duration" partly reverts #174083: `for` + a state list
  validates again (the entity must stay in one of the listed states for the
  whole duration; switching between listed states restarts it). `for` +
  `attribute` and `for` + a state from another entity stay rejected.
  `skills/ha-nova/automation-patterns.md:133` still forbids the list case
  for all of 2026.10. Fix: narrow the line to attribute/other-entity and
  note the list case fails on 2026.10.0–2026.10.1 only.
- CLEAN — 2026.10.2 #186558 (removing a user revokes its refresh tokens and
  closes its WebSocket connections): `config/auth/delete` request/response
  unchanged; admin skill's preview flow holds.
- CLEAN — 2026.10.2 #186398 guards owner accounts and existing credentials in
  `config/auth_provider/homeassistant/create`; no skill or
  `cli/relay_raw_allow.go` entry pins that command (`config/auth/create`
  stays credential-free).
- CLEAN — 2026.10.2 MQTT device triggers/tags firing twice after reload
  (#185998), Alexa re-link handling on cloud entity registry updates
  (#185894; no cloud domain or WS type change), frontend 20260930.4, and the
  remaining integration fixes/dependency bumps: no pinned WS/REST contract
  change. Cloud → Link rename stays UI-only (handled in #722).
- CLEAN — `cli/relay_raw_allow.go`: no listed WS type renamed or removed in
  2026.10.2 (2026.10.1 ZHA change is the reconfigure subscription, not
  `zha/devices`).
- CLEAN — dev-blog 2026-10-10 "Update entities can customize update
  availability" (Core 2026.11): new `has_update` entity property; no wire
  change. `skills/updates/SKILL.md` already reads pending from
  `state: "on"`, never from comparing versions.
- CLEAN — dev-blog 2026-10-13 "DeviceInfo is now a dataclass" (Core 2026.11):
  Python integration API only; `via_device` removal 2027.8, other legacy
  `DeviceInfo` keys 2027.9; WS `config/device_registry/*` not described as
  changed.
- CLEAN — dev-blog 2026-10-16 "A new home for every contributor": developer
  docs, design and data portals merge into contributors.home-assistant.io;
  old URLs redirect (developers.home-assistant.io/blog/ → 301). No skill links
  the old domains (grep-clean); future screens use the new blog URL.
- CLEAN — no HACS release after 2.0.5.
- Sources: github.com/home-assistant/core/releases/tag/2026.10.2;
  github.com/home-assistant/core/pull/186165;
  github.com/home-assistant/core/pull/186398;
  github.com/home-assistant/core/pull/186558;
  github.com/home-assistant/core/pull/185894;
  home-assistant.io/blog/2026/10/07/release-202610/;
  contributors.home-assistant.io/blog/2026/10/10/update-entity-update-availability/;
  contributors.home-assistant.io/blog/2026/10/13/device-info-class/;
  contributors.home-assistant.io/blog/2026/10/16/a-new-home-for-every-contributor/;
  github.com/hacs/integration/releases.
- Next window starts AFTER HA 2026.10.2 + dev-blog 2026-10-16 + HACS 2.0.5;
  watch the 2026.11 items (`temperature_unit`, in-place `entity_id` renames,
  `has_update`, `DeviceInfo` dataclass), the deferred lawn-mower `idle`
  bucket, and the legacy device-registry removal command for 2027.9.

## 2026-10-22 — pre-release screen (v0.27.2 cycle): HA after 2026.10.2 + developer blog 2026-10-17–2026-10-22 + HACS

- Window screened: Core releases and tags after 2026.10.2 (none; latest
  release and tag is 2026.10.2 of 2026-10-16; no 2026.11 beta or `2026.11.0b*`
  tag yet); contributors.home-assistant.io/blog posts after 2026-10-16
  through 2026-10-22 (2026-10-17, -19, -20, -22); HACS releases and tags
  after 2.0.5 (none).
- HIT (P2, not an upstream change in this window; surfaced by the
  2026-10-22 post) — dev-blog "Introducing child devices" says targeting a
  parent device in an action also targets the entities of its child devices.
  This is the 2026.9 behavior from core #178666; the CLI already does this
  (`cli/tier_device.go`, via `parent_device_id`).
  `skills/ha-nova/membership-resolution.md` → Devices row still says "the
  entity registry filtered by `device_id` ONLY", so a skill-built preview of
  a parent-device action can leave out the child devices' entities. Issue
  #833.
- CLEAN — same post, other points: area inheritance (child with null
  `area_id` takes the parent's) matches Device rows; labels not inherited
  (no skill claims they are); parent delete/disable cascades to children,
  child cannot have children, no reparenting. All are Python integration
  APIs. No `config/device_registry/*` WS or REST change described.
- CLEAN — dev-blog 2026-10-17 "A Python console inside Home Assistant"
  (Core 2026.11): new `debug_console:` integration, YAML-only, admin-only
  panel. No WS command named; no skill references it, and
  `cli/relay_raw_allow.go` has no entry for it. Never add one.
- CLEAN — dev-blog 2026-10-19 "Auth providers now opt in to multi-factor
  authentication" (Core 2026.11): `AuthProvider.support_mfa` removed; custom
  auth providers only. The admin skill pins `config/auth/list|create|delete`
  and `auth/current_user`, none affected.
- CLEAN — dev-blog 2026-10-20 "Integrations have categories now": new
  manifest key `categories`. `manifest/list` may gain an additive field
  (inferred; the post names no WS change). Skills join by `domain` and read
  the display name only.
- CLEAN — no Core release after 2026.10.2; no 2026.11 beta yet.
- CLEAN — no HACS release after 2.0.5.
- Sources: github.com/home-assistant/core/releases (and tags);
  contributors.home-assistant.io/blog/2026/10/17/debug-console/;
  contributors.home-assistant.io/blog/2026/10/19/auth-providers-opt-in-mfa/;
  contributors.home-assistant.io/blog/2026/10/20/integration-categories/;
  contributors.home-assistant.io/blog/2026/10/22/child-devices/;
  github.com/home-assistant/core/pull/178666;
  github.com/hacs/integration/releases (and tags).
- Next window starts AFTER HA 2026.10.2 + dev-blog 2026-10-22 + HACS 2.0.5.
  Screen the 2026.11 beta notes when they appear; watch `temperature_unit`,
  in-place `entity_id` renames, `has_update`, the `DeviceInfo` dataclass,
  `support_mfa` removal, `debug_console`, whether `manifest/list` exposes
  `categories`, the deferred lawn-mower `idle` bucket, and the legacy
  device-registry removal command for 2027.9.

## 2026-10-24 — pre-release screen (v0.27.3 cycle): HA 2026.10.3 + developer blog 2026-10-21, 2026-10-23–2026-10-24 + HACS

- Window screened: Core 2026.10.3 (2026-10-23; full 2026.10.2...2026.10.3
  compare, 32 commits); Core tags after 2026.10.3 (none; no `2026.11.0b*`
  tag yet); contributors.home-assistant.io/blog posts 2026-10-23 and
  2026-10-24, plus 2026-10-21 (listed now, missing from the 2026-10-22
  entry); HACS releases and tags after 2.0.5 (none).
- CLEAN — 2026.10.3: fixes and dependency bumps in single integrations,
  Python 3.14.8, base image 2026.10.1, frontend 20260930.5 (UI fixes
  only). No change under
  `websocket_api`, `config`, `api`, `http`, `auth`, or a registry; no
  skill or `cli/relay_raw_allow.go` entry names a touched integration.
- CLEAN — dev-blog 2026-10-24 "Two-way audio for camera entities" (Core
  2026.11): new `camera_entity_capabilities` with `two_way_audio`, WebRTC
  only, behind a Labs preview. Additive. `skills/camera/SKILL.md` reads
  `supported_features`/`frontend_stream_type` and pins `camera/stream`
  (HLS); neither changes.
- CLEAN — dev-blog 2026-10-23 "Config entry functions now return None"
  (Core 2026.11): Python integration API only. A custom integration's
  `False` now ends in the same entry states (`setup_error`,
  `failed_unload`, `migration_error`) with a reason added; skills read
  `state`, so no change.
- CLEAN — dev-blog 2026-10-23 "Always pass a step_id to flow results"
  (core now, custom integrations by 2027.5.0): flow results always carry
  `step_id`; the REST/WS flow shape is unchanged. `skills/integration-setup`
  branches on `type` and the live `data_schema`, never on a missing
  `step_id`.
- CLEAN, watch (P3 when 2026.11 ships) — dev-blog 2026-10-21 "Show form
  fields based on other fields" (Core 2026.11): config/options flow fields
  and blueprint inputs/sections take a `visible` condition; a hidden field
  is not required and its value is not saved (a hidden blueprint input
  gets its default or None). Flow submissions and `blueprint/substitute`
  stay compatible. Gap: `skills/integration-setup/SKILL.md` builds "the
  full step body" from the live `data_schema` and does not know `visible`,
  so on 2026.11 an agent may ask for a hidden field or preview a value that
  is then dropped. Check the serialized `data_schema` key on the first
  2026.11 beta before changing skill text.
- CLEAN — no Core release after 2026.10.3; no 2026.11 beta yet.
- CLEAN — no HACS release after 2.0.5.
- Sources: github.com/home-assistant/core/releases/tag/2026.10.3;
  github.com/home-assistant/core/compare/2026.10.2...2026.10.3;
  github.com/home-assistant/frontend/releases/tag/20260930.5;
  contributors.home-assistant.io/blog/2026/10/21/form-fields-visible/;
  contributors.home-assistant.io/blog/2026/10/23/config-entry-methods-return-none/;
  contributors.home-assistant.io/blog/2026/10/23/flow-results-require-step-id/;
  contributors.home-assistant.io/blog/2026/10/24/camera-two-way-audio/;
  github.com/hacs/integration/releases (and tags).
- Next window starts AFTER HA 2026.10.3 + dev-blog 2026-10-24 + HACS 2.0.5.
  Screen the 2026.11 beta notes when they appear; watch `temperature_unit`,
  in-place `entity_id` renames, `has_update`, the `DeviceInfo` dataclass,
  `support_mfa` removal, `debug_console`, whether `manifest/list` exposes
  `categories`, the flow/blueprint `visible` key in serialized
  `data_schema`, camera two-way audio, the deferred lawn-mower `idle`
  bucket, and the legacy device-registry removal command for 2027.9.

## 2026-11-03 — ad hoc check (HA 2026.11.0b4): HACS becomes the built-in Marketplace

- Window screened: the Marketplace only, one day before 2026.11 stable.
  Core tag `2026.11.0b4` (`homeassistant/components/marketplace/` with its
  `websocket/`, `update.py`, `migration.py`; `loader.py`
  `BLOCKED_CUSTOM_INTEGRATIONS`; `bootstrap.py` `DEFAULT_INTEGRATIONS`), the
  Marketplace docs, and a live probe on throwaway 2026.11.0b4 and 2026.10.1
  containers (never a real home). The rest of the 2026.11 beta notes is NOT
  screened here and carries into the next window.
- HIT (fixed in fix/hacs-marketplace) — the `hacs` custom integration is
  blocked (`replaced_by="marketplace"`) and removed; its config entry and
  update entities migrate to domain/platform `marketplace` (same entity
  IDs). Every `hacs/*` command answers `unknown_command`; the `marketplace`
  integration is set up on every install. The HACS skill then found no
  `hacs` entry and gave install guidance, which is false (HACS cannot be
  installed on 2026.11+). Now the gate falls through to `marketplace/info`
  (result; `not_loaded` whenever no entry is loaded: disabled, still starting,
  `setup_retry`/`setup_error`, or deleted; `unknown_command` during startup)
  and `skills/hacs` reads the Marketplace inventory only
  (`marketplace/info` and `marketplace/repositories/list` raw-allowlisted
  as reads); every write is handed to Home Assistant's UI with its steps.
- HIT (fixed, same branch) — `update.install` on a migrated update entity
  (platform `marketplace`) planned at tier preview ("Reply yes"); the tier
  engine now types it like platform `hacs` (`HACS_INSTALL`), and
  `skills/updates` no longer plans Marketplace installs.
- OPEN (parity, follow-up issue) — Marketplace writes through HA NOVA:
  `marketplace/repository/install|uninstall|version|beta|refresh|ignore`,
  `marketplace/repositories/add|remove|detect`, `marketplace/critical/*`,
  `marketplace/repositories/removed`, `marketplace/repository/info`/
  `releases`, the per-user `warning/accept` precondition (installs and
  `update.install` fail with `warning_not_accepted` until the Relay's own
  user accepted it), and consumer discovery before a Marketplace uninstall.
- WATCH — `integration_entities('hacs')` is empty (use `'marketplace'`);
  `release_summary` stays empty (restart via a repair); card resources move
  to `/local/community/` (`/hacsfiles/` keeps working; the raw guard already
  allows `/local/`); AppDaemon apps and Python scripts are no longer managed.
- Sources: github.com/home-assistant/core/tree/2026.11.0b4/homeassistant/components/marketplace;
  www.hacs.xyz/docs/marketplace/;
  contributors.home-assistant.io/marketplace/how-it-works/;
  next.home-assistant.io/integrations/marketplace/.
- Next window starts AFTER HA 2026.10.3 + dev-blog 2026-10-24 (unchanged by
  this ad hoc check): screen the 2026.11.0 release post and the remaining
  beta items (entity naming, exclude targets, sun/calendar offset sign, flow
  `visible`) plus the 2026-10-24 watch list.

## 2026-11-05 — pre-release screen (v0.28.1 cycle): HA 2026.11.0 + developer blog 2026-10-25–2026-11-01 + HACS

- Window screened: the 2026.11 release post (2026-11-04) with its whole
  backward-incompatible list and the feature sections that touch pinned
  surfaces; Core `2026.11.0` (2026-11-04) against `2026.10.3` for
  `websocket_api`, `config/*` registries, `helpers/entity_registry`,
  `helpers/device_registry`, `helpers/target`, `data_entry_flow`,
  automation/script, sun/calendar triggers, update, climate, lawn_mower,
  logbook, repairs, backup and `auth/indieauth`; the Marketplace from
  `2026.11.0b4` to `2026.11.0`; contributors.home-assistant.io/blog posts
  2026-10-25 through 2026-11-01; HACS releases and tags after 2.0.5 (none).
  Live checks, never a real home: the rc lane for v0.28.1-rc2 against a
  test bed on `homeassistant/home-assistant:2026.11.0`, the disposable-HA
  E2E on `stable` (= 2026.11.0) for 4da8d7dd, and a naming probe on that
  test bed.
- HIT (P2) — entity names (core #184340, #184341): an entity's `name` is
  now its own part, and Home Assistant puts the device name in front of it
  everywhere, also for names set by a user. Stored names lose their device
  prefix in a one-time migration; where the full name changes, the old one
  stays in `compat_name` and in `friendly_name` until the next rename.
  Probe on 2026.11.0: `config/entity_registry/update` with
  `name: "Sun Reading Lamp"` on device "Sun" gives `friendly_name`
  "Sun Sun Reading Lamp"; `name: "Reading Lamp"` gives "Sun Reading Lamp".
  `skills/organize` renames by writing the name the user says and reads the
  registry name back, so on 2026.11+ a name that repeats the device name is
  shown doubled while the skill reports success. A device rename now also
  renames how every entity of the device is shown and spoken (Assist),
  which the preview does not say. Fix in `skills/organize`: send only the
  entity's own part (strip a leading device name, say so in the preview),
  preview and verify by the resulting `friendly_name`, and name the
  entities a device rename changes.
- HIT (P3, same change) — `config/entity_registry/list_for_display` drops
  `hn` and the registry lists drop `has_entity_name`;
  `skills/ha-nova/relay-api.md` still documents `hn`. No CLI or skill
  logic reads either (grep-clean). The CLI names entities by the state's
  `friendly_name` (confirmations, `find`, `map`, `get`), which 2026.11
  still sets to the full name, so confirmations are unchanged.
- HIT (P3) — flow `visible` (watch item from 2026-10-24, now shipped):
  serialized `data_schema` fields carry `visible` (`{field, value}`, a list
  of them, or a bool), recursing into sections. A hidden field is not
  required and its value is dropped. `skills/integration-setup` builds the
  step body from `data_schema` without it, so an agent can ask for, or
  preview, a value that is never saved. Fix: ask only for fields whose
  `visible` holds for the answers given so far.
- HIT (P3) — target `exclude` (release post "Exclude items from a target"):
  targets in triggers, conditions and actions, and the target keys of an
  entity service call, accept an `exclude` block with the same keys. The
  tier walk (`cli/tier_walk.go`, `cli/tier_fires.go`) ignores it and
  works on the superset, which can only ask more, never less.
  `ha-nova plan call` refuses `entity_id`/`area_id`/`device_id`/`target`
  and `floor_id`/`label_id` in service data but passes `exclude`, so Home
  Assistant can act on fewer entities than the preview lists (never more).
  Fix: refuse `exclude` in service data like `floor_id`/`label_id`.
- CLEAN — sun and calendar offset sign (core #184855): only the new
  purpose-specific `sun.*` and `calendar.event_*` triggers lose the
  `offset_type: before` default. `skills/ha-nova/automation-patterns.md`
  uses `trigger: sun` with a signed offset ("-00:30:00" = before), whose
  meaning is unchanged.
- CLEAN — Marketplace `2026.11.0b4` → `2026.11.0`: `marketplace/websocket`
  unchanged; `marketplace/info` and `marketplace/repositories/list` keep
  their shape (download URL internals and profiles only). The fix that
  shipped in #957 stands.
- CLEAN — registries: `config/device_registry/*` keys unchanged;
  `config/entity_registry/update` drops its "device is disabled" check
  (the registry now derives an entity's `disabled_by` from its device);
  `config_entries/disable` reports `require_restart: false` for an entry in
  `setup_retry`/`setup_error`;
  `repairs/list_issues` leaves out issues of a disabled config entry. New,
  additive: `explain_target`, `logbook/get_action_calls`,
  `subscribe_system_state`/`dismiss_system_state`. `automation/config`,
  `script/config`, `get_panels`, `frontend/get_translations` and
  `manifest/list` keep their command types; `search` is untouched.
- CLEAN — watch list: update entities keep their attributes (`has_update`
  only decides `on`); `temperature_unit` on climate is additive; no lawn
  mower `idle` activity; `DeviceInfo`, `support_mfa` and `debug_console`
  stay Python- or YAML-only; renaming an `entity_id` updates the entity in
  place (no wire change).
- CLEAN — Auth client IDs (core #186537, #186543): a public IP client ID
  must now match the request address. Link sign-in uses
  `http://127.0.0.1:<port>/…` (loopback, still accepted), and chat-app
  connectors use domain names.
- CLEAN — backups: local agents stay `hassio.local`/`backup.local`; a new
  local-disk mount is one more Supervisor agent, which
  `cli/relay_raw_backup.go` treats as remote (refuses a raw unencrypted
  backup there; the automatic-settings path works).
- CLEAN — child devices for Z-Wave endpoints and own devices for ZHA groups:
  device targets already cover child devices (`cli/tier_device.go`;
  skills fixed with #833). Matter tilt-only covers are blinds now using the
  `*_cover_tilt` actions; device tracker, MQTT, Telegram (legacy notify
  removed), SMTP, Python `deps` and the other integration items name no
  pinned surface.
- CLEAN — dev-blog 2026-10-25 (backup agents can be renamed), 2026-10-28
  (custom section strategies, frontend components, repair issues per config
  entry, ScannerEntity devices and disabled-by-default), 2026-11-01
  (`Undefined` sentinel): Python or frontend APIs, no wire change.
- CLEAN — no HACS release after 2.0.5; HACS is superseded by the
  Marketplace (2026-11-03 entry).
- Live: the disposable-HA E2E on 2026.11.0 is green (run 41998902372).
  The rc lane for v0.28.1-rc2 on a 2026.11.0 test bed passed every step on
  its second run. The first run failed only the DE batch chat check: the
  model put "Gib beide Codes in einer Nachricht ein." after the codes, which
  is model wording, not a Home Assistant change.
- Sources: home-assistant.io/blog/2026/11/04/release-202611/;
  github.com/home-assistant/core/releases/tag/2026.11.0;
  github.com/home-assistant/core/compare/2026.10.3...2026.11.0;
  github.com/home-assistant/core/pull/184340;
  github.com/home-assistant/core/pull/184341;
  github.com/home-assistant/core/pull/184855;
  github.com/home-assistant/core/pull/186537;
  contributors.home-assistant.io/blog/2026/10/25/rename-backup-agents/;
  contributors.home-assistant.io/blog/2026/10/28/custom-section-strategies-in-the-dashboard-editor/;
  contributors.home-assistant.io/blog/2026/10/28/frontend-component-updates-2026.11/;
  contributors.home-assistant.io/blog/2026/10/28/issue-registry-refresh/;
  contributors.home-assistant.io/blog/2026/10/28/scanner-entity-devices/;
  contributors.home-assistant.io/blog/2026/11/01/undefined-helper/;
  github.com/hacs/integration/releases (and tags).
- Next window starts AFTER HA 2026.11.0 + dev-blog 2026-11-01 + HACS 2.0.5.
  Watch the `friendly_name` deprecation (the CLI names entities by it), the
  Marketplace write parity (2026-11-03 entry), the ScannerEntity default
  device removal (2027.11), `via_device` (2027.8) and the legacy
  `DeviceInfo` keys (2027.9), `step_id` for custom flows (2027.5), the
  deferred lawn-mower `idle` bucket, and the legacy device-registry removal
  command for 2027.9.

## 2026-11-05 — pre-release screen (v0.28.2 cycle): HA after 2026.11.0 + developer blog after 2026-11-01 + HACS

- Window screened: Core releases after `2026.11.0` (none; `2026.11.0` of
  2026-11-04 is the latest); contributors.home-assistant.io/blog after
  2026-11-01 (none; the newest post, "Undefined sentinel changes" of
  2026-11-01, was screened above); HACS releases and tags after 2.0.5
  (none).
- Clean pass: nothing upstream changed since the v0.28.1-cycle screen
  above. Its four hits ship fixed in 0.28.2: #988 (#982, #983), #992
  (#984) and #989 (#985).
- Next screen: before the next release cycle, or on HA 2026.11.1 or
  2026.12.0, whichever comes first.
## 2026-11-06 — pre-release screen (v0.28.3 cycle): HA after 2026.11.0 + developer blog after 2026-11-01 + HACS

- Window screened: Core releases after `2026.11.0` (none; `2026.11.0` of
  2026-11-04 is still the latest); contributors.home-assistant.io/blog after
  2026-11-01 (none; the newest post is still "Undefined sentinel changes" of
  2026-11-01); home-assistant.io/blog after the 2026.11 release post (none);
  HACS releases and tags after 2.0.5 (none).
- Clean pass: nothing upstream changed since the v0.28.2-cycle screen above.
- Next screen: before the next release cycle, or on HA 2026.11.1 or
  2026.12.0, whichever comes first.
## 2026-11-08 — pre-release screen (v0.29.0 cycle): HA after 2026.11.0 + developer blog after 2026-11-01 + HACS

- Window screened: Core releases after `2026.11.0` (none; `2026.11.0` of
  2026-11-04 is still the latest); contributors.home-assistant.io/blog after
  2026-11-01 (one post: "Apps will lose four default Linux capabilities" of
  2026-11-06); home-assistant.io/blog after the 2026.11 release post (none);
  HACS releases and tags after 2.0.5 (none).
- Hit, no change needed: the Supervisor stops granting `NET_RAW`, `MKNOD`,
  `AUDIT_WRITE`, and `SETFCAP` to App containers unless an App lists them
  in `privileged` (behind feature flags since Supervisor 2026.09.3; the
  default for new installations from Supervisor 2026.12, later for all). The
  NOVA App (`nova/config.yaml`, `nova/Dockerfile`) is a Node.js HTTP and
  WebSocket service with the bundled CLI: no raw or packet sockets, no
  `mknod`, `sshd`, or `setcap`, so it needs no `privileged` entry. No
  skill pins a capability, and the App install preview in
  `skills/updates/apps.md` reads no new field.
- Source checked: contributors.home-assistant.io/blog/2026/11/06/apps-fewer-default-capabilities/;
  github.com/home-assistant/core/releases; github.com/hacs/integration/releases (and tags).
- Next screen: before the next release cycle, or on HA 2026.11.1 or
  2026.12.0, whichever comes first. Watch Supervisor 2026.12, which turns
  the smaller capability set on.

## 2026-11-09 — service-calling platforms (MAR-127), HA 2026.11.0

- Source checked: `homeassistant/home-assistant:2026.11.0` (the test bed's
  image), grepped for `services.async_call`, `Script(`, and device actions.
- Pinned in `cli/tier_callset.go`: `callingPlatforms` (built-in platforms
  whose entities call services when actuated), `forwards` (built-in services
  whose handlers call other services), and `reactors` (built-in
  integrations that call services when other entities change).
- Re-check the three tables on each HA release: a built-in platform or
  service that calls services and is missing from them makes a
  `call_service` listener look unreachable.
