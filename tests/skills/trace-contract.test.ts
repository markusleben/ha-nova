// tests/skills/trace-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const relayApi = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/relay-api.md"),
  "utf-8",
);
const readSkill = readFileSync(
  resolve(__dirname, "../../skills/ha-nova-read/SKILL.md"),
  "utf-8",
);

describe("trace contract", () => {
  describe("relay-api.md documents trace WS types", () => {
    it("documents trace/list with domain and item_id", () => {
      expect(relayApi).toContain('"type":"trace/list"');
      expect(relayApi).toContain('"domain":"automation"');
      expect(relayApi).toContain('"item_id"');
    });

    it("documents trace/get with domain, item_id, and run_id", () => {
      expect(relayApi).toContain('"type":"trace/get"');
      expect(relayApi).toContain('"run_id"');
    });

    it("documents trace response structure", () => {
      expect(relayApi).toContain("trace.trigger");
      expect(relayApi).toContain("trace.condition");
      expect(relayApi).toContain("trace.action");
    });
  });

  describe("ha-nova:read skill references trace types", () => {
    it("skill documents trace/list usage", () => {
      expect(readSkill).toContain("trace/list");
    });

    it("skill documents trace/get usage", () => {
      expect(readSkill).toContain("trace/get");
    });

    it("skill supports both automation and script traces", () => {
      expect(readSkill).toMatch(/automation.*trace|trace.*automation/i);
      expect(readSkill).toMatch(/script.*trace|trace.*script/i);
    });
  });
});
