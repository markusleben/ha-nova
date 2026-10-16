import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const flat = (s: string) => s.replace(/\s+/g, " ");
const skill = flat(readFileSync("skills/home-map/SKILL.md", "utf8"));
const router = flat(readFileSync("skills/ha-nova/SKILL.md", "utf8"));
const spec = flat(readFileSync("docs/work/2026-10-16-home-map-spec.md", "utf8"));
const mapGo = readFileSync("cli/verb_map.go", "utf8") + readFileSync("cli/verb_map_look.go", "utf8");

describe("home-map skill (needs-analysis TOP 3)", () => {
  it("routes whole-home understanding to home-map and keeps single items with review", () => {
    expect(router).toContain('| "show me my home", "where is X used?", audit or document the whole home, first look | `ha-nova:home-map` |');
    expect(router).toContain("(whole home: `ha-nova:home-map`) | `ha-nova:review`");
  });

  it("never lets the model count", () => {
    expect(skill).toContain("Every number you state comes from a map result of this session");
    expect(skill).toContain("Never add, subtract, or count rows yourself");
    expect(skill).toContain("never estimate");
  });

  it("never claims unused beyond what search/related saw", () => {
    expect(skill).toContain("Always name the `not_checked` surfaces");
    expect(skill).toContain('never "unused" or "safe to delete"');
    expect(mapGo).toContain('var whereUsedNotChecked = []string{"dashboards", "templates", "helper and integration options"}');
  });

  it("keeps persistent notes in Home Assistant's own description field", () => {
    expect(skill).toContain("the \"why\" belongs in the automation's or script's own `description` field");
    expect(skill).toContain("saving goes through `ha-nova:write`");
    expect(skill).toContain("never invent a reason");
  });

  it("documents the first-look contract the onboarding flow calls", () => {
    expect(spec).toContain("`ha-nova map --first-look` — read-only");
    for (const kind of ["battery_low", "automation_unavailable", "integration_problem", "automation_never_triggered", "unavailable_long"]) {
      expect(spec).toContain(`\`${kind}\``);
      expect(skill).toContain(`\`${kind}\``);
      expect(mapGo).toContain(kind.startsWith("automation_") ? `"${kind.slice("automation_".length)}"` : `"${kind}"`);
    }
    expect(skill).toContain("Any non-zero exit: skip the first look without comment.");
  });
});
