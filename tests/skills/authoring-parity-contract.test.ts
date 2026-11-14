import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// Home Assistant 2026.7+ authoring parity (HANO-34 F5, HANO-36 P1-3): write,
// read, and review know the purpose-specific triggers and conditions the
// automation editor builds, their targets, and the 2026.11 naming change.
// Shapes verified with validate_config on the 2026.11 test bed.
const read = (file: string) => readFileSync(file, "utf8");
const bestPractices = read("skills/ha-nova/best-practices.md");

describe("HA 2026.7+ authoring parity", () => {
  it("teaches purpose-specific triggers with targets, options, and exclude", () => {
    expect(bestPractices).toContain("## Triggers, Conditions, and Targets (HA 2026.7+)");
    for (const pin of [
      "trigger: motion.detected",
      "condition: light.is_off",
      "get_triggers_for_target",
      "get_conditions_for_target",
      "Settings of the trigger or condition go under `options:`",
      "Home Assistant refuses `behavior` or `for` there",
      "`each` (default",
      "`any` (default) or `all`",
      "needs a target beside it",
      "never convert them unasked",
    ]) {
      expect(bestPractices, pin).toContain(pin);
    }
  });

  it("teaches 2026.11 names and no polling", () => {
    expect(bestPractices).toContain("`entity_name()`, `device_name()`, `area_name()`, never `friendly_name`");
    expect(bestPractices).toContain("never a `time_pattern` for a value a trigger can watch");
  });

  it("routes write, read, and review to the new shapes", () => {
    expect(read("skills/write/SKILL.md")).toContain("best-practices.md` → Triggers, Conditions, and Targets");
    expect(read("skills/read/SKILL.md")).toContain("`exclude` as \"except …\"");
    const checks = read("skills/review/checks.md");
    expect(checks).toContain("R-32 [HIGH]: Purpose-specific trigger or condition");
    expect(checks).toContain("never flag them as legacy");
    expect(read("skills/review/SKILL.md")).toContain("`item_type: area|floor|label`");
    expect(read("skills/ha-nova/relay-api.md")).toContain('{"type":"get_triggers_for_target","target":{"area_id":["{area_id}"]}}');
  });
});
