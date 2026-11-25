# Upstream Drift Log

Status: active

Append-only record of upstream-release screenings against skill-pinned
surfaces (rule: AGENTS.md → Releases → upstream drift check). Every run is
logged — hits AND clean passes — so the next run knows where the last one
stopped. A full screen's heading says "pre-release screen" or "deep-audit
screen" (`## YYYY-MM-DD — pre-release screen …`); `npm run verify:drift-log`
counts only those, not ad hoc checks or corrections.

Entries older than the last two releases move to
`docs/archive/work/upstream-drift-log-until-0.31.md` at each release prep (the
check reads only the newest screen); entries that code comments cite as a
standing recheck stay here.

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

## 2026-11-10 — thermostat and hygrostat helper options (MAR-131), HA 2026.11.1

- Source checked: `home-assistant/core` tag 2026.11.1:
  `generic_thermostat/config_flow.py`, `generic_thermostat/climate.py`,
  `generic_hygrostat/config_flow.py`, `generic_hygrostat/humidifier.py`,
  `helpers/schema_config_entry_flow.py`.
- Pinned in `cli/tier_helper_options.go` (`helperOptionFields`): each helper
  calls only `homeassistant.turn_on`/`turn_off` on its options' `heater`
  (climate.py:681,710) or `humidifier` (humidifier.py:579,584); the options
  flow's `init` step is a `SchemaFlowFormStep` with no callback whose
  `suggested_value`s are the stored options, and an abort saves nothing.
- Re-check on each HA release: a new service call in either platform, a
  renamed field, a callback or preview on the `init` step, or a second step
  before `init` breaks the read (it then fails closed to unbounded).

## 2026-11-17 — pre-release screen (v0.32.0 cycle): HA 2026.11.2 + developer blog after 2026-11-10 + HACS

- Window screened: Core releases after `2026.11.1` (`2026.11.2` of
  2026-11-16); contributors.home-assistant.io/blog after 2026-11-10 (none:
  "Deprecated device info constants" of 2026-11-10 is still the newest
  post); home-assistant.io/blog after the 2026.11 release post (one
  hardware announcement, no API change); HACS releases and tags after 2.0.5
  (none); Supervisor releases after `2026.11.0` (none).
- Hit, no change needed: `2026.11.2` rejects `input_text.set_value` values
  that violate the helper's pattern. The skills never set a value outside a
  helper's pattern on purpose; such a call now fails in Home Assistant, and
  HA NOVA reports the service error as it does for any refused call.
- Clean pass otherwise: the other `2026.11.2` items are integration fixes
  and dependency bumps. None changes a WS command, REST route, or service
  that `skills/**/*.md` pins, so every pinned surface stands as screened on
  HA 2026.11.1.
