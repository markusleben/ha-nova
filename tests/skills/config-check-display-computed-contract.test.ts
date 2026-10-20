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
    expect(skill).toContain("before `homeassistant.restart`, `reload_all`, or `hassio.host_reboot|shutdown`: `raw-routes.md` → Config Check First");
  });

  it("checks first, sends nothing when invalid, and hands off to the file's owner", () => {
    expect(routes).toContain("`ha-nova relay core --method POST --path /api/config/core/check_config`");
    expect(routes).toContain("Before `homeassistant.restart`, `homeassistant.reload_all`, `hassio.host_reboot`, or `hassio.host_shutdown`");
    expect(routes).toContain("Run it before the `ha-nova call`");
    expect(routes).toContain("The call keeps its tier and confirmation.");
    expect(routes).toContain("`\"result\": \"invalid\"`: send nothing.");
    // The broken file decides the fixing skill; yaml-config excludes automations, scripts, scenes.
    expect(routes).toContain("`automations.yaml`/`scripts.yaml` → `ha-nova:write`, `scenes.yaml` → `ha-nova:scene`, any other file → `ha-nova:yaml-config`");
    // A failed check is never "valid", and the user decides; the tier stays.
    expect(routes).toContain("(never call it valid), then ask whether to go ahead anyway");
    expect(routes).toContain("a host reboot or shutdown it does not. The call keeps its tier.");
    expect(routes).toContain("A script or automation run that contains `homeassistant.restart` or `reload_all`: say that step would fail");
  });
});

describe("entity and area display fields (organize)", () => {
  const skill = flat("skills/organize/SKILL.md");
  const ref = flat("skills/organize/display-fields.md");

  it("lists the fields and reads the reference first", () => {
    expect(skill).toContain("`aliases`, `icon`, `device_class`, `options_domain`+`options`");
    expect(skill).toContain("`aliases`, `labels`, temperature/humidity sensor");
    expect(skill).toContain("read `display-fields.md` first");
  });

  it("keeps the risky parts guarded", () => {
    // HA replaces the whole options object of a domain.
    expect(ref).toContain("`options_domain` is the entity's own domain, and only `sensor` or `number`.");
    expect(ref).toContain("Assistant replaces the whole object, so a key left out is reset");
    expect(ref).toContain("not `cloud.*` (for example `disable_2fa`), `conversation`, `switch_as_x`, or `sensor.private`");
    expect(ref).toContain("every action that sets it (`number.set_value`");
    expect(ref).toContain("Restore: write the snapshot's `options.<domain>` object back with the same `options_domain`.");
    expect(ref).toContain("For each label ADDED or REMOVED (compare the current and the new list), run WS `search/related`");
    expect(ref).toContain("narrow until `returned` equals `total`, since a partial list can hide a match");
    expect(ref).toContain("only for a `binary_sensor`");
    expect(ref).toContain("A cover's \"Show as\" is refused by the CLI");
    // A unit change rewrites the state value consumers compare against.
    expect(ref).toContain("A unit change changes the state value itself");
    expect(ref).toContain("run the rename consumer check");
    expect(ref).toContain("verify by the state's `unit_of_measurement`");
    expect(ref).toContain("`default_code` holds a PIN; never ask for one or repeat one a read shows");
    expect(ref).toContain("`temperature_entity_id` / `humidity_entity_id` take a `sensor`");
  });
});

describe("computed answers (entity-discovery)", () => {
  const skill = flat("skills/entity-discovery/SKILL.md");
  const ref = flat("skills/entity-discovery/computed-answers.md");

  it("points at one read-only template render", () => {
    expect(skill).toContain("An average, total, or highest value right now: `computed-answers.md`");
    expect(skill).toContain("A `POST /api/template` render is a read.");
  });

  it("resolves the set first, keeps counts on find, and shows every input", () => {
    expect(ref).toContain("How MANY things are on or open stays a `ha-nova find` count");
    expect(ref).toContain("Never guess an id, and never stand in an entity of another kind.");
    expect(ref).toContain("ha-nova relay core --method POST --path /api/template --body-file <payload-file>");
    expect(ref).toContain("'unreadable': ns.bad");
    // expand() would pull in group members from other places.
    expect(ref).not.toContain("expand(");
    expect(ref).toContain("s.attributes.get('unit_of_measurement')");
    expect(ref).toContain("(ns.ok | max(attribute=1)) if ns.ok else none");
    expect(ref).toContain("Leave out diagnostic sensors");
    // `ec` is an index into entity_categories, not a diagnostic flag.
    expect(ref).toContain("skip only rows where it maps to `diagnostic`");
    expect(ref).toContain("s.entity_id not in keep");
    // The grid sensor is not home use once solar or a battery exists.
    expect(ref).toContain("The grid sensor alone is that only without solar or a battery");
    expect(ref).toContain("carry each AREA's name");
    expect(ref).toContain("More than one unit in `inputs`");
    expect(ref).toContain("Every name in `unreadable` and `skip` is named");
  });
});
