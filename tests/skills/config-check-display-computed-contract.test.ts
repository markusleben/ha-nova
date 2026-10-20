import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// Three skill gaps closed for 0.27: a config check before a restart, entity
// and area display fields in organize, and computed answers by one template
// render in entity-discovery. Each rule lives in an on-demand reference; the
// SKILL.md files carry only the pointer.

const flat = (p: string): string => readFileSync(p, "utf8").replace(/\s+/g, " ");

describe("config check before a restart (service-call)", () => {
  const skill = flat("skills/service-call/SKILL.md");
  const routes = flat("skills/service-call/raw-routes.md");

  it("points the run step at the check", () => {
    expect(skill).toContain("before `homeassistant.restart`, `reload_all`, or `hassio.host_reboot`: `raw-routes.md` → Config Check First");
  });

  it("checks first, sends nothing when invalid, and hands off to yaml-config", () => {
    expect(routes).toContain("`ha-nova relay core --method POST --path /api/config/core/check_config`");
    expect(routes).toContain("Before `homeassistant.restart`, `homeassistant.reload_all`, or `hassio.host_reboot`");
    expect(routes).toContain("Run it before the `ha-nova call`");
    expect(routes).toContain("The call keeps its tier and confirmation.");
    expect(routes).toContain("`\"result\": \"invalid\"`: send nothing.");
    expect(routes).toContain("hand off to `ha-nova:yaml-config`");
    expect(routes).toContain("never call it valid");
  });
});

describe("entity and area display fields (organize)", () => {
  const skill = flat("skills/organize/SKILL.md");
  const ref = flat("skills/organize/display-fields.md");

  it("lists the fields and reads the reference first", () => {
    expect(skill).toContain("`aliases`, `icon`, `device_class`, `options`");
    expect(skill).toContain("`aliases`, `labels`, temperature/humidity sensor");
    expect(skill).toContain("read `display-fields.md` first");
  });

  it("keeps the risky parts guarded", () => {
    // HA replaces the whole options object of a domain.
    expect(ref).toContain("the entity's current `options.sensor` object with only `display_precision` changed");
    expect(ref).toContain("only for a `binary_sensor`");
    expect(ref).toContain("A cover's \"Show as\" is refused by the CLI");
    // A unit change rewrites the state value consumers compare against.
    expect(ref).toContain("A unit change changes the state value itself");
    expect(ref).toContain("run the rename consumer check");
    expect(ref).toContain("verify by the state's `unit_of_measurement`");
    expect(ref).toContain("`default_code` holds a PIN");
    expect(ref).toContain("`temperature_entity_id` / `humidity_entity_id` take a `sensor`");
  });
});

describe("computed answers (entity-discovery)", () => {
  const skill = flat("skills/entity-discovery/SKILL.md");
  const ref = flat("skills/entity-discovery/computed-answers.md");

  it("points at one read-only template render", () => {
    expect(skill).toContain("An average, total, or highest value right now: `computed-answers.md`");
    expect(skill).toContain("a `POST /api/template` render is a read");
  });

  it("resolves the set first, keeps counts on find, and shows every input", () => {
    expect(ref).toContain("How MANY things are on or open stays a `ha-nova find` count");
    expect(ref).toContain("Never guess an id, and never stand in an entity of another kind.");
    expect(ref).toContain("ha-nova relay core --method POST --path /api/template --body-file <payload-file>");
    expect(ref).toContain("'unreadable': ns.bad");
    expect(ref).toContain("More than one unit in `inputs`");
    expect(ref).toContain("Every name in `unreadable` is named");
  });
});
