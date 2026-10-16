# ESPHome Checks (reference for `ha-nova:esphome`)

Checked against esphome.io and the ESPHome changelogs up to 2026.9 (October
2026). Each row names its source; cite it with the finding. A key, option,
or limit that is not here is not asserted from memory: read the component's
page (`https://esphome.io/components/<component>/`) or say Validate decides.
Severity: HIGH = will not compile or will strand the device; MEDIUM = works
today, breaks on a likely next step; LOW = advisory.

## Required Blocks

| Finding | Severity | Fix | Source |
|---|---|---|---|
| No network block (`wifi:`, `ethernet:`, …) | HIGH | add `wifi:` with `ssid: !secret wifi_ssid`, `password: !secret wifi_password` | esphome.io/components/wifi: "You need to have a network configuration … or ESPHome will fail in the config validation stage" |
| No `api:` (and no `mqtt:`) | HIGH | add `api:` with `encryption: key: !secret <device>_api_key`; the device appears in HA only with it | esphome.io/components/api |
| `api:` without `encryption:` | LOW | add `encryption: key:` (a 32-byte base64 string) as a `!secret`; HA then asks for the key once (Settings → Devices & services) | esphome.io/components/api ("encryption will not be used until it is set") |
| No `ota:` | MEDIUM | add `ota:` with `- platform: esphome`; until one install carries it, updates need USB | esphome.io/components/ota |
| `wifi:` without `ap:` and `captive_portal:` | LOW | add both: the device opens its own hotspot when it cannot reach the router (`ap_timeout`, default 90s), so a wrong Wi-Fi edit stays fixable without USB | esphome.io/components/wifi |

## Removed or Changed Keys

Ask which ESPHome version last installed the device (the dashboard shows it; an `update.*` firmware entity of the device, when present, too) — rows older than that version have already been dealt with.

| Old YAML | Since | What happens | Fix | Source |
|---|---|---|---|---|
| `ota:` as a mapping (`ota:` → `password:` directly below) | 2024.6.0 | config error | `ota:` → `- platform: esphome` (its options go in that list item) | esphome.io/components/ota ("In release 2024.6.0, the `ota` component transitioned … to a platform component") |
| top-level `dallas:` + `platform: dallas` | 2024.6.0 | config error | `one_wire:` → `- platform: gpio` with the `pin:`; each sensor `platform: dallas_temp` | ESPHome 2024.6.0 changelog (esphome#6860) |
| `platform:` / `board:` inside `esphome:` | 2025.2.0 | config error | move to an `esp8266:` or `esp32:` block with `board:` | ESPHome 2025.2.0 changelog (esphome#8118, "Remove old style platform configuration") |
| `platform: custom` sensors/switches/components | 2025.2.0 | config error | an `external_components:` component, or a `template` platform with a lambda | ESPHome 2025.2.0 changelog (esphome#8119) |
| `api:` → `password:` | 2026.1.0 | "will fail to compile" | `encryption:` → `key: !secret <device>_api_key`; HA asks for the key after the install | ESPHome 2026.1.0 blog (upgrade checklist; esphome#12819) |
| OTA with an MD5 password | 2026.1.0 | ESPHome before 2025.10.0 using password auth cannot OTA to a device on 2026.1.0+ | update the Device Builder App first | ESPHome 2026.1.0 blog (esphome#12707) |
| `esp32:` without `framework:` | 2026.1.0 | now builds with ESP-IDF; before, Arduino | to keep the old behavior, `framework:` → `type: arduino` (needed by e.g. `heatpumpir`, `midea`, the `wled` light effect) | ESPHome 2026.1.0 blog; esphome.io/components/esp32 ("Defaults to `esp-idf` for all ESP32 variants") |
| a `custom_components/` folder beside the YAML | 2026.6.0 | no longer loaded | `external_components:` | ESPHome 2026.6.0 changelog ("Removes deprecated `custom_components` folder loading") |
| ESP8266 on a WPA-only router | 2026.6.0 | `min_auth_mode` now defaults to WPA2, the device stays offline | `wifi:` → `min_auth_mode: WPA` (weaker: TKIP) or a WPA2 router | ESPHome 2026.6.0 changelog; esphome.io/components/wifi |

Not findings: `api:` → `services:` and `homeassistant.service` still work (now called actions; esphome.io/components/api).

## Pins

The chip family is the `esp32:`/`esp8266:` block and its `board:`. ESP32
variants (C3, C6, S2, S3, …) differ: read their notes on
esphome.io/components/esp32 before judging a pin there.

| Chip | Pins | Rule | Severity | Source |
|---|---|---|---|---|
| ESP32 | GPIO34-39 | input only, no internal pull-up/pull-down: never an `output`, a switch, or `mode: INPUT_PULLUP` | HIGH | esphome.io/components/esp32 ("These pins cannot be used as outputs"); Espressif ESP32 GPIO reference |
| ESP32 | GPIO6-11, GPIO16-17 | usually wired to the module's flash/PSRAM: never use | HIGH | Espressif ESP32 GPIO reference |
| ESP32 | GPIO0, 2, 5, 12, 15 | strapping pins: something pulling them at boot can stop the boot; outputs are fine | MEDIUM | Espressif ESP32 GPIO reference; esphome.io/components/esp32 |
| ESP32 | ADC2 pins (GPIO0, 2, 4, 12-15, 25-27) | ADC2 is also used by Wi-Fi: use an ADC1 pin (GPIO32-39) for `platform: adc` | MEDIUM | esphome.io/components/sensor/adc; Espressif ADC oneshot reference ("ADC2 is also used by Wi-Fi") |
| ESP8266 | GPIO6-11 | used by the flash interface: avoid | HIGH | esphome.io/components/esp8266 |
| ESP8266 | GPIO0, 2, 15 | boot mode: GPIO0 and GPIO2 high, GPIO15 low at boot | MEDIUM | esphome.io/components/esp8266 |
| ESP8266 | GPIO16 | pull-down only, the deep-sleep wake pin | LOW | esphome.io/components/esp8266 |
| ESP8266 | ADC | only GPIO17 (A0/TOUT), 0-1.0 V | HIGH when another pin | esphome.io/components/esp8266 |

The same pin used by two components is a validation error unless it sets `allow_other_uses: true` (HIGH; esphome.io/guides/configuration-types).

## Secrets

- An inline credential (`wifi` `password`, `ap` `password`, an `api` `encryption` `key`, an `ota` `password`): MEDIUM. Fix: `!secret <name>`; the user puts the value into the dashboard's secrets file. esphome.io/components/wifi: "It is highly recommended to use secrets."
- A `!secret` name cannot be checked here (the relay never serves `secrets.yaml`): Validate reports a missing one.

## Device-Breaking Edits (name each in the preview)

- `esphome:` → `name:` is the hostname the next wireless install looks for; the device keeps its old one until then. Use the dashboard's rename action, which installs the new name to the old address.
- An entity's `name:`: Home Assistant may treat it as a new entity, leaving the old id unavailable and its automations pointing at nothing. To rename only in HA: `ha-nova:organize`.
- A new or replaced `api` `encryption` `key`: Home Assistant loses the device until the user enters the new key (Settings → Devices & services).
- Removing `ota:`: every later install needs USB.
- `wifi` `ssid`/`password`/`manual_ip` changes without `ap:` + `captive_portal:`: one mistake means USB.
- A `framework:` change: a clean rebuild; some components need Arduino.
