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
