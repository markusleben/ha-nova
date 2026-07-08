import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const sceneSkill = readFileSync("skills/scene/SKILL.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const fallbackSkill = readFileSync("skills/fallback/SKILL.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const architectureDoc = readFileSync("docs/reference/skill-architecture.md", "utf8");

describe("scene contract", () => {
  it("enforces the editability guard before any config operation", () => {
    // Integration-owned scenes (hue, deconz, ...) have no HA-side config;
    // only platform "homeassistant" registry entries are editable.
    expect(sceneSkill).toContain("## Editability Guard (critical)");
    expect(sceneSkill).toContain('`platform: "homeassistant"`');
    expect(sceneSkill).toContain("writes must never be attempted");
    expect(sceneSkill).toContain("Resolve the platform BEFORE any config operation");
    expect(sceneSkill).toContain("managed in that integration's own app");
  });

  it("uses unique_id as the config id, never the entity_id slug", () => {
    expect(sceneSkill).toContain("`unique_id` is the scene config id");
    expect(sceneSkill).toContain("never use the entity_id slug as the config id");
    expect(sceneSkill).toContain("/api/config/scene/config/<unique_id>");
  });

  it("treats scene updates as full-document replaces with read-back verification", () => {
    expect(sceneSkill).toContain("the POST replaces the ENTIRE scene config");
    expect(sceneSkill).toContain("never send a partial body");
    expect(sceneSkill).toContain("never drop entities the user did not mention");
    expect(sceneSkill).toContain("survival of unrelated entities");
    // Body id must match the path id (verified live against the config API).
    expect(sceneSkill).toContain("`id` in the body MUST equal the id in the path");
    // The config API reloads scenes on save — no manual reload step.
    expect(sceneSkill).toContain("reloads scenes automatically");
  });

  it("gates deletes on consumer check plus token confirmation and verifies absence", () => {
    expect(sceneSkill).toContain('`{"type":"search/related","item_type":"entity","item_id":"scene.<slug>"}`');
    expect(sceneSkill).toContain("an empty `data` object means no consumers");
    expect(sceneSkill).toContain("`confirm:<token>`");
    expect(sceneSkill).toContain("proceed only when the user types it back exactly");
    expect(sceneSkill).toContain("config GET returns status 404 and the entity is gone");
  });

  it("resolves the created entity_id from the registry instead of guessing the slug", () => {
    // The entity_id derives from name, not from the config id (live-verified;
    // the registry can lag the storage write by a moment).
    expect(sceneSkill).toContain("matching `unique_id` to the config id");
    expect(sceneSkill).toContain("the entity_id derives from `name`, not the id");
    expect(sceneSkill).toContain("never guess the slug");
    expect(sceneSkill).toContain("retry once");
  });

  it("captures attributes deliberately and supports save-current-state scenes", () => {
    expect(sceneSkill).toContain("**Capture attributes deliberately**");
    // All light color modes covered — xy/rgbw/rgbww lights would otherwise
    // replay without their captured color/white-channel data.
    expect(sceneSkill).toContain("`color_temp_kelvin`, `hs_color`, `rgb_color`, `xy_color`, `rgbw_color`, or `rgbww_color`");
    expect(sceneSkill).toContain('an off light exposes no color attributes, capture `state: "off"` only');
    expect(sceneSkill).toContain("never copy measurement or diagnostic attributes");
    expect(sceneSkill).toContain('### Create from current state ("save this room as a scene")');
    expect(sceneSkill).toContain("the entities map IS the live state");
    // Persistence routing references the SSOT instead of restating it.
    expect(sceneSkill).toContain("Persistence Model");
    expect(sceneSkill).toContain("a stored scene is a static capture that never updates itself");
    expect(sceneSkill).toContain("helpers, not scenes");
    // Resilience: orphaned members, duplicate names, upsert protection.
    expect(sceneSkill).toContain("renamed or deleted since capture");
    expect(sceneSkill).toContain("offer removal, never preserve or drop silently");
    expect(sceneSkill).toContain("ask before creating a duplicate");
    expect(sceneSkill).toContain("require a 404 so an existing scene is never silently overwritten");
    expect(sceneSkill).toContain("re-read and re-verify the merge basis before writing (last writer wins)");
    // Scene entity state semantics: timestamp, unknown = never activated.
    expect(sceneSkill).toContain("`unknown` means \"never activated\"");
    // Activation extras stay in service-call, but the skill teaches them.
    expect(sceneSkill).toContain("`scene.turn_on` supports `transition` (lights only)");
    expect(sceneSkill).toContain("`scene.apply`");
  });

  it("names HA Backups as the recovery path and keeps activation out of scope", () => {
    expect(sceneSkill).toContain("Scene writes have no `revert`");
    expect(sceneSkill).toContain("Home Assistant Backups");
    expect(sceneSkill).toContain("use `ha-nova:service-call`");
    // scene.create runtime snapshots stay with write/best-practices.
    expect(sceneSkill).toContain("`scene.create` runtime snapshots");
    expect(sceneSkill).toContain("Persistence Model");
  });

  it("is wired into dispatch, capability map, availability table, and architecture doc", () => {
    expect(contextSkill).toContain("| list, show, read, create, update, delete scenes | `ha-nova:scene` |");
    expect(contextSkill).toContain("| activate a scene | `ha-nova:service-call` |");
    expect(contextSkill).toContain('"Create a scene called Movie Night"** → `ha-nova:scene`');
    expect(contextSkill).toContain('"Activate the scene Movie Night"** → `ha-nova:service-call`');
    expect(fallbackSkill).toContain("| Scenes (storage CRUD) | Covered | scene |");
    expect(writeSafety).toContain("| `scene` | preview + read-back verify | no | HA Backups |");
    expect(architectureDoc).toContain("scene/SKILL.md");
    expect(architectureDoc).toContain("14 independent sub-skills");
  });

  it("teaches file-based relay payloads and localized output slots", () => {
    expect(sceneSkill).toMatch(/--(data-file|body-file|out)\b/);
    expect(sceneSkill).toContain("skills/ha-nova/output-rules.md");
    expect(sceneSkill).toContain("Use stable localized slot labels in this order; omit empty slots.");
  });
});
