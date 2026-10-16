# ESPHome skill: explain, check, and edit ESPHome device YAML — Spec (2026-10-17)

Status: active

## Problem

People ask an assistant to explain or fix their ESPHome device configs
(needs analysis TOP 5; forum 840935, 1028637 #14; ha-mcp#1167, #1319). The
files sit in `/config/esphome/` beside Home Assistant's own config. What
breaks in practice: configs that stopped compiling after an ESPHome release
(`api: password:` removed in 2026.1.0, `ota:` as a platform list since
2024.6.0, `dallas` replaced by `one_wire` + `dallas_temp` in 2024.6.0), pins
that cannot do what the YAML asks (input-only GPIO34-39 used as an output,
flash pins, strapping pins), inline credentials, and edits that strand a
working device (a new hostname, a removed `ota:` block, a changed encryption
key, Wi-Fi changes without a fallback hotspot).

ha-nova already has the transport: opt-in `relay files` (list/read/write
with a `.bak`), the `yaml` config-snapshot category, and the yaml-config
read-before-write, snapshot, and drift-check steps. It has no ESPHome
expertise. Compile, flash, and device logs live in the ESPHome Device Builder
App, which has no Home Assistant API.

## Feasibility check (done before writing the skill)

- `nova/src/http/handlers/files-paths.ts`: `esphome` is not a denied segment,
  so `/config/esphome/*.yaml` is listable, readable, and writable (`.yaml` is a
  writable extension) when the user enabled `file_access`.
- The final-name pattern `/^secrets\.ya?ml/i` applies in every directory:
  `/config/esphome/secrets.yaml` (and `.bak`/`~` copies) is always refused.
  Secret values are unreachable by construction; only `!secret <name>`
  references in device files are visible.
- Gap, not fixed here: reads are not extension-limited, and `.esphome/` is
  not a denied segment. When ESPHome's build directory lives under
  `/config/esphome/.esphome/`, generated sources there can contain compiled-in
  credentials. The skill forbids reading `.esphome/`; a relay deny entry is a
  separate, release-bound change.

No new verb, no relay change.

## Behavior

A new task skill `ha-nova:esphome`. Expertise only.

### Read and explain

- Locate: `list_dir /config/esphome` (device files are top-level `*.yaml`;
  skip `secrets.yaml`, `.esphome/`, `archive/`, `*.bak`). Pasted YAML works
  the same without file access.
- Resolve `substitutions` (`${name}`) and local `packages`/`!include` files
  (read them too); a remote package (`github://…`, `url:`) is named as not
  read.
- Explain in plain language: board and framework, network, the HA link
  (`api`), OTA, then each sensor/switch/light with its pin and what it does
  in Home Assistant. Lambdas are explained by behavior.

### Check (diagnose)

`skills/esphome/esphome-checks.md` holds each check with its source:
missing `api:`/`ota:`/`wifi:` (or other network), deprecated or removed keys
since 2024.6.0, pin misuse per chip family, inline credentials, ESP32
framework default change (ESP-IDF since 2026.1.0), ESP8266 Wi-Fi
`min_auth_mode` WPA2 default (2026.6.0). Each finding cites the esphome.io
page or changelog. Anything not listed there is never asserted from memory:
the agent reads the component's esphome.io page or says the dashboard's
Validate decides.

### Edit

- Through the yaml-config file path, steps 1 and 3 (read before write;
  File-Change Preview; natural confirmation; `yaml` auto config snapshot;
  drift check; `write_file` with `.bak`). NOT Home Assistant's
  `check_config` or a reload: ESPHome files are not HA configuration.
- After the write: read back, then the external handoff (below).
- Device-breaking edits are named in the preview and need the user to
  accept the named consequence: `esphome: name:` change (use the
  dashboard's Rename instead), removing or replacing `api:` encryption keys,
  removing `ota:`, Wi-Fi/`manual_ip` changes without `ap:` +
  `captive_portal:`, any `manual_ip` change (`use_address` for one
  install), an `ota` password change (`on_boot` → `set_auth_password`), a
  changed `api` key (also the OTA key on 2026.9.0+ with OTA encryption), an
  entity `name:` change (HA may create a new entity), and a shared
  `packages:`/`!include` file or `substitutions` edit (every device and key
  it reaches is listed).
- One file per operation.

### Secrets

- Never show, ask for, or write a credential value. A credential is the
  value of any key whose name contains `password`, `key`, `psk`, `token`,
  `secret`, `auth`, or `bearer`, every HTTP header value, and every
  substitution feeding one, in any read file. It is masked in every reply:
  YAML (explicitly requested included), prose, pasted Validate/log output;
  confirmation binds to the stored value behind the mask. A mask string is
  never written: untouched credential lines stay byte-identical.
- An inline credential is LOW (the wizard writes the `api` key inline).
  Moving one to `!secret <name>` only on request; the preview tells the user
  to copy the current value unchanged into the dashboard's secrets file
  first, because a different value is a new credential.

### Compile and flash: external

Exact next step, either path:
- App: Home Assistant → Settings → Apps → ESPHome Device Builder → Open Web
  UI → device card → ⋮ → Validate, then Install → On the network (older
  dashboard: Wirelessly; first install: USB). Logs on the card.
- CLI: `esphome config <file>` (redacts secrets), `esphome run <file>`
  (compile, upload, logs), `esphome logs <file>`.

### Device side (verify)

After the user reports the install: `ha-nova find "<friendly name>"`, then
`ha-nova get <one entity> --references` → `references.entities[]` (platform
`esphome`, state), then the device registry `sw_version` (ESPHome version
and build time): a build older than the write means the file is not on the
device yet. All `unavailable` = device offline or not reconnected;
config entry state via `config_entries/get` with `domain: esphome`. A new
device not yet in HA: Discovered → `ha-nova:integration-setup` (the user
enters the encryption key in the HA UI, never in chat).

### Restore

`.bak` (one step) or the `yaml` snapshot (`config-snapshots.md` → Restore)
through this skill's edit flow. Restoring the file does not restore the
firmware: the user installs again.

## Non-goals

Compile, flash, logs, adoption over the Device Builder's own API; writing
`secrets.yaml`; ESPHome configs outside HA's `/config` (a standalone
ESPHome container: pasted YAML only); C++ custom components.

## Acceptance

- Dispatch row + index line; router budgets unchanged (trim).
- Safety Core byte-identical; one-time-code terminology.
- Contract test pins: file-access probe, no `check_config`/reload, yaml
  snapshot + drift check, masking, never `secrets.yaml`/`.esphome/`,
  device-breaking edits named, external compile/flash steps, device-side
  verification, cited deprecations.
- Agent check on the local test bed: explain + diagnose + one edit of a
  sample `esphome/kitchen-sensor.yaml`, with no secret value in the reply.
