import { existsSync, readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

/**
 * Contract tests for the best-practice patterns adopted from homeassistant-ai/skills.
 *
 * Validates:
 * 1. New reference files exist and contain expected patterns
 * 2. Cross-references between files are valid
 * 3. Payload examples follow the rules they teach
 * 4. Review check ranges are consistent across all files
 * 5. Attribution is present
 */
describe("best-practice patterns contract", () => {
  // ── automation-patterns.md ────────────────────────────────────────────

  describe("automation-patterns.md", () => {
    const file = "skills/ha-nova/automation-patterns.md";
    let content: string;

    it("exists", () => {
      expect(existsSync(file), `Expected ${file} to exist`).toBe(true);
      content = readFileSync(file, "utf8");
    });

    it("has MIT attribution for homeassistant-ai/skills", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("homeassistant-ai/skills");
      expect(content).toContain("MIT License");
    });

    it("covers action flow control patterns", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("choose vs if/then");
      expect(content).toContain("wait_for_trigger vs delay");
      expect(content).toContain("if/then/else");
      expect(content).toContain("default:");
    });

    it("covers trigger ID pattern with choose routing", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("Trigger IDs with choose");
      expect(content).toContain("id:");
      expect(content).toContain("condition: trigger");
    });

    it("covers sun and time_pattern triggers", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("Sun Trigger");
      expect(content).toContain("trigger: sun");
      expect(content).toContain("Time Pattern Trigger");
      expect(content).toContain("time_pattern");
    });

    it("includes canonical motion light example with restart + wait_for_trigger", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("mode: restart");
      expect(content).toContain("wait_for_trigger");
      expect(content).toContain("binary_sensor.motion_hallway");
      expect(content).toContain('to: "off"');
      expect(content).toContain("minutes: 3");
      expect(content).toContain("transition: 3");
    });

    it("includes target structure pattern (M-03)", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("target: Structure");
      expect(content).toContain("target:");
      expect(content).toContain("entity_id vs device_id");
    });

    it("cross-references template-guidelines.md for native conditions", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("template-guidelines.md");
    });

    it("cross-references best-practices.md for Zigbee button patterns", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("best-practices.md");
      expect(content).toContain("Zigbee Button Patterns");
    });
  });

  // ── payload-schemas.md compound examples ──────────────────────────────

  describe("payload-schemas.md compound examples", () => {
    const file = "skills/ha-nova/payload-schemas.md";
    let content: string;

    it("exists", () => {
      expect(existsSync(file)).toBe(true);
      content = readFileSync(file, "utf8");
    });

    it("has MIT attribution", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("homeassistant-ai/skills");
      expect(content).toContain("MIT License");
    });

    it("includes compound example #5: motion light with restart + wait_for_trigger", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("### 5.");
      expect(content).toContain("Motion Light");
      expect(content).toContain("restart");
      expect(content).toContain("wait_for_trigger");
      expect(content).toContain("continue_on_timeout");
    });

    it("includes compound example #6: multi-trigger with trigger.id + choose", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("### 6.");
      expect(content).toContain("trigger.id");
      expect(content).toContain('"id"');
      expect(content).toContain("condition");
      expect(content).toContain("trigger");
      expect(content).toContain('"default"');
    });

    it("includes compound example #7: parallel window/climate", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("### 7.");
      expect(content).toContain("parallel");
      expect(content).toContain("trigger.entity_id");
    });

    it("example #5 uses mode:restart (not single) for motion light", () => {
      content ??= readFileSync(file, "utf8");
      // Extract the JSON block for example 5 — it should use restart mode
      const ex5Match = content.match(/### 5\.[^]*?```json\n([^]*?)```/);
      expect(ex5Match).not.toBeNull();
      const ex5Json = JSON.parse(ex5Match![1]!);
      expect(ex5Json.mode).toBe("restart");
    });

    it("example #6 uses trigger IDs with choose (not state conditions)", () => {
      content ??= readFileSync(file, "utf8");
      const ex6Match = content.match(/### 6\.[^]*?```json\n([^]*?)```/);
      expect(ex6Match).not.toBeNull();
      const ex6Json = JSON.parse(ex6Match![1]!);

      // Triggers should have id fields
      for (const trigger of ex6Json.triggers) {
        expect(trigger.id).toBeDefined();
      }

      // Choose conditions should use condition: trigger
      const chooseAction = ex6Json.actions.find(
        (a: Record<string, unknown>) => a.choose,
      );
      expect(chooseAction).toBeDefined();
      for (const branch of chooseAction.choose) {
        expect(
          branch.conditions.some(
            (c: Record<string, string>) => c.condition === "trigger",
          ),
        ).toBe(true);
      }

      // Should have a default branch (R-09)
      expect(chooseAction.default).toBeDefined();
    });

    it("example #7 uses mode:parallel with max", () => {
      content ??= readFileSync(file, "utf8");
      const ex7Match = content.match(/### 7\.[^]*?```json\n([^]*?)```/);
      expect(ex7Match).not.toBeNull();
      const ex7Json = JSON.parse(ex7Match![1]!);
      expect(ex7Json.mode).toBe("parallel");
      expect(ex7Json.max).toBeGreaterThan(1);
    });

    it("all automation examples use plural keys (triggers/conditions/actions)", () => {
      content ??= readFileSync(file, "utf8");
      // Extract all JSON blocks under Automation Payloads
      const automationSection = content.split("## Script Payloads")[0]!;
      const jsonBlocks = automationSection.match(
        /```json\n([^]*?)```/g,
      );
      expect(jsonBlocks).not.toBeNull();

      for (const block of jsonBlocks!) {
        const json = block.replace(/```json\n/, "").replace(/```$/, "");
        const parsed = JSON.parse(json);
        // Automation payloads must use plural keys
        expect(parsed.trigger).toBeUndefined();
        expect(parsed.condition).toBeUndefined();
        expect(parsed.action).toBeUndefined();
        expect(parsed.triggers).toBeDefined();
      }
    });

    it("all automation examples have explicit mode", () => {
      content ??= readFileSync(file, "utf8");
      const jsonBlocks = [
        ...content.matchAll(/```json\n([^]*?)```/g),
      ];
      for (const [, json] of jsonBlocks) {
        const parsed = JSON.parse(json!);
        // Both automations and scripts should have mode
        expect(
          parsed.mode,
          `Missing mode in example with alias "${parsed.alias}"`,
        ).toBeDefined();
      }
    });

    it("all examples use target: structure (not entity_id in data)", () => {
      content ??= readFileSync(file, "utf8");
      const jsonBlocks = [
        ...content.matchAll(/```json\n([^]*?)```/g),
      ];
      for (const [, json] of jsonBlocks) {
        const parsed = JSON.parse(json!);
        const allActions = [
          ...(parsed.actions ?? []),
          ...(parsed.sequence ?? []),
        ];
        for (const action of allActions) {
          if (action.data?.entity_id && !action.target) {
            throw new Error(
              `Example "${parsed.alias}" has entity_id in data without target: structure`,
            );
          }
        }
      }
    });

    it("example #3 cross-references example #6 for preferred trigger ID approach", () => {
      content ??= readFileSync(file, "utf8");
      const ex3Section = content.match(/### 3\.[^]*?(?=### 4\.)/s);
      expect(ex3Section).not.toBeNull();
      expect(ex3Section![0]).toContain("trigger `id:`");
      expect(ex3Section![0]).toContain("example #6");
    });
  });

  // ── best-practices.md new sections ────────────────────────────────────

  describe("best-practices.md new sections", () => {
    const file = "skills/ha-nova/best-practices.md";
    let content: string;

    it("includes Platform Helpers vs Template Sensors section", () => {
      content = readFileSync(file, "utf8");
      expect(content).toContain("Platform Helpers vs Template Sensors");
      expect(content).toContain("min_max");
      expect(content).toContain("derivative");
      expect(content).toContain("threshold");
      expect(content).toContain("utility_meter");
      expect(content).toContain("statistics");
      expect(content).toContain("history_stats");
    });

    it("marks config-entry helpers with planned relay support", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("Relay support planned");
      expect(content).toContain("#81");
    });

    it("includes Zigbee Button Patterns section", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("Zigbee Button Patterns");
      expect(content).toContain("ZHA");
      expect(content).toContain("Zigbee2MQTT");
      expect(content).toContain("zha_event");
      expect(content).toContain("device_ieee");
    });

    it("ZHA pattern uses event trigger (not device trigger)", () => {
      content ??= readFileSync(file, "utf8");
      const zhaSection = content.match(
        /### ZHA[^]*?(?=### Zigbee2MQTT)/s,
      );
      expect(zhaSection).not.toBeNull();
      expect(zhaSection![0]).toContain("trigger: event");
      expect(zhaSection![0]).toContain("event_type: zha_event");
      expect(zhaSection![0]).toContain("device_ieee");
    });

    it("Z2M pattern uses device or mqtt trigger", () => {
      content ??= readFileSync(file, "utf8");
      const z2mSection = content.match(/### Zigbee2MQTT[^]*/s);
      expect(z2mSection).not.toBeNull();
      expect(z2mSection![0]).toContain("trigger: device");
      expect(z2mSection![0]).toContain("trigger: mqtt");
    });

    it("includes ZHA vs Z2M comparison table", () => {
      content ??= readFileSync(file, "utf8");
      expect(content).toContain("ZHA vs Z2M Quick Reference");
      expect(content).toContain("Trigger type");
      expect(content).toContain("Persistent ID");
    });

    it("has MIT attribution for adopted sections", () => {
      content ??= readFileSync(file, "utf8");
      // Both new sections should have attribution comments
      const attributionCount = (
        content.match(/homeassistant-ai\/skills.*MIT License/g) || []
      ).length;
      expect(attributionCount).toBeGreaterThanOrEqual(2);
    });
  });

  // ── P-05 check consistency ────────────────────────────────────────────

  describe("P-05 check consistency", () => {
    it("P-05 exists in checks.md", () => {
      const checks = readFileSync(
        "skills/review/checks.md",
        "utf8",
      );
      expect(checks).toContain("P-05");
      expect(checks).toContain("device_id");
      expect(checks).toContain("Zigbee Button Patterns");
    });

    it("review SKILL.md references P-01..P-05 range", () => {
      const reviewSkill = readFileSync(
        "skills/review/SKILL.md",
        "utf8",
      );
      expect(reviewSkill).toContain("P-01..P-05");
    });

    it("review-agent.md references P-01..P-05 range", () => {
      const reviewAgent = readFileSync(
        "skills/ha-nova/agents/review-agent.md",
        "utf8",
      );
      expect(reviewAgent).toContain("P-01..P-05");
    });

    it("skill-architecture.md references P-01..P-05 range", () => {
      const arch = readFileSync(
        "docs/reference/skill-architecture.md",
        "utf8",
      );
      expect(arch).toContain("P-01..P-05");
    });
  });

  // ── Cross-reference integrity ─────────────────────────────────────────

  describe("cross-reference integrity", () => {
    it("automation-patterns.md is listed in skill-architecture.md skill tree", () => {
      const arch = readFileSync(
        "docs/reference/skill-architecture.md",
        "utf8",
      );
      expect(arch).toContain("automation-patterns.md");
    });

    it("automation-patterns.md is referenced in write skill", () => {
      const writeSkill = readFileSync(
        "skills/write/SKILL.md",
        "utf8",
      );
      expect(writeSkill).toContain("automation-patterns.md");
    });

    it("all files referenced in automation-patterns.md exist", () => {
      const content = readFileSync(file("automation-patterns.md"), "utf8");
      const refs = extractMdFileRefs(content);
      for (const ref of refs) {
        expect(
          existsSync(ref),
          `Referenced file does not exist: ${ref}`,
        ).toBe(true);
      }
    });

    it("all files referenced in write skill References exist", () => {
      const content = readFileSync(
        "skills/write/SKILL.md",
        "utf8",
      );
      const refsSection = content.match(/## References[^]*/s);
      expect(refsSection).not.toBeNull();
      const refs = extractMdFileRefs(refsSection![0]);
      for (const ref of refs) {
        expect(
          existsSync(ref),
          `Referenced file does not exist: ${ref}`,
        ).toBe(true);
      }
    });
  });

  // ── README attribution ────────────────────────────────────────────────

  describe("README attribution", () => {
    it("acknowledges homeassistant-ai/skills in README", () => {
      const readme = readFileSync("README.md", "utf8");
      expect(readme).toContain("homeassistant-ai/skills");
      expect(readme).toContain("@sergeykad");
      expect(readme).toContain("@julienld");
    });
  });

  // ── Pattern correctness: examples follow their own rules ──────────────

  describe("pattern correctness", () => {
    it("motion light examples use restart mode (not single)", () => {
      // Check both automation-patterns.md and payload-schemas.md
      const patterns = readFileSync(file("automation-patterns.md"), "utf8");
      const schemas = readFileSync(file("payload-schemas.md"), "utf8");

      // automation-patterns.md motion light
      const motionPatternMatch = patterns.match(
        /Motion light.*?mode: restart/s,
      );
      expect(
        motionPatternMatch,
        "automation-patterns.md motion light should use mode: restart",
      ).not.toBeNull();

      // payload-schemas.md Example #5
      const ex5Match = schemas.match(/### 5\.[^]*?```json\n([^]*?)```/);
      expect(ex5Match).not.toBeNull();
      const ex5Json = JSON.parse(ex5Match![1]!);
      expect(ex5Json.mode).toBe("restart");
    });

    it("wait_for_trigger examples always include timeout", () => {
      const patterns = readFileSync(file("automation-patterns.md"), "utf8");
      const schemas = readFileSync(file("payload-schemas.md"), "utf8");

      // Every wait_for_trigger should have a nearby timeout
      for (const [label, content] of [
        ["automation-patterns.md", patterns],
        ["payload-schemas.md", schemas],
      ] as const) {
        const waitMatches = [
          ...content.matchAll(/wait_for_trigger/g),
        ];
        if (waitMatches.length > 0) {
          expect(
            content,
            `${label} has wait_for_trigger but should also contain timeout`,
          ).toContain("timeout");
        }
      }
    });

    it("choose examples with 3+ branches include default", () => {
      const schemas = readFileSync(file("payload-schemas.md"), "utf8");
      const jsonBlocks = [...schemas.matchAll(/```json\n([^]*?)```/g)];

      for (const [, json] of jsonBlocks) {
        const parsed = JSON.parse(json!);
        const allActions = [
          ...(parsed.actions ?? []),
          ...(parsed.sequence ?? []),
        ];
        for (const action of allActions) {
          if (action.choose && action.choose.length >= 3) {
            expect(
              action.default,
              `Choose with ${action.choose.length} branches in "${parsed.alias}" should have default`,
            ).toBeDefined();
          }
        }
      }
    });

    it("numeric_state example uses native trigger (not template condition)", () => {
      const schemas = readFileSync(file("payload-schemas.md"), "utf8");
      const ex4Match = schemas.match(/### 4\.[^]*?```json\n([^]*?)```/);
      expect(ex4Match).not.toBeNull();
      const ex4Json = JSON.parse(ex4Match![1]!);

      // Should use numeric_state trigger
      expect(
        ex4Json.triggers.some(
          (t: Record<string, string>) => t.trigger === "numeric_state",
        ),
      ).toBe(true);
      // Should NOT use a template condition for numeric comparison
      expect(
        ex4Json.conditions.some(
          (c: Record<string, string>) =>
            c.condition === "template" && c.value_template?.includes("above"),
        ),
      ).toBe(false);
    });
  });
});

// ── Helpers ───────────────────────────────────────────────────────────

function file(name: string): string {
  return `skills/ha-nova/${name}`;
}

/** Extract relative .md file paths from skill cross-references like `skills/foo/bar.md` */
function extractMdFileRefs(content: string): string[] {
  const refs = new Set<string>();
  const matches = content.matchAll(
    /`((?:skills|docs)\/[a-z0-9_\-/]+\.md)`/g,
  );
  for (const [, ref] of matches) {
    refs.add(ref!);
  }
  return [...refs];
}
