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
  });

  it("uses the existing opt-in file access and works from pasted YAML without it", () => {
    expect(skill).toContain('`{"action":"list_dir","path":"/config/esphome"}`');
    expect(skill).toContain("`FILE_ACCESS_DISABLED` / `FILE_ACCESS_READONLY`: handle as in `skills/yaml-config/SKILL.md` → Bootstrap");
    expect(skill).toContain("work from YAML the user pastes");
    expect(skill).toContain("a standalone ESPHome container keeps them elsewhere");
  });

  it("relies on the relay refusing every secrets.yaml, in any directory", () => {
    // The skill's promise rests on this transport rule: a final-name pattern,
    // so /config/esphome/secrets.yaml is refused like /config/secrets.yaml.
    expect(filePaths).toContain("/^secrets\\.ya?ml/i");
    expect(filePaths).toMatch(/const name = segments\[segments\.length - 1\]/);
    expect(filePaths).not.toMatch(/"esphome"/);
    expect(skill).toContain("Never read `secrets.yaml` (the relay refuses it), anything under `.esphome/` (build output can hold compiled-in credentials), or `*.bak`");
  });

  it("never puts secrets into chat or into a file", () => {
    expect(skill).toContain("**Secrets never reach chat.** `!secret <name>` stays a reference.");
    expect(skill).toContain("in every shown YAML, explicitly requested YAML included, and say a value was masked");
    expect(skill).toContain("confirmation binds to the stored value behind the mask");
    expect(skill).toContain("Never ask for a credential and never write one");
    expect(skill).toContain("moving an inline value to `!secret` writes the reference only");
    expect(skill).toContain("The user types the encryption key there, never in chat.");
    expect(checks).toContain("A `!secret` name cannot be checked here (the relay never serves `secrets.yaml`)");
  });

  it("edits through the yaml-config file path with snapshot, drift check, preview, and no HA reload", () => {
    expect(skill).toContain("`skills/yaml-config/SKILL.md` → Flow step 1 (read before write)");
    expect(skill).toContain("Preview as the yaml-config File-Change Preview, masked (Safety)");
    expect(skill).toContain("On a reply to that preview: `skills/yaml-config/SKILL.md` → Flow step 3 (auto config snapshot category `yaml` with `{path, content}`, drift check, `write_file` with `.bak`)");
    expect(skill).toContain("Never Home Assistant's `check_config` or a reload: ESPHome files are not HA configuration.");
    expect(skill).toContain("one file per operation");
    expect(skill).toContain("The device changes only when installed again — say so.");
  });

  it("names device-breaking edits before they are confirmed", () => {
    expect(skill).toContain("Name every device-breaking consequence (checks file → Device-Breaking Edits) in the preview.");
    expect(skill).toContain("An edit never removes or replaces `api:`, its encryption key, `ota:`, or the network block of a device that works");
    for (const line of [
      "`esphome:` → `name:` is the hostname the next wireless install looks for",
      "An entity's `name:`: Home Assistant may treat it as a new entity",
      "A new or replaced `api` `encryption` `key`: Home Assistant loses the device",
      "Removing `ota:`: every later install needs USB.",
      "without `ap:` + `captive_portal:`: one mistake means USB.",
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
    ]) {
      expect(checks).toContain(row);
    }
    expect(checks).toContain("Not findings: `api:` → `services:` and `homeassistant.service` still work");
  });

  it("checks pins per chip family with sources", () => {
    expect(checks).toContain("| ESP32 | GPIO34-39 | input only, no internal pull-up/pull-down");
    expect(checks).toContain("| ESP32 | GPIO6-11, GPIO16-17 |");
    expect(checks).toContain("| ESP32 | GPIO0, 2, 5, 12, 15 | strapping pins");
    expect(checks).toContain("ADC2 is also used by Wi-Fi");
    expect(checks).toContain("| ESP8266 | ADC | only GPIO17 (A0/TOUT), 0-1.0 V |");
    expect(checks).toContain("read their notes on esphome.io/components/esp32 before judging a pin there");
  });

  it("keeps compile and flash external with exact next steps", () => {
    expect(skill).toContain("Settings → Apps → ESPHome Device Builder → Open Web UI");
    expect(skill).toContain("**Validate**, then **Install** → Wirelessly");
    expect(skill).toContain("The very first install, or a device without `ota:`, needs USB.");
    expect(skill).toContain("`esphome config <file>.yaml` (validates; secrets redacted), `esphome run <file>.yaml` (compile, upload, logs)");
  });

  it("verifies the device side through find/get after the user installed it", () => {
    expect(skill).toContain("`ha-nova get <entity_id> --references` (`references.entities[]`: `platform`, `state` for the whole device)");
    expect(skill).toContain('`{"type":"config_entries/get","domain":"esphome"}`');
    expect(skill).toContain("**Verify** after the user says it is installed");
    expect(skill).toContain("Never report a change as on the device before the user installed it and the entities came back.");
    expect(skill).toContain("re-pointing consumers is `ha-nova:device`");
  });
});
