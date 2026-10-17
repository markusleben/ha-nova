import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const flat = (s: string) => s.replace(/\s+/g, " ");
const skill = flat(readFileSync("skills/esphome/SKILL.md", "utf8"));
const checks = flat(readFileSync("skills/esphome/esphome-checks.md", "utf8"));
const router = flat(readFileSync("skills/ha-nova/SKILL.md", "utf8"));
const index = flat(readFileSync("skills/ha-nova/index.md", "utf8"));
const yamlConfig = flat(readFileSync("skills/yaml-config/SKILL.md", "utf8"));
const filePaths = readFileSync("nova/src/http/handlers/files-paths.ts", "utf8");

describe("esphome skill (needs-analysis TOP 5)", () => {
  it("routes ESPHome device YAML to esphome, not yaml-config", () => {
    expect(router).toContain("| ESPHome device YAML: explain, check, edit; verify the device after an install | `ha-nova:esphome` |");
    expect(index).toContain("`ha-nova:esphome` —");
    expect(yamlConfig).toContain("ESPHome device files (`ha-nova:esphome`)");
    expect(router).toContain("`ha-nova:yaml-config` (files; `/config/esphome/`: `ha-nova:esphome`)");
    expect(flat(readFileSync("skills/ha-nova/output-rules.md", "utf8"))).toContain("masking it (or an ESPHome credential) is the ONE legal edit");
  });

  it("uses the existing opt-in file access and works from pasted YAML without it", () => {
    expect(skill).toContain('`{"action":"list_dir","path":"/config/esphome"}`');
    expect(skill).toContain("`FILE_ACCESS_DISABLED` / `FILE_ACCESS_READONLY`: say once how to turn it on (`../yaml-config/SKILL.md` → Bootstrap), no nagging.");
    expect(skill).toContain("an edit becomes the changed sections (masked, no credential line) for the user to change in the dashboard's editor, then Validate and Install");
    expect(skill).toContain("a standalone ESPHome container keeps them elsewhere");
  });

  it("relies on the relay refusing every secrets.yaml, in any directory", () => {
    // The skill's promise rests on this transport rule: a final-name pattern,
    // so /config/esphome/secrets.yaml is refused like /config/secrets.yaml.
    expect(filePaths).toContain("/^secrets\\.ya?ml/i");
    expect(filePaths).toMatch(/const name = segments\[segments\.length - 1\]/);
    expect(filePaths).not.toMatch(/"esphome"/);
    expect(skill).toContain("Never read `secrets.yaml` (the relay refuses it) or anything under `.esphome/` (build output can hold compiled-in credentials); `*.bak` is never a candidate (Undo reads it).");
  });

  it("never puts secrets into chat or into a file", () => {
    expect(skill).toContain("**Secrets never reach chat.** `!secret <name>` stays a reference.");
    expect(skill).toContain("Mask every credential (checks file → Secrets) in every reply: YAML (explicitly requested included), prose, and pasted output; say a value was masked");
    expect(skill).toContain("confirmation binds to the stored value behind the mask");
    expect(skill).toContain("Never ask for a credential and never write one");
    expect(skill).toContain("Never write a mask: credential lines the user did not ask to change stay byte-identical to the step-1 read.");
    expect(skill).toContain("Validate fails after an edit: show its message (masked)");
    expect(checks).toContain("the value of any key named, or ending in, `password`, `key`, `psk`, `token`, `secret`, or `pin_code` (any case; option names such as `min_auth_mode` are not), everything under an `auth:` block, every HTTP header value, and every substitution that feeds one");
    expect(checks).toContain("the relay refuses only `secrets.yaml`, so `credentials.yaml` reads like any file");
    expect(checks).toContain("say where it lives (this device file, or the dashboard's secrets file), never the value");
    expect(checks).toContain("copy the current value unchanged into the dashboard's secrets file under `<name>`");
    expect(checks).toContain("Move one to `!secret <name>` only when asked, with a device-prefixed `<name>`");
    expect(checks).toContain("a secret inside any string — a lambda argument (`set_auth_password(\"…\")`), a URL with an access key or `user:password@`, a header value");
    expect(checks).toContain("add `ota:` → `- platform: esphome` with `password: !secret <name>` (a new credential), never without");
    expect(checks).toContain("Check every row against the file itself, whatever version runs on the device");
    expect(checks).toContain("writes only `!secret <name>`; the preview tells the user to add a value under `<name>` in the dashboard's secrets file before installing");
    expect(skill).toContain("ask for the YAML pasted with inline passwords and keys replaced by `***` (`!secret` lines stay)");
    expect(skill).toContain("`ha-nova find '<friendly name>'` (single quotes: the name is device data)");
    expect(checks).toContain("Never as a literal: `substitutions:` → `ota_new: !secret <name>`");
    expect(checks).toContain("with a device-prefixed `<name>`");
    expect(checks).toContain("Security-weakening (name the exposure)");
    expect(checks).toContain("A `!secret` name cannot be checked here (the relay never serves `secrets.yaml`)");
  });

  it("edits through the yaml-config file path with snapshot, drift check, preview, and no HA reload", () => {
    expect(skill).toContain("`../yaml-config/SKILL.md` → Flow step 1 (read before write)");
    expect(skill).toContain("Preview as the yaml-config File-Change Preview, masked (Safety)");
    expect(skill).toContain("On a reply to that preview: `../yaml-config/SKILL.md` → Flow step 3 (auto config snapshot category `yaml` with `{path, content}`, drift check, `write_file` with `.bak`)");
    expect(skill).toContain("Never Home Assistant's `check_config` or a reload: ESPHome files are not HA configuration.");
    expect(skill).toContain("one file per operation");
    expect(skill).toContain("The device changes only when installed again — say so.");
    expect(skill).toContain("a mismatch: say so, offer the `.bak` restore");
    expect(skill).toContain("(a new file has neither — say so)");
  });

  it("names device-breaking edits before they are confirmed", () => {
    expect(skill).toContain("Name every device-breaking or security-weakening consequence and every other device the file reaches (checks file → Device-Breaking Edits).");
    expect(skill).toContain("An edit never removes or replaces `api:`, its encryption key, `ota:`, or the network block of a device that works");
    for (const line of [
      "`esphome:` → `name:` is the hostname the next wireless install looks for",
      "An entity's `name:`: Home Assistant may treat it as a new entity",
      "A new or replaced `api` `encryption` `key`: Home Assistant loses the device",
      "Removing `ota:`: every later install needs USB.",
      "without `ap:` (with its password) + `captive_portal:`: one mistake means USB.",
      "OTA reuses that key and \"the device only accepts the key it is running\": the next wireless install fails, USB needed.",
      "Never generate a new key for a device Home Assistant already uses.",
      "A changed `ota` `password`: the install authenticates with the new one and the device refuses it.",
      "set `wifi` → `use_address: <current IP>` for that one install",
      "A file in `packages:` or `!include` of other devices changes all of them",
      "A `substitutions` edit is an edit of every key it feeds",
    ]) {
      expect(checks).toContain(line);
    }
  });

  it("never invents ESPHome keys: findings come from the cited checks file", () => {
    expect(skill).toContain("Nothing outside that file is asserted from memory: read the component's esphome.io page, or say the dashboard's Validate decides.");
    expect(skill).toContain("Write only keys the checks file or the component's esphome.io page shows; never invent options or C++ calls.");
    expect(skill).toContain("Never claim a draft compiles; Validate decides.");
    expect(skill).toContain("A remote package (`github://…`, `url:`) is named as not read; its keys are unknown, never assumed.");
  });

  it("pins the verified deprecations with their sources", () => {
    for (const row of [
      "esphome.io/components/ota (\"In release 2024.6.0, the `ota` component transitioned",
      "ESPHome 2024.6.0 changelog (esphome#6860)",
      "ESPHome 2025.2.0 changelog (esphome#8118, \"Remove old style platform configuration\")",
      "ESPHome 2025.2.0 changelog (esphome#8119)",
      "| `api:` → `password:` | 2026.1.0 | \"will fail to compile\"",
      "ESPHome 2026.1.0 blog (esphome#12707)",
      "esphome.io/components/esp32 (\"Defaults to `esp-idf` for all ESP32 variants\")",
      "ESPHome 2026.6.0 changelog (\"Removes deprecated `custom_components` folder loading\")",
      "`min_auth_mode` now defaults to WPA2",
      "ESPHome 2026.7.0 blog (\"The single-package `!include` form is removed\")",
      "`toolchain: platformio` keeps the old build",
    ]) {
      expect(checks).toContain(row);
    }
    expect(checks).toContain("Not findings: `api:` → `services:` and `homeassistant.service` still work");
  });

  it("rates open device endpoints as security findings and never recommends one", () => {
    expect(checks).toContain("| `api:` without `encryption:` | MEDIUM (security) |");
    expect(checks).toContain("| `web_server:` without `auth:` | MEDIUM (security) |");
    expect(checks).toContain("| `ota:` with neither `password:` nor `encryption:` | MEDIUM (security) |");
    expect(checks).toContain("`ap:` always with `password: !secret <name>`");
    expect(checks).toContain("OTA encryption, in two installs: 1) keep `password:`, install with ESPHome 2026.9.0+");
    expect(checks).toContain("Adding `encryption:` before step 1 ran makes the install refuse");
    expect(checks).toContain("A `list_dir` with `truncated: true` cannot prove the list complete");
    expect(router).toContain("dispatch to `ha-nova:diagnose` (ESPHome YAML or build errors: `ha-nova:esphome`)");
  });

  it("checks pins per chip family with sources", () => {
    expect(checks).toContain("| ESP32 | GPIO34-39 | input only, no internal pull-up/pull-down");
    expect(checks).toContain("| ESP32 | GPIO6-8, GPIO11 | the flash interface: validation error | HIGH |");
    expect(checks).toContain("| ESP32 | GPIO9-10 | flash data in QUAD IO flash mode: validation warning | MEDIUM |");
    expect(checks).toContain("| ESP32 | GPIO16-17 | PSRAM on modules that have it (WROVER)");
    expect(checks).toContain("| ESP32 | GPIO0, 2, 5, 12, 15 | strapping pins");
    expect(checks).toContain("ADC2 is also used by Wi-Fi");
    expect(checks).toContain("| ESP8266 | ADC | only GPIO17 (A0/TOUT), 0-1.0 V |");
    expect(checks).toContain("read their notes on esphome.io/components/esp32 before judging a pin there");
  });

  it("keeps compile and flash external with exact next steps", () => {
    expect(skill).toContain("**Hand off** with the checks file → Hand-Off steps, exactly; wait for the user.");
    expect(checks).toContain("Settings → Apps → ESPHome Device Builder → Open Web UI");
    expect(checks).toContain("**Validate**, then **Install** → On the network (older dashboard: Wirelessly)");
    expect(checks).toContain("The very first install, or a device without `ota:`, needs USB.");
    expect(checks).toContain("⋮ → **Show encryption key** — the user copies it there, never into chat");
    expect(checks).toContain("`esphome config <file>.yaml` (validates; it hides secrets only on screen, so pasted output still needs masking)");
    expect(checks).toContain("Home Assistant's ESPHome firmware update entity compiles and installs the current file too");
    expect(flat(readFileSync("skills/updates/SKILL.md", "utf8"))).toContain("`platform: esphome` compiles the device's current YAML: check it with `ha-nova:esphome` first.");
    expect(flat(readFileSync("skills/diagnose/SKILL.md", "utf8"))).toContain("an ESPHome device's own YAML (pins, removed keys, Wi-Fi/API/OTA) -> `ha-nova:esphome`");
  });

  it("verifies the device side through find/get after the user installed it", () => {
    expect(skill).toContain("`ha-nova get <entity_id> --references` (`references.entities[]`: `platform`, `state` for the whole device)");
    expect(skill).toContain('`{"type":"config_entries/get","domain":"esphome"}`');
    expect(skill).toContain("**Verify** after the user says it is installed");
    expect(skill).toContain("Never report a change as on the device before the user installed it and the entities came back.");
    expect(skill).toContain("offer the takeover — the stale entry removed (`ha-nova:maintenance`), the new id renamed to the old (`ha-nova:organize`) — never a device swap");
    expect(skill).toContain("`{\"type\":\"config/device_registry/list\"}` with `--jq-file` to the `references.device.id` row (`sw_version`: ESPHome version and build time)");
    expect(skill).toContain("a build time before the write means the new file is not on the device — say so");
    expect(skill).toContain("The Options line is the reply's last line: no reason after it, and never quote, cite, or link this skill, its rules, or any HA NOVA file.");
    expect(skill).toContain("and the exact Hand-Off steps.");
  });
});
