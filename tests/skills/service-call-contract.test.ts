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
const reviewSkill = readFileSync(
  resolve(__dirname, "../../skills/review/SKILL.md"),
  "utf-8",
);
const mqttSkill = readFileSync(
  resolve(__dirname, "../../skills/mqtt/SKILL.md"),
  "utf-8",
);
const adminSkill = readFileSync(
  resolve(__dirname, "../../skills/admin/SKILL.md"),
  "utf-8",
);
const testRunDoc = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/test-run.md"),
  "utf-8",
);
const assistSkill = readFileSync(
  resolve(__dirname, "../../skills/assist/SKILL.md"),
  "utf-8",
);
const indirectActuation = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/indirect-actuation.md"),
  "utf-8",
);
const writeSkill = readFileSync(
  resolve(__dirname, "../../skills/write/SKILL.md"),
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
      expect(skillDoc).toContain("No typed confirmation code needed for ordinary service calls");
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
      // The direct fire path needs the same escalation as the stored event:
      // action path — an opaque listener is opaque either way.
      expect(flat(skillDoc)).toContain(
        "an unenumerable listener cannot be shown to be harmless",
      );
      // The confirmation step must not downgrade what step 2 escalated.
      expect(flat(skillDoc)).toContain(
        "only when EVERY listener was enumerable",
      );
      expect(flat(skillDoc)).toContain("unknown impact is not low impact");
      expect(skillDoc).toContain("up to three reads over ten seconds");
      expect(skillDoc).toContain("never repeat an event automatically");
      expect(relayApi).toContain('"path":"/api/events/example_event"');
      expect(apiMatrix).toContain("Custom events use an exact user-defined event type");
    });

    it("keeps webhook IDs secret and rejects opaque HTTP 200 as proof", () => {
      expect(skillDoc).toContain("WS `webhook/list`");
      expect(skillDoc).toContain("never allow its secret-bearing response on stdout");
      expect(skillDoc).toContain("`--out <result-file>` in client-private scratch storage");
      expect(skillDoc).toContain("multiple triggers can share one webhook");
      expect(skillDoc).toContain("never ask the user to paste it");
      expect(skillDoc).toContain("persist it outside client-private scratch storage");
      expect(skillDoc).toContain("unknown IDs, blocked remote calls, and handler errors");
      expect(skillDoc).toContain("never weaken `local_only`");
      expect(relayApi).toContain('{"type":"webhook/list"}');
      expect(relayApi).toContain("Never print the full response to stdout");
      expect(relayApi).toContain("multiple automation triggers can share one webhook ID");
      expect(apiMatrix).toContain("opaque HTTP 200; effect verification required");
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
      expect(skillDoc).toContain("whenever `code_arm_required` is true");
      expect(skillDoc).toContain("even when `code_format` is absent");
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

  describe("owning-skill deferrals cover every gated service family (#513)", () => {
    // A generic service call must never reach a family whose owning skill
    // carries stricter gates. Each row below exists because the owning skill
    // enforces something this flow does not: a feature bitmask, a backup
    // offer, an impact quantification, or a typed confirmation code.
    it("defers every gated family to its owning skill", () => {
      for (const [service, owner] of [
        ["mqtt.publish", "ha-nova:mqtt"],
        ["update.install", "ha-nova:updates"],
        ["camera.snapshot", "ha-nova:camera"],
        ["camera.turn_on", "ha-nova:camera"],
        ["media_player.*", "ha-nova:media"],
        ["notify.*", "ha-nova:notify"],
        ["logger.set_level", "ha-nova:diagnose"],
        ["recorder.purge", "ha-nova:maintenance"],
        ["recorder.purge_entities", "ha-nova:maintenance"],
        ["calendar.create_event", "ha-nova:calendar"],
        ["todo.add_item", "ha-nova:todo"],
        ["backup.create", "ha-nova:backup"],
        ["conversation.process", "ha-nova:assist"],
      ]) {
        const row = skillDoc
          .split("\n")
          .find((line) => line.startsWith("|") && line.includes(`\`${service}\``));
        expect(row, `no deferral row for ${service}`).toBeTruthy();
        expect(row, `${service} must defer to ${owner}`).toContain(owner);
      }
    });

    it("keeps read-only response services and scene activation in this flow", () => {
      expect(skillDoc).toContain("Read-only response services stay here");
      expect(skillDoc).toContain("`calendar.get_events`");
      expect(skillDoc).toContain("`todo.get_items`");
      expect(skillDoc).toContain(
        "`ha-nova:scene` owns scene CRUD, not activation",
      );
    });

    it("keeps Supervisor lifecycle here rather than routing it somewhere weaker", () => {
      // fallback tiers Apps as External, so deferring hassio.* would send the
      // agent to a page that denies the transport works — and fallback has no
      // disruptive tier and no self-amputation rule.
      const rows = skillDoc
        .split("\n")
        .filter((line) => line.startsWith("|") && line.includes("hassio"));
      expect(rows.length).toBeGreaterThanOrEqual(2);
      expect(rows.join(" ")).toContain("disruptive tier");
      // The domain is not a wildcard: restores reboot HA and updates have an
      // owning skill, so both must route away from the generic flow.
      expect(flat(skillDoc)).toContain("The `hassio` domain is NOT a wildcard");
      expect(flat(skillDoc)).toContain("belong to `ha-nova:backup`, which refuses restores outright");
      expect(flat(skillDoc)).toContain("`addon_update` belongs to `ha-nova:updates`");
      expect(flat(skillDoc)).toContain(
        "Refuse outright any call targeting the App that runs this Relay",
      );
      // An App restart takes every device that App serves offline.
      expect(skillDoc).toContain("`hassio.addon_restart`");
      expect(skillDoc).toContain("`hassio.addon_start`");
      expect(flat(skillDoc)).toContain(
        "an MQTT or Z-Wave App takes every device it serves offline while it comes back",
      );
    });
  });

  describe("indirect actuation cannot bypass the high-consequence tier (#513)", () => {
    it("routes every indirect-run service through the shared gate", () => {
      expect(skillDoc).toContain("Indirect actuation gate");
      expect(skillDoc).toContain("actuate entities the request never names");
      expect(skillDoc).toContain("skills/ha-nova/indirect-actuation.md");
      // Every spelling of a script run is covered: main previously gated
      // only "direct script.*", while test-run steers toward script.turn_on.
      // The Flow entry keys on the target domain — a service-name list can
      // always be walked around by an alias. The exhaustive spellings live in
      // the shared gate document, checked below.
      for (const trigger of [
        "decided by the TARGET, not the service name",
        "any call whose target is in `scene`, `script`, or `automation`",
        "`homeassistant.turn_on`/`turn_off`/`toggle` on `script.open_door` is a script run",
        "`input_button.press` and `button.press`",
      ]) {
        expect(skillDoc).toContain(trigger);
      }
      expect(skillDoc).toContain("Ordinary device control does not carry this gate");
      // homeassistant.turn_on with a script target is a script run under
      // another name — a service-name list alone would wave it through.
      const gate = flat(indirectActuation);
      expect(gate).toContain("Classify by the TARGET's domain first");
      expect(gate).toContain("`entity_id: script.open_door` is a script run wearing another name");
      expect(gate).toContain("whatever service was used to get there");
      // Stopping performs none of the member actions — gating it would
      // demand a typed code to make behavior END.
      expect(gate).toContain("Only an actual run expands members");
      expect(gate).toContain("enabling or disabling an AUTOMATION");
      expect(gate).toContain("`automation.trigger` is the one that runs it");
      // A toggle on a RUNNING script stops it; on an idle one it starts it.
      expect(gate).toContain("on a script that is currently running (state `on`): that stops it");
      // Helper domains exist to drive automations; a counter crossing a
      // threshold is a trigger like any other.
      for (const helper of ["`counter`", "`timer`", "`schedule`", "`input_number`"]) {
        expect(gate, `trigger-source list missing ${helper}`).toContain(helper);
      }
    });

    it("binds the tier to the performed action, not the entity or the service", () => {
      // Entity-scoped wording would make LOCKING a door high-consequence.
      expect(skillDoc).toContain(
        "The tier follows the performed action, not the called service",
      );
      expect(skillDoc).toContain("grants physical access or is physically irreversible");
      expect(skillDoc).toContain(
        "A member that only locks, closes, or arms grants nothing and stays ordinary",
      );
      expect(contextSkill).toContain("The tier follows the action that ends up performed");
      expect(contextSkill).toContain("Locking, closing, and arming stay ordinary");
    });

    it("keeps disruptive services distinct from the access-granting tier", () => {
      expect(skillDoc).toContain("Disruptive is not the high-consequence tier");
      // Both limbs of the tier definition survive the split.
      expect(skillDoc).toContain(
        "neither grants physical access nor makes anything physically irreversible",
      );
      expect(flat(testRunDoc)).toContain(
        "a deliberate superset of the context skill's high-consequence confirmation tier",
      );
      expect(flat(testRunDoc)).toContain(
        "Members that grant physical access still take the typed confirmation code",
      );
    });

    it("expands nested runs, every target kind, and the trigger-source direction", () => {
      const gate = flat(indirectActuation);
      expect(gate).toContain("Descend into nested scene, script, and automation calls");
      expect(gate).toContain("union of every `choose`, `if`, `repeat`, and `parallel` branch");
      // A self-imposed depth cap that fails OPEN just moves the bypass one
      // level deeper, so the cap is gone and an unresolved chain escalates.
      expect(gate).toContain("Do not stop early at a self-imposed depth");
      expect(gate).not.toContain("Stop at depth 3");
      expect(gate).toContain(
        "you stopped following a chain before it resolved, for any reason",
      );
      expect(gate).toContain("`area_id`, `device_id`, `floor_id`, and `label_id`");
      expect(gate).toContain("This never rewrites a payload");
      expect(gate).toContain("no stored config exists");
      expect(gate).toContain("`search/related` on the target");
      expect(gate).toContain("helper toggle that another automation answers by unlocking a door");
      // A stored event: action reaches its listeners, which the config being
      // read does not name.
      expect(gate).toContain("A stored `event:` action reaches further");
      expect(gate).toContain("their presence escalates the run on its own");
      // "I could not read it" is not evidence of harmlessness.
      expect(gate).toContain("When the ACTION NAME itself is templated");
      expect(gate).toContain(
        'is not evidence that it is harmless',
      );
    });

    it("states the config-read path the gate depends on", () => {
      // Without an authorized endpoint the gate is unactionable: the skill
      // forbids guessing config IDs and probing unfamiliar endpoints.
      const gate = flat(indirectActuation);
      expect(gate).toContain(
        "/api/config/{scene|script|automation}/config/<config_id>",
      );
      expect(gate).toContain("`config_id` is `attributes.id`");
      expect(gate).toContain("object part of the entity_id");
    });

    it("fails open on unreadable members but never on a failed scan", () => {
      const gate = flat(indirectActuation);
      // Integration-owned scenes are the common case; blanket escalation
      // would demand a typed code for every Hue scene activation.
      expect(gate).toContain("return 404");
      // A templated target hides the entity, not the action: an access-capable
      // service still proves what the run grants.
      expect(gate).toContain("When the stored ACTION is access-capable");
      expect(gate).toContain("Hiding the entity id behind a template does not make it unknown");
      expect(gate).toContain("stay at the ordinary tier and name in the preview");
      expect(gate).toContain(
        "Do not escalate everything you cannot read",
      );
      expect(gate).toContain("a `search/related` scan FAILED");
      expect(gate).toContain("inconclusive, never a clean result");
      expect(gate).toContain(
        "`unlocked`, `open`, and `disarmed` grant access; `locked`, `closed`, and `armed` do not",
      );
    });

    it("keeps the single-confirmation card from becoming the bypass", () => {
      expect(flat(indirectActuation)).toContain(
        "That shortcut never applies to a run this gate placed on the typed tier",
      );
      expect(flat(testRunDoc)).toContain(
        "the single card confirmation never replaces it",
      );
      expect(flat(writeSkill)).toContain(
        "the card confirmation never replaces the typed confirmation code",
      );
      expect(flat(skillDoc)).toContain(
        "if the indirect actuation gate put the run on the typed tier, the card choice never replaces `confirm:<token>`",
      );
    });

    it("gates utterance execution in the skill it defers to", () => {
      expect(flat(assistSkill)).toContain(
        "An utterance is the least enumerable indirect actuation there is",
      );
      expect(assistSkill).toContain("it takes the typed `confirm:<token>`");
      expect(assistSkill).toContain("including the re-run proof after an exposure fix");
    });
  });

  describe("sibling flows restate the access gate (#513 class sweep)", () => {
    it("excludes access-granting corrections from review Quick-Fix", () => {
      expect(reviewSkill).toContain(
        "The corrective call would grant physical access or is physically irreversible",
      );
      expect(reviewSkill).toContain("Never Quick-Fix these");
      expect(reviewSkill).toContain("offer to run it as a separate service call");
      // A helper reset is the classic innocent-looking trigger source.
      expect(flat(reviewSkill)).toContain(
        "any correction that enters the indirect-actuation gate at all",
      );
      expect(flat(reviewSkill)).toContain("resetting a desynchronized `input_select`");
      // Running an automation reaches the same outcome as calling the service.
      expect(reviewSkill).toContain(
        "running a scene, script, or automation that reaches one",
      );
    });

    it("gives command/set-topic publishes the typed tier where the flow reads it", () => {
      // The rule already lived in Safety; the publish bullet is where an
      // agent about to publish actually is, so it has to say it too.
      expect(mqttSkill).toContain(
        "Preview it as an action, not as a message — typed `confirm:<token>`, see Safety",
      );
      expect(mqttSkill).toContain(
        "Retained publishes and command/`set` topics take the typed",
      );
      expect(contextSkill).toContain("retained and command/`set` MQTT publishes");
    });

    it("elevates user-account creation above the ordinary create tier", () => {
      expect(adminSkill).toContain(
        "Creating a user account grants durable system access",
      );
      expect(adminSkill).toContain("not the ordinary create tier");
      expect(contextSkill).toContain("user-account creation");
    });
  });

  it("reads a Template button's own action instead of trusting search/related", () => {
    const gate = flat(indirectActuation);
    // Every spelling of a script run still has to be enumerated somewhere.
    for (const spelling of [
      "`scene.turn_on`, `scene.apply`",
      "`automation.trigger`",
      "`script.<script_id>`, `script.turn_on`, `script.toggle`",
    ]) {
      expect(gate).toContain(spelling);
    }
    // Live registry sample: 131 buttons across 16 integrations, none of them
    // template — blanket escalation would gate every restart/identify button.
    expect(gate).toContain('`pl: "template"` is a');
    expect(gate).toContain("`search/related` returning nothing proves nothing");
    expect(gate).toContain("if you cannot read it, escalate");
    expect(gate).toContain("is an integration button whose behaviour");
  });
});
