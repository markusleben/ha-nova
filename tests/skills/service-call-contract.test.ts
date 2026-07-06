// tests/skills/service-call-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const relayApi = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/relay-api.md"),
  "utf-8",
);
const skillDoc = readFileSync(
  resolve(__dirname, "../../skills/service-call/SKILL.md"),
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

    it("binds service-call confirmation to the active preview", () => {
      expect(skillDoc).toContain("Active Preview Confirmation");
      expect(skillDoc).toContain("Earlier planning consent is draft-only");
      expect(skillDoc).toContain("For batch service calls, show a grouped manifest first");
      expect(skillDoc).toContain("No token confirmation needed for ordinary service calls");
      expect(skillDoc).not.toContain("service calls are reversible actions");
    });

    it("uses stable preview slots before live service execution", () => {
      expect(skillDoc).toContain("Preview the service call with stable localized slots");
      expect(skillDoc).toContain("explicit not-executed-yet line before confirmation");
      expect(skillDoc).toContain("Options block with the execute/apply choice and `cancel`");
      expect(skillDoc).toContain("Do not offer `show yaml` unless the user asks for raw payload details");
    });

    it("treats automation and script execution as explicit live runtime actions", () => {
      expect(skillDoc).toContain("automation.trigger");
      expect(skillDoc).toContain("direct script execution");
      expect(skillDoc).toContain("script.turn_on");
      expect(skillDoc).toContain("Never call them automatically from read, review, write, or post-write verification");
      expect(skillDoc).toContain("whether `skip_condition` is set");
      expect(skillDoc).toContain("Treat `skip_condition: true` as higher risk");
      expect(skillDoc).toContain("confirmation bound to that exact runtime-call preview");
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
