// tests/skills/indirect-actuation-contract.test.ts
//
// Split out of service-call-contract.test.ts: the #513 gate contract grew the
// file past the repo's ~400-line ceiling. Same subject, own file, so a future
// change to the gate does not have to be reviewed inside 550 lines of
// unrelated service-call assertions.
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (relative: string): string =>
  readFileSync(resolve(__dirname, "../../", relative), "utf-8");

const skillDoc = read("skills/service-call/SKILL.md");
const contextSkill = read("skills/ha-nova/SKILL.md");
const fallbackSkill = read("skills/fallback/SKILL.md");
const reviewSkill = read("skills/review/SKILL.md");
const mqttSkill = read("skills/mqtt/SKILL.md");
const adminSkill = read("skills/admin/SKILL.md");
const testRunDoc = read("skills/ha-nova/test-run.md");
const assistSkill = read("skills/assist/SKILL.md");
const indirectActuation = read("skills/ha-nova/indirect-actuation.md");
const writeSkill = read("skills/write/SKILL.md");
// Contract docs hard-wrap at ~72 columns, so a pinned sentence must not also
// pin the column it happens to break at.
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("indirect actuation and owning-skill deferrals (#513)", () => {
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
      ] as Array<[string, string]>) {
        // A row may name the service outright or cover its whole domain
        // (`camera.*`), so accept either spelling — otherwise a later PR that
        // consolidates a family silently loses its deferral.
        const domainWildcard = `\`${service.split(".")[0]}.*\``;
        const row = skillDoc
          .split("\n")
          .find(
            (line) =>
              line.startsWith("|") &&
              (line.includes(`\`${service}\``) || line.includes(domainWildcard)),
          );
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
      // The gate's VERDICT disqualifies a Quick-Fix, not the act of running
      // it — a clean consumer scan leaves the correction ordinary.
      expect(flat(reviewSkill)).toContain("must RUN the indirect-actuation gate first");
      expect(flat(reviewSkill)).toContain("What disqualifies it is the gate's VERDICT");
      expect(flat(reviewSkill)).toContain("a clean consumer scan leaves the correction ordinary");
      expect(reviewSkill).toContain("Never Quick-Fix those");
      expect(reviewSkill).toContain("offer to run it as a separate service call");
      // A helper reset is the classic innocent-looking trigger source, so the
      // gate must run — but an unenumerable scan is what blocks the Quick-Fix,
      // never the mere fact of having consulted the gate.
      expect(flat(reviewSkill)).toContain("resetting a desynchronized `input_select`");
      expect(flat(reviewSkill)).toContain(
        "a scan that could not enumerate the consumers — sends it out of Quick-Fix",
      );
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

  it("gives Supervisor lifecycle calls a path the entity flow cannot provide", () => {
    const doc = flat(skillDoc);
    // An App is addressed by slug; there is no entity to read before or after.
    expect(doc).toContain("The target is an App SLUG, not an entity");
    // Probed live: the relay passes only /api/..., and HA answers its
    // /api/hassio/... proxy with 403 — so the slug comes from the update
    // entity's entity_picture, and App state is simply not readable here.
    expect(doc).toContain("The Supervisor API is NOT reachable from here");
    expect(doc).toContain("`entity_picture` is `/api/hassio/addons/<slug>/icon`");
    expect(doc).toContain("App state cannot be verified from here");
    expect(doc).toContain("Never infer success from the service call returning");
    // A host reboot takes the transport with it, so success is unobservable.
    expect(doc).toContain("Never report success: you will not be there to see it");
    expect(doc).toContain("needs physical access to come back");
  });

  it("does not let a targetless reload skip the scan for lack of a target", () => {
    const gate = flat(indirectActuation);
    expect(gate).toContain("A targetless reload of a trigger-source domain");
    expect(gate).toContain("Its effect set is every entity of that domain");
    expect(gate).toContain('"No target" is not evidence of no impact');
  });
});
