// tests/skills/service-call-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const relayApi = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/relay-api.md"),
  "utf-8",
);
const skillDoc = readFileSync(
  resolve(__dirname, "../../skills/ha-nova-service-call/SKILL.md"),
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

  describe("state delta before preview", () => {
    it("reads current state before showing preview", () => {
      expect(skillDoc).toContain("State delta:");
      expect(skillDoc).toContain("/api/states/{entity_id}");
    });

    it("shows brightness in percent, not raw 0-255", () => {
      expect(skillDoc).toContain("0-255 internally");
      expect(skillDoc).toContain("show delta in %");
    });

    it("distinguishes temperature setpoint from current_temperature sensor", () => {
      expect(skillDoc).toContain("temperature");
      expect(skillDoc).toContain("current_temperature");
      expect(skillDoc).toContain("setpoint");
    });

    it("handles unavailable and unknown states with distinct messages", () => {
      expect(skillDoc).toContain("unavailable");
      expect(skillDoc).toContain("Device is offline or unreachable");
      expect(skillDoc).toContain("unknown");
      expect(skillDoc).toContain("State not yet known");
    });

    it("does not block on state read failure", () => {
      expect(skillDoc).toContain("State read failed");
      expect(skillDoc).toContain("preview without delta");
    });

    it("shows state delta for parameterless state-changing services", () => {
      expect(skillDoc).toContain("inherently changes entity state");
      expect(skillDoc).toContain("parameterless state-changing services");
      expect(skillDoc).toMatch(/toggle.*turn_on.*turn_off/);
    });
  });

  describe("broad-target ambiguity", () => {
    it("requires clarification when area-wide targeting may be too broad", () => {
      expect(skillDoc).toContain("room/area");
      expect(skillDoc).toContain("one clarifying question");
      expect(skillDoc).toContain("before using `area_id`");
      expect(skillDoc).toContain("second blocking ambiguity question");
      expect(skillDoc).toContain("narrower confirmed target");
    });
  });
});
