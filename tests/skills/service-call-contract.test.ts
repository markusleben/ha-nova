// tests/skills/service-call-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const relayApi = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/relay-api.md"),
  "utf-8",
);
const skillDoc = readFileSync(
  resolve(__dirname, "../../.agents/skills/ha-nova-service-call/SKILL.md"),
  "utf-8",
);

describe("service call contract", () => {
  describe("relay-api.md documents service call paths", () => {
    it("documents POST /api/services/{domain}/{service}", () => {
      expect(relayApi).toContain("/api/services/light/turn_on");
    });

    it("documents GET /api/services for listing", () => {
      expect(relayApi).toContain('"method":"GET","path":"/api/services"');
    });

    it("documents return_response query parameter", () => {
      expect(relayApi).toContain("return_response");
    });

    it("documents supported target fields", () => {
      expect(relayApi).toContain("entity_id");
      expect(relayApi).toContain("area_id");
      expect(relayApi).toContain("device_id");
    });
  });

  describe("service-call skill matches relay-api contract", () => {
    it("skill references /api/services path pattern", () => {
      expect(skillDoc).toContain("/api/services/{domain}/{service}");
    });

    it("skill references /api/states for verification", () => {
      expect(skillDoc).toContain("/api/states/{entity_id}");
    });

    it("skill declares entity_id, area_id, device_id targeting", () => {
      expect(skillDoc).toContain("entity_id");
      expect(skillDoc).toContain("area_id");
      expect(skillDoc).toContain("device_id");
    });

    it("skill uses /core endpoint for execution", () => {
      expect(skillDoc).toMatch(/relay.*core/);
    });
  });
});