- The 0.32 train pins no new upstream surface: its changes are between HA
  NOVA, its App, and the computer (window approval, the Mac key, Link
  sign-out through Home Assistant's existing OAuth token revoke).
- Source checked: github.com/home-assistant/core/releases (API listing);
  developers.home-assistant.io/blog/; home-assistant.io/blog/;
  github.com/hacs/integration/releases and tags;
  github.com/home-assistant/supervisor/releases.
- Next screen: before the next release cycle, or on HA 2026.11.3 or
  2026.12.0, whichever comes first. Watch Supervisor 2026.12, which turns
  the smaller App capability set on.

## 2026-11-18 — pre-release screen (v0.33.0 cycle): HA after 2026.11.2 + developer blog after 2026-11-10 + HACS + Supervisor

- Window screened: Core releases after `2026.11.2` (none);
  contributors.home-assistant.io/blog after 2026-11-10 (one post, "The
  multicast plugin is being deprecated" of 2026-11-17); home-assistant.io/blog
  since the last screen (a partner announcement and a hardware launch, no API
  change); HACS releases and tags after 2.0.5 (none); Supervisor releases
  after `2026.11.0` (none).
- Hit, no change needed: Supervisor 2026.12 stops installing the multicast
  plugin on new installations, and a later release removes it. Only apps that
  listen for mDNS themselves on the internal network are affected. The NOVA
  App doesn't: it publishes its ports and uses Supervisor discovery
  (`nova/config.yaml`), and the CLI finds Home Assistant from the computer, not
  from inside an app. No skill pins a multicast surface.
- The 0.33 train pins no new upstream surface. Decision A (MAR-209) reads the
  same `call_service` listener counts as 0.32; the chat code, the phone offer,
  and the file-key offer are between HA NOVA, its App, and the computer.
- Source checked: github.com/home-assistant/core/releases (API listing);
  contributors.home-assistant.io/blog/; home-assistant.io/blog/;
  github.com/hacs/integration/releases and tags;
  github.com/home-assistant/supervisor/releases.
- Next screen: before the next release cycle, or on HA 2026.11.3 or
  2026.12.0, whichever comes first.

## 2026-11-20 — pre-release screen (v0.34.0 cycle): HA after 2026.11.2 + developer blog after 2026-11-18 + HACS + Supervisor

- Window screened: Core releases after `2026.11.2` of 2026-11-16
  (none; no `2026.11.3` or `2026.12.0` beta); contributors.home-assistant.io/blog
  after 2026-11-18 (none; “The multicast plugin is being deprecated” of
  2026-11-17 is still the newest post, covered in the last screen);
  home-assistant.io/blog since the last screen (one post, “Community Day
  2026: You knocked it out of the park!” of 2026-11-19, no API change);
  HACS releases and tags after `2.0.5` of 2025-01-28 (none); Supervisor
  releases after `2026.11.0` of 2026-11-06 (none). The beta channel still
  lists Core `2026.11.2` and Supervisor `2026.11.0`.
- Clean pass: nothing new changes a WS command, REST route, or service
  pinned in `skills/**/*.md` or `cli/*.go`, including the registry commands,
  `system_log/list`, `manifest/list`, the Core `/api/` routes, and the
  Supervisor `/addons` and `/store` routes.
- The 0.34 train adds a filesystem dependency (MAR-245), not a new
  upstream WS command or REST route. The NOVA App's own `list_dir` and
  `read_file` use the existing `homeassistant_config` mount at `/config`;
  `list_dir` now reports change time from `lstat`. Against Core `2026.11.2`,
  the run lock still records `start_ts`, the YAML loader still uses
  `annotatedyaml`, and the generic thermostat `heater`, generic hygrostat
  `humidifier`, and template switch `unique_id` and `turn_on`/`turn_off`
  shapes still match. No change needed. The announced Supervisor 2026.12
  capability reduction does not remove a capability used by these reads.
- Source checked: [Core releases (API listing)](https://api.github.com/repos/home-assistant/core/releases?per_page=1),
  [Core tags](https://github.com/home-assistant/core/tags), and the
  [2026.11 patch notes](https://www.home-assistant.io/blog/2026/11/04/release-202611/#patch-releases);
  [contributors blog](https://contributors.home-assistant.io/blog/);
  [home-assistant.io/blog](https://www.home-assistant.io/blog/);
  [HACS releases](https://github.com/hacs/integration/releases) and [tags](https://github.com/hacs/integration/tags);
  [Supervisor releases](https://github.com/home-assistant/supervisor/releases) and
  the [beta channel](https://version.home-assistant.io/beta.json);
  Core `2026.11.2` [runner](https://github.com/home-assistant/core/blob/2026.11.2/homeassistant/runner.py),
  [YAML loader](https://github.com/home-assistant/core/blob/2026.11.2/homeassistant/util/yaml/loader.py),
  [generic thermostat](https://github.com/home-assistant/core/blob/2026.11.2/homeassistant/components/generic_thermostat/climate.py),
  [generic hygrostat](https://github.com/home-assistant/core/blob/2026.11.2/homeassistant/components/generic_hygrostat/__init__.py),
  [template helpers](https://github.com/home-assistant/core/blob/2026.11.2/homeassistant/components/template/helpers.py)
  and [switch](https://github.com/home-assistant/core/blob/2026.11.2/homeassistant/components/template/switch.py);
  [App mount configuration](https://contributors.home-assistant.io/docs/apps/configuration/)
  and the [Supervisor capability announcement](https://contributors.home-assistant.io/blog/2026/11/06/apps-fewer-default-capabilities/).
- Next screen: before the next release cycle, or on HA `2026.11.3` or
  the `2026.12.0` beta, whichever comes first. Watch Supervisor 2026.12
  for the smaller App capability set and multicast rollout.

## 2026-11-23 — pre-release screen (v0.35.0 cycle): HA after 2026.11.2 + developer blog after 2026-11-18 + HACS + Supervisor

- Window screened: Core releases after `2026.11.2` of 2026-11-16
  (none; no `2026.11.3` or `2026.12.0` beta); contributors.home-assistant.io/blog
  after 2026-11-18 (one post, “Dict-style access to device info is
  deprecated” of 2026-11-19, which changes the Python `DeviceInfo` API for
  integrations, not a WS command or REST route); HACS releases after
  `2.0.5` of 2025-01-28 (none); Supervisor releases after `2026.11.0` of
  2026-11-06 (none). The beta channel still lists Core `2026.11.2` and
  Supervisor `2026.11.0`.
- Clean pass: nothing new changes a WS command, REST route, or service
  pinned in `skills/**/*.md` or `cli/*.go`.
- The 0.35 train adds no upstream WS command or REST route. Batching
  (`POST /ws` with `batch`, `ws_batch` in `/health`) is a NOVA route;
  the App sends the same `automation/config` and `script/config`
  messages one after another (MAR-180). It relies on these existing
  shapes, all unchanged:
  - Core `2026.11.2`: the event trigger
    (`components/homeassistant/triggers/event.py`) renders `event_data`
    once with the trigger variables and matches it key by key (MAR-262);
    `search/related` still returns the scripts and automations of an area
    or floor (MAR-262); the state trigger accepts entity registry ids
    (`cv.entity_ids_or_uuids`, resolved by `er.async_validate_entity_ids`),
    matched against the `id` field of `config/entity_registry/list`
    (MAR-263).
  - Supervisor `2026.11.0` and the frontend: `/addons/self/info` still
    reports the App's `slug`, and the App panel lives at `/app/<slug>`
    (MAR-266).
  - Companion apps: notification data `url` (iOS), `clickAction`
    (Android, including `noAction`), `sticky`, and `push.sound` (MAR-266).
- Source checked: [Core releases (API listing)](https://api.github.com/repos/home-assistant/core/releases?per_page=3),
  [contributors blog](https://contributors.home-assistant.io/blog/),
  [HACS releases](https://github.com/hacs/integration/releases),
  [Supervisor releases](https://github.com/home-assistant/supervisor/releases) and
  the [beta channel](https://version.home-assistant.io/beta.json);
  Core `2026.11.2` [event trigger](https://github.com/home-assistant/core/blob/2026.11.2/homeassistant/components/homeassistant/triggers/event.py)
  and [state trigger](https://github.com/home-assistant/core/blob/2026.11.2/homeassistant/components/homeassistant/triggers/state.py);
  [Companion app notifications](https://companion.home-assistant.io/docs/notifications/notifications-basic/).
- Next screen: before the next release cycle, or on HA `2026.11.3` or
  the `2026.12.0` beta, whichever comes first. Watch Supervisor 2026.12
  for the smaller App capability set and multicast rollout.

## 2026-11-25 — pre-release screen (v0.36.0 cycle): HA 2026.11.3 + developer blog after 2026-11-19 + HACS + Supervisor

- Window screened: Core releases after `2026.11.2` (one: `2026.11.3` of
  2026-11-23; no `2026.12.0` beta yet, the beta channel lists Core
  `2026.11.3`); contributors.home-assistant.io/blog after 2026-11-19
  (none); HACS releases after `2.0.5` (none); Supervisor releases after
  `2026.11.0` (none).
- `2026.11.3` touches three areas outside device integrations, none of
  them a pinned shape:
  - `core_config.py` (#193306): `config/core/update` now checks the time
    zone before it changes anything, so an invalid time zone changes
    nothing. The message, its fields, and the tier HA NOVA gives it are
    unchanged.
  - `auth/indieauth.py` (#193440): larger response headers when Home
    Assistant fetches an OAuth `client_id` page. The Link OAuth flow sends
    the same requests.
  - Marketplace (#193438, #193439): install fallback and card file mode
    only; `marketplace_remove` and the Marketplace reads keep their shapes.
- Clean pass: nothing new changes a WS command, REST route, or service
  pinned in `skills/**/*.md` or `cli/*.go`.
- The 0.36 train adds no upstream WS command or REST route. It relies on
  these existing shapes, all unchanged in Core `2026.11.3`:
  - `config/auth/list` rows carry `id`, `is_owner`, `is_active`,
    `system_generated`, and `group_ids` (MAR-170, MAR-181: the role read
    per request and under the chat lock);
  - `config/entity_registry/update` with `name`, `aliases`, and
    `new_entity_id` (#979 Rename Scheme);
  - the entity, device, and area registry lists,
    `config/device_registry/list_composite_splits`, `/api/states`,
    `/api/services`, `/api/events`, and `automation/config` /
    `script/config` (#978 dead-reference sweep, MAR-272, MAR-280).
- The test bed pin moves to Core `2026.11.3` with #1297 (merged into
  `main` as c21a521e; the train takes it at its landing).
- Source checked: [Core releases (API listing)](https://api.github.com/repos/home-assistant/core/releases?per_page=5),
  the [2026.11.2...2026.11.3 compare](https://github.com/home-assistant/core/compare/2026.11.2...2026.11.3),
  [contributors blog](https://contributors.home-assistant.io/blog/),
  [HACS releases](https://github.com/hacs/integration/releases),
  [Supervisor releases](https://github.com/home-assistant/supervisor/releases) and
  the [beta channel](https://version.home-assistant.io/beta.json);
  Core `2026.11.3` [config/auth.py](https://github.com/home-assistant/core/blob/2026.11.3/homeassistant/components/config/auth.py)
  and [config/entity_registry.py](https://github.com/home-assistant/core/blob/2026.11.3/homeassistant/components/config/entity_registry.py).
- Next screen: on the `2026.12.0` beta (due this week) or before the next
  release cycle, whichever comes first. Watch Supervisor 2026.12 for the
  smaller App capability set and the multicast plugin's removal.
