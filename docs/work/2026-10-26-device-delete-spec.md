# Device deletion through HA NOVA

Status: active

Date: 2026-10-26. Owner: `ha-nova:organize`.

## Research (Home Assistant 2026.10)

- The [frontend device page](https://github.com/home-assistant/frontend/blob/dev/src/panels/config/devices/ha-config-device-page.ts) offers Delete only for a loaded config entry with `supports_remove_device`. Its [registry client](https://github.com/home-assistant/frontend/blob/dev/src/data/device/device_registry.ts) calls `config/device_registry/remove` with `device_id`, after a destructive confirmation. The [developer docs](https://developers.home-assistant.io/docs/device_registry_index/#removing-devices) describe `async_remove_config_entry_device`: the integration decides whether removal is allowed and should return false when it is not. Without this hook, remove the integration entry instead, affecting everything it owns; never silently substitute that larger operation.
- [Core 2026.10 handler](https://github.com/home-assistant/core/blob/2026.10.0/homeassistant/components/config/device_registry.py): both commands are admin-only. `remove` delegates to the integration hook before removing the registry record. Success is `null`. `remove_config_entry` additionally requires `config_entry_id` matching the owner; it is deprecated until 2027.9. The new command is absent in [2026.8](https://github.com/home-assistant/core/blob/2026.8.0/homeassistant/components/config/device_registry.py) and present in [2026.9](https://github.com/home-assistant/core/blob/2026.9.0/homeassistant/components/config/device_registry.py). The legacy command predates 2026.8; that release changed its semantics from detaching an entry from a potentially shared device to removing an owning device.
- Handler errors are `HomeAssistantError`: `Cannot remove a composite device`, `Unknown device`, `Config entry not in device` (legacy only), `Unknown config entry`, `Config entry does not support device removal`, `Integration not found`, and `Failed to remove device entry, rejected by integration`. [WS connection handling](https://github.com/home-assistant/core/blob/2026.10.0/homeassistant/components/websocket_api/connection.py) maps these to `home_assistant_error`; non-admin is `unauthorized`, invalid payload `invalid_format`. An unsupported command on older HA is not permission to probe another write.
- [Device registry](https://github.com/home-assistant/core/blob/2026.10.0/homeassistant/helpers/device_registry.py) removes the selected device and its direct child devices (`parent_device_id`), not topology neighbours (`via_device_id`). [Entity registry](https://github.com/home-assistant/core/blob/2026.10.0/homeassistant/helpers/entity_registry.py), `async_device_modified`, removes associated entities whose config entry AND subentry match the removed device, including disabled records; foreign-owned entities are unlinked instead. Consumers are not rewritten. Registry removal does not purge recorder history/statistics and is not a physical factory reset. `async_remove_device` keeps a deleted-device record: if the same integration adds the device again, Home Assistant restores its id, area, labels, and user-given name itself.

## Flow and safety

1. Resolve a device by its current name from the device registry; ask if ambiguous. Require HA 2026.9+ and a loaded owning entry whose metadata says `supports_remove_device`. Older HA gets Settings > Devices & services, not an experimental legacy call.
2. Expand the device plus direct child devices. Read their full entity registry records, including disabled entities. Refuse when any has a lock, alarm panel, or access cover (unknown cover class fails closed), or when the device is HA NOVA's own Relay/App. The shared raw guard enforces these at planning and immediately before sending on computers and in chat apps alike.
3. Run `search/related` per device AND per entity. Show the removed entities/count and the union of indexed consumers, plus entities merely unlinked. Say templates and dashboards are not ruled out by this index. Failed reads are inconclusive: stop rather than claim no consumers.
4. Save a `metadata` snapshot containing the device records and full entity registry records first, following `config-snapshots.md`; then send the single official WS delete in the same raw plan. Use existing typed confirmation: computer window/linked phone, app form where available, otherwise the user's one-time code. Relay `say` in full. No handwritten code or unplanned raw delete.
5. Verify registry absence after success; on timeout verify before any retry. On unsupported removal or integration rejection, report HA's error and offer the existing typed integration-entry removal flow (`fallback/relay-ready.md`) or HA UI. Entry removal needs its own full impact preview and confirmation, never the device's confirmation.
6. No undo or guaranteed restore: the snapshot can reapply names/areas only if the integration brings the matching device/entities back. Do not recreate registry records or promise that integration-specific removal side effects can be reversed.

## Deliberate scope

KISS, per maintainer: one new allowlisted WS type, existing raw guard/tier/say paths, one owning skill section; no Relay changes, new flags, commands, or dependency. The generic `/remove` tier is already typed; its allowlist-driven parity test must add the expected typed case. The old command remains denied: supporting old shared-device detach semantics would add a second destructive contract. HA below 2026.9 uses the UI. Single-device deletion only; no new batch family.

## Known limits

- `ha-nova relay ws` can still send the delete without the plan (guard only, no snapshot or code), like every other allowlisted raw delete; the skill always uses `plan raw`.
- The plan binds what goes (devices and entities) to its hash, so a member added after the preview is drift; what the integration's own `async_remove_config_entry_device` removes besides the registry records is outside HA NOVA's view.
- The own-App refusal recognises the published App slug (`*_ha_nova_relay`) and the `ha_nova` domain, not a renamed local dev App.

## Verification

- Fake Relay Go tests: own-App/access/child-device refusals, unreadable registries, missing target, typed tier, device name and removed-entity count in `say` and prompt, apply rechecks; allowlist-driven CLI/chat parity.
- Skill contracts: routing, impact, snapshots, typed confirmation, unsupported-integration handoff, honest recovery; existing word/route budgets unchanged.
- Typecheck, docs checks, targeted safe tests; read-only adversarial reviews and local Codex review. A disposable test-bed agent run only when Docker is available and a deletable demo device exists; never a real home, host pairing, or keychain-sensitive test.
