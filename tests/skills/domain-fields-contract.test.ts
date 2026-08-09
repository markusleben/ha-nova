// tests/skills/domain-fields-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const domainFields = readFileSync(
  resolve(__dirname, "../../skills/service-call/domain-fields.md"),
  "utf-8",
);
const serviceCall = readFileSync(
  resolve(__dirname, "../../skills/service-call/SKILL.md"),
  "utf-8",
);
const mediaSkill = readFileSync(
  resolve(__dirname, "../../skills/media/SKILL.md"),
  "utf-8",
);
const cameraSkill = readFileSync(
  resolve(__dirname, "../../skills/camera/SKILL.md"),
  "utf-8",
);
const sceneSkill = readFileSync(
  resolve(__dirname, "../../skills/scene/SKILL.md"),
  "utf-8",
);
// Reference docs hard-wrap, so a pinned sentence must not pin its wrap column.
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("per-domain service depth (#530)", () => {
  it("is reachable from the skill without loading it for every call", () => {
    expect(serviceCall).toContain("skills/service-call/domain-fields.md");
    expect(flat(serviceCall)).toContain(
      "read the section for the domain you are calling",
    );
    expect(flat(domainFields)).toContain(
      "do not load this file for a domain it does not list",
    );
  });

  it("states the values-come-from-attributes rule the service schema cannot give", () => {
    expect(flat(serviceCall)).toContain("`/api/services` gives field NAMES");
    expect(flat(serviceCall)).toContain(
      "Never carry a value over from another device",
    );
    // Silent-drop is the failure mode that makes feature bits load-bearing.
    expect(flat(serviceCall)).toContain("dropped silently rather than rejected");
  });

  it("teaches color_temp_kelvin instead of the removed mireds field", () => {
    const gate = flat(domainFields);
    expect(gate).toContain("`color_temp_kelvin` (Kelvin)");
    expect(gate).toContain("higher Kelvin means cooler light");
    expect(gate).toContain("a converted number is not interchangeable");
    expect(domainFields).not.toContain("(mireds)");
    // The scene skill already used Kelvin; the two must not diverge again.
    expect(sceneSkill).toContain("color_temp_kelvin");
  });

  it("covers vacuum area cleaning with its bit, prerequisite, and start trap", () => {
    const gate = flat(domainFields);
    expect(gate).toContain("`vacuum.clean_area` (Home Assistant 2026.3+, feature bit 16384)");
    expect(gate).toContain("`cleaning_area_id`, a list of Home Assistant AREA ids");
    expect(gate).toContain("mapped to those areas once in the entity settings");
    expect(gate).toContain("the modern vacuum entity has no on/off");
    expect(gate).toContain("`returning` is transitional on the way to `docked`");
  });

  it("treats cover tilt as its own axis with its own bits and attribute", () => {
    const gate = flat(domainFields);
    for (const bit of [
      "`open_cover_tilt` (bit 16)",
      "`close_cover_tilt` (32)",
      "`stop_cover_tilt` (64)",
      "`set_cover_tilt_position` (128",
    ]) {
      expect(gate).toContain(bit);
    }
    expect(gate).toContain("verify `current_position`/ `current_tilt_position`");
  });

  it("splits climate setpoints and mode families", () => {
    const gate = flat(domainFields);
    expect(gate).toContain("`target_temp_high` + `target_temp_low`");
    expect(gate).toContain("required together when the entity targets a range");
    expect(gate).toContain("There is no `aux_heat` any more");
    for (const list of ["`hvac_modes`", "`preset_modes`", "`fan_modes`", "`swing_modes`"]) {
      expect(gate).toContain(list);
    }
  });

  it("gives fan levels, humidifier and water-heater verification quirks, and siren bits", () => {
    const gate = flat(domainFields);
    expect(gate).toContain("`percentage_step`");
    expect(gate).toContain("3 x 25 = 75");
    // Setpoint/sensor twins are the classic wrong-attribute bug.
    expect(gate).toContain("`humidity` is the setpoint, `current_humidity` the sensor reading");
    expect(gate).toContain("The entity STATE is the operation mode");
    expect(gate).toContain("`tone` (bit 4");
    expect(gate).toContain("`volume_level` (bit 8), and `duration` (bit 16)");
  });

  it("wires media search, queue placement, and playback modes into the flow", () => {
    expect(mediaSkill).toContain("`media_player/search_media` with `search_query` (bit 4194304)");
    expect(mediaSkill).toContain("`media_source/search_media`");
    expect(mediaSkill).toContain("`can_search` is true");
    expect(mediaSkill).toContain("`enqueue: add | next | play | replace` (bit 2097152)");
    expect(mediaSkill).toContain("`repeat: off | all | one`, bit 262144 — not the grouping bit");
    expect(mediaSkill).toContain("`media_player.shuffle_set`");
  });

  it("covers camera casting and the motion-detection toggle it used to disclaim", () => {
    expect(cameraSkill).toContain("`camera.play_stream`");
    expect(cameraSkill).toContain("Verify on the RECEIVER's state");
    expect(cameraSkill).toContain("`camera.enable_motion_detection`");
    // The scope line must no longer read as excluding the toggle itself.
    expect(cameraSkill).toContain("toggling its motion detection");
    expect(cameraSkill).toContain("configuring the detection pipeline itself");
  });

  it("names the writable capture attributes for the setpoint-twin domains", () => {
    expect(sceneSkill).toContain("fan `percentage`/`preset_mode`");
    expect(sceneSkill).toContain("humidifier `humidity`/`mode`");
    expect(sceneSkill).toContain("never its `current_*` sensor twin");
  });

  it("stores a partial cover as current_position, not the service parameter", () => {
    // HA's cover reproduce_state reads CURRENT_POSITION from the stored
    // attributes and passes it as the POSITION service parameter. Storing
    // `position` in the scene loses the partial target and the cover just
    // opens fully on activation.
    expect(sceneSkill).toContain(
      "a cover stores `current_position` / `current_tilt_position`",
    );
    expect(sceneSkill).toContain("the cover then just opens fully");
    expect(sceneSkill).toContain(
      "A scene stores STATE attributes, not service parameters",
    );
  });

  it("does not invent a toggle-tilt feature bit", () => {
    // toggle_cover_tilt is registered against OPEN_TILT|CLOSE_TILT; there is
    // no TOGGLE_TILT member in CoverEntityFeature.
    expect(domainFields).toContain(
      "gates on OPEN_TILT or\n  CLOSE_TILT rather than a bit of its own",
    );
    expect(domainFields).not.toMatch(/toggle[^.]*\b512\b/i);
  });
});
