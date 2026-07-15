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
const contextSkill = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/SKILL.md"),
  "utf-8",
);
const fallbackSkill = readFileSync(
  resolve(__dirname, "../../skills/fallback/SKILL.md"),
  "utf-8",
);
const architecture = readFileSync(
  resolve(__dirname, "../../docs/reference/skill-architecture.md"),
  "utf-8",
);
const apiMatrix = readFileSync(
  resolve(__dirname, "../../docs/reference/ha-api-matrix.md"),
  "utf-8",
);
const waveSpec = readFileSync(
  resolve(__dirname, "../../docs/work/2026-07-15-wave-4-coverage-spec.md"),
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

  describe("response services", () => {
    it("teaches the mandatory return_response query parameter", () => {
      // Live-verified: weather.get_forecasts returns 400 without it.
      expect(skillDoc).toContain("## Response services");
      expect(skillDoc).toContain("REQUIRE the `?return_response` query parameter");
      expect(skillDoc).toContain("`.data.body.service_response`");
      expect(skillDoc).toContain("Pure data services (the examples above) are reads — no write confirmation");
      expect(skillDoc).toContain("it never downgrades an action to a read");
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

  describe("custom event and webhook runtime actions", () => {
    it("pins event listener impact, confirmation, and bounded verification", () => {
      expect(skillDoc).toContain("## Custom Events And Webhooks");
      expect(skillDoc).toContain("GET /api/events");
      expect(skillDoc).toContain("`webhook/list` metadata");
      expect(skillDoc).toContain("`trigger: event`");
      expect(skillDoc).toContain("`platform: event`");
      expect(skillDoc).toContain("literal `event_data` filters");
      expect(skillDoc).toContain("unclassified-listener warning");
      expect(skillDoc).toContain("up to three reads over ten seconds");
      expect(skillDoc).toContain("never repeat an event automatically");
      expect(relayApi).toContain('"path":"/api/events/example_event"');
      expect(apiMatrix).toContain("Custom events use an exact user-defined event type");
    });

    it("keeps webhook IDs secret and rejects opaque HTTP 200 as proof", () => {
      expect(skillDoc).toContain("WS `webhook/list`");
      expect(skillDoc).toContain("multiple triggers can share one webhook");
      expect(skillDoc).toContain("never ask the user to paste it");
      expect(skillDoc).toContain("persist it outside client-private scratch storage");
      expect(skillDoc).toContain("unknown IDs, blocked remote calls, and handler errors");
      expect(skillDoc).toContain("never weaken `local_only`");
      expect(relayApi).toContain('{"type":"webhook/list"}');
      expect(relayApi).toContain("multiple automation triggers can share one webhook ID");
      expect(apiMatrix).toContain("opaque HTTP 200; effect verification required");
      expect(waveSpec).toContain("only fresh listener evidence can verify an effect");
    });

    it("moves ownership out of mandatory fallback", () => {
      expect(contextSkill).toContain("fire a custom event or trigger a known JSON webhook");
      expect(contextSkill).toContain('**"Fire the movie_night event"** → `ha-nova:service-call`');
      expect(fallbackSkill).toContain("| Custom events / known JSON webhooks | Covered | service-call |");
      expect(fallbackSkill).not.toContain("### Events / Webhooks -- RELAY-READY");
      expect(architecture).toContain("## Service Call Architecture");
      expect(architecture).toContain("webhook HTTP 200 is deliberately opaque");
    });
  });

  describe("alarm and lock security gates", () => {
    it("pins alarm modes, feature bits, code handoff, and terminal states", () => {
      for (const [service, bit, state] of [
        ["alarm_arm_home", "1", "armed_home"],
        ["alarm_arm_away", "2", "armed_away"],
        ["alarm_arm_night", "4", "armed_night"],
        ["alarm_trigger", "8", "triggered"],
        ["alarm_arm_custom_bypass", "16", "armed_custom_bypass"],
        ["alarm_arm_vacation", "32", "armed_vacation"],
      ]) {
        expect(skillDoc).toContain(`| \`${service}\` | ${bit} | \`${state}\` |`);
      }
      expect(skillDoc).toContain("`code_arm_required` is true and `code_format` is present");
      expect(skillDoc).toContain("`code_format` indicates a code");
      expect(skillDoc).toContain("Never include a `code` field in a Relay payload");
      expect(skillDoc).toContain("finish the action in the Home Assistant UI");
    });

    it("feature-gates lock.open and elevates access-granting actions", () => {
      expect(skillDoc).toContain("`lock.open` requires `supported_features & 1`");
      expect(skillDoc).toContain("`lock.unlock` and `lock.open` take the typed high-consequence confirmation");
      expect(skillDoc).toContain("never auto-retry a security action");
      expect(contextSkill).toContain("unlocking or opening locks");
      expect(fallbackSkill).toContain("| Alarm / lock runtime control | Covered | service-call |");
      expect(fallbackSkill).toContain("Home Assistant UI; codes never enter chat");
      expect(architecture).toContain("feature bits 1/2/4/8/16/32");
    });
  });
});
