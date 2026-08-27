# Upstream Drift Log

Status: active

Append-only record of upstream-release screenings against skill-pinned
surfaces (rule: AGENTS.md → Research → Upstream drift check). Every run is
logged — hits AND clean passes — so the next run knows where the last one
stopped.

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
