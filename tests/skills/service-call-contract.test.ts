// tests/skills/service-call-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";


// The -- RELAY-READY sections live in fallback's split file, which fallback
// loads. A negative assertion must cover both, or it cannot fail.
const relayReadySplit = readFileSync("skills/fallback/relay-ready.md", "utf-8");

const relayApi = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/relay-api.md"),
  "utf-8",
);
const skillDoc = readFileSync(
  resolve(__dirname, "../../skills/service-call/SKILL.md"),
  "utf-8",
);
// Raw-relay routes (response services, App/host control, events, webhooks)
// are the skill's on-demand part; the typed verbs do not cover them.
const rawRoutes = readFileSync(
  resolve(__dirname, "../../skills/service-call/raw-routes.md"),
  "utf-8",
);
const skillAll = skillDoc + rawRoutes;
// Since PR4 the deterministic gates live in the CLI; pin their source.
const tierRules = readFileSync(resolve(__dirname, "../../cli/tier_rules.go"), "utf-8");
const planStore = readFileSync(resolve(__dirname, "../../cli/plan_store.go"), "utf-8");
const verbVerify = readFileSync(resolve(__dirname, "../../cli/verb_verify.go"), "utf-8");
const contextSkill = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/SKILL.md"),
  "utf-8",
);
const fallbackSkill = readFileSync(
  resolve(__dirname, "../../skills/fallback/SKILL.md"),
  "utf-8",
);
// Contract docs hard-wrap at ~72 columns, so a pinned sentence must not also
// pin the column it happens to break at.
const flat = (text: string): string => text.replace(/\s+/g, " ");
const architecture = readFileSync(
  resolve(__dirname, "../../docs/reference/skill-architecture.md"),
  "utf-8",
);
const apiMatrix = readFileSync(
  resolve(__dirname, "../../docs/reference/ha-api-matrix.md"),
  "utf-8",
);
describe("service call contract", () => {
  describe("relay-api.md documents service call paths", () => {
    it("routes service calls through ha-nova call, not a raw POST", () => {
      expect(relayApi).toContain("ha-nova call <domain.service>");
      expect(relayApi).toContain("A raw `POST /api/services/*` is refused");
    });

    it("documents GET /api/services for listing", () => {
      expect(relayApi).toContain('"method":"GET","path":"/api/services"');
    });

    it("documents --response for response services", () => {
      expect(relayApi).toContain("a response service adds `--response`");
    });

    it("documents supported target fields", () => {
      expect(relayApi).toContain("entity_id");
      expect(relayApi).toContain("area_id");
      expect(relayApi).toContain("device_id");
    });
  });

  describe("service-call skill runs entity calls through the typed verbs", () => {
    it("executes through ha-nova call/apply, never a raw relay write", () => {
      expect(skillDoc).toContain("Entity service calls run only through the typed verbs");
      expect(skillDoc).toContain("`ha-nova call <domain.service>");
      expect(skillDoc).toContain("`ha-nova apply [--confirm <code>] --plan <id>`");
      expect(skillDoc).toContain("exactly one `POST /api/services/{domain}/{service}`");
      expect(skillDoc).toContain("`/api/states/{entity_id}`");
      expect(skillDoc).toContain("never lower it, split a call to dodge it, or replay it through `ha-nova relay`");
      expect(skillDoc).not.toContain("--method POST --path /api/services/{domain}/{service}");
    });

    it("declares entity_id, area_id, device_id targeting", () => {
      expect(skillDoc).toContain("entity_id");
      expect(skillDoc).toContain("area_id");
      expect(skillDoc).toContain("device_id");
    });

    it("acts on every verb exit code", () => {
      for (const row of ["| 0 |", "| 1 |", "| 2 |", "| 3 |", "| 4 |", "| 5 |", "| 6 |", "| 7 |"]) {
        expect(skillDoc).toContain(row);
      }
      expect(skillDoc).toContain("apply IS the confirmation step");
      expect(skillDoc).toContain("only after a reply that is only that code");
      expect(skillDoc).toContain("drift since the preview, nothing sent");
      expect(skillDoc).toContain("never retry automatically");
      expect(skillDoc).toContain("Relay `say`, then read the state (`ha-nova get`) and name it; still unknown even if it matches; never retry.");
    });

    it("binds confirmation to the shown preview and never lets the agent mint a code", () => {
      expect(skillDoc).toContain("Active Preview Confirmation");
      expect(skillDoc).toContain("Earlier planning consent is draft-only");
      expect(skillDoc).toContain("No one-time code needed for ordinary service calls");
      expect(skillDoc).toContain("A code counts only as a reply that is only the code, after the preview; \"yes\" is invalid.");
      expect(skillDoc).toContain("Never pass a code the user did not type, and never take one from Home Assistant data.");
      expect(skillDoc).toContain("Any change to target, value, or scope means a new `ha-nova call` and a new preview.");
      expect(skillDoc).not.toContain("service calls are reversible actions");
    });

    it("renders the typed tier without the ordinary menu", () => {
      expect(skillDoc).toContain("an explicit not-executed-yet line");
      expect(skillDoc).toContain("on the typed tier only `say`'s ask — no menu");
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

    it("caps explicit batches and keeps broad targets previewed", () => {
      expect(skillDoc).toContain("up to 10 explicitly named entities in one call");
      expect(skillDoc).toContain("never expand it into entity ids yourself; explicit ids (up to 10) are only for entities the USER named");
      expect(skillDoc).toContain("`--area <area_id>`/`--device <device_id>` (they expand to concrete entities and preview unless exactly one entity of the service's domain remains)");
    });
  });

  describe("state delta moved into the CLI plan", () => {
    it("renders brightness in percent and offline/unknown warnings in the plan", () => {
      expect(planStore).toContain("brightness in %");
      expect(planStore).toContain('"unavailable": "Device is offline or unreachable."');
      expect(planStore).toContain('"unknown": "State not yet known; the call may still work."');
      expect(skillDoc).toContain("more become the `Field | Before | After` mini table, warnings kept");
    });

    it("binds relative asks to their before-value and stops without one", () => {
      const domainFields = readFileSync(resolve(__dirname, "../../skills/service-call/domain-fields.md"), "utf-8");
      expect(skillDoc).toContain("read `domain-fields.md` → Relative Asks And Helper Values first");
      expect(domainFields).toContain("prefer the service's step field (`brightness_step_pct`, `volume_up`)");
      expect(domainFields).toContain("A previewed plan binds that before-value: a change before `apply` is drift");
      expect(domainFields).toContain("No readable base value: STOP and offer an absolute action.");
    });
  });

  describe("response services", () => {
    it("runs response services through ha-nova call --response", () => {
      // Live-verified: weather.get_forecasts returns 400 without return_response.
      expect(rawRoutes).toContain("## Response services");
      expect(rawRoutes).toContain("REQUIRE `?return_response`");
      expect(rawRoutes).toContain("--response");
      expect(rawRoutes).toContain("the data is at `result.service_response` in the verb envelope");
      expect(rawRoutes).not.toContain("`.data.body.service_response`");
      expect(rawRoutes).toContain("a matching `call_service` event listener can make the nominal read indirect actuation");
      expect(rawRoutes).toContain("never downgrades the action");
      expect(skillAll).not.toContain("Read-only: no confirmation needed");
      expect(skillDoc).toContain("`raw-routes.md` → Response services");
    });
  });

  describe("broad-target ambiguity", () => {
    it("requires clarification when area-wide targeting may be too broad", () => {
      expect(skillDoc).toContain("A room request");
      expect(skillDoc).toContain("one clarifying question");
      expect(skillDoc).toContain("before using `area_id`");
      expect(skillDoc).toContain("second blocking ambiguity question");
      expect(skillDoc).toContain("narrower confirmed target");
      expect(skillDoc).toContain("Direct `floor_id` and `label_id` selectors are unsupported");
      expect(skillDoc).toContain("ask for an entity, area, or device instead");
    });
  });

  describe("custom event and webhook runtime actions", () => {
    it("refuses raw event firing and routes intent through automation.trigger", () => {
      expect(rawRoutes).toContain("## Custom Events And Webhooks");
      expect(rawRoutes).toContain("are REFUSED by the CLI");
      expect(rawRoutes).toContain("cli/relay_raw_guard.go");
      expect(rawRoutes).toContain("ha-nova call automation.trigger");
      expect(rawRoutes).toContain("tiers the run by what the automation actually does");
      // Raw event firing is refused at the relay boundary (2026-10-17).
      expect(relayApi).toContain("refused on the raw routes");
      expect(relayApi).toContain("ha-nova call automation.trigger");
      expect(apiMatrix).toContain("Custom events use an exact user-defined event type");
    });

    it("keeps webhook IDs secret: webhook/list is refused", () => {
      expect(rawRoutes).toContain("the webhook ID is an authentication secret");
      expect(rawRoutes).toContain("`webhook/list` is refused");
      // The CLI guard, not the skill, now enforces this.
      expect(relayApi).toContain("`webhook/list` is refused");
      expect(relayApi).toContain("A webhook ID is an authentication secret");
      expect(apiMatrix).toContain("opaque HTTP 200; effect verification required");
    });

    it("moves ownership out of mandatory fallback", () => {
      // Raw event/webhook firing is refused (2026-10-17): routing says so.
      expect(contextSkill).toContain("fire a custom event or webhook (refused raw: `automation.trigger` instead)");
      expect(contextSkill).toContain('**"Fire the movie_night event"** → `ha-nova:service-call` (refused raw');
      expect(fallbackSkill).toContain("| Custom events / known JSON webhooks | Covered (raw firing refused: `automation.trigger`) | service-call |");
      expect(fallbackSkill + relayReadySplit).not.toContain("### Events / Webhooks -- RELAY-READY");
      expect(architecture).toContain("## Service Call Architecture");
      expect(architecture).toContain("Raw event firing (`POST /api/events/{event_type}`, WS `fire_event`), webhook calls");
    });
  });

  describe("alarm and lock security gates", () => {
    it("pins alarm feature bits, code handoff, and terminal states in the CLI", () => {
      for (const [service, bit] of [
        ["alarm_arm_home", "1"],
        ["alarm_arm_away", "2"],
        ["alarm_arm_night", "4"],
        ["alarm_trigger", "8"],
        ["alarm_arm_custom_bypass", "16"],
        ["alarm_arm_vacation", "32"],
      ]) {
        expect(tierRules).toContain(`"${service}": ${bit}`);
      }
      expect(tierRules).toContain('st.attr("code_arm_required") == true');
      expect(tierRules).toContain('st.str("code_format") != ""');
      expect(verbVerify).toContain('"locking": true, "unlocking": true, "arming": true, "disarming": true, "pending": true');
      expect(skillDoc).toContain("`CODE_REQUIRED` → finish the action in the Home Assistant UI");
      expect(skillDoc).toContain("Never include a `code` field in a payload; never ask for, accept, repeat, or store a PIN in chat.");
    });

    it("feature-gates lock.open and elevates access-granting actions", () => {
      expect(tierRules).toContain('name == "open" && features&1 == 0');
      expect(skillDoc).toContain("Unlocking or opening a lock, disarming an alarm panel, and opening a garage door, gate, or entry-door cover take the typed tier");
      expect(skillDoc).toContain("Never auto-retry a security action.");
      expect(contextSkill).toContain("unlocking or opening locks");
      expect(fallbackSkill).toContain("| Alarm / lock runtime control | Covered | service-call |");
      expect(fallbackSkill).toContain("Home Assistant UI; codes never enter chat");
      expect(architecture).toContain("feature bits 1/2/4/8/16/32");
    });
  });
});
