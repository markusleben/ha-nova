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
describe("indirect actuation and tier classification (#513)", () => {
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
        "any call whose target is in `scene`, `script`, `automation`, or a legacy `group`",
        "including `scene.apply`, which names its entities in an `entities` map",
        "`homeassistant.turn_on`/`turn_off`/`toggle` on `script.open_door` reaches it too",
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
  it("does not let a targetless reload skip the scan for lack of a target", () => {
    const gate = flat(indirectActuation);
    expect(gate).toContain("A targetless reload has NO target, and two families qualify");
    expect(gate).toContain("both enter the gate, exactly like a write to a single trigger source");
    // No instance-specific inventory in executable guidance.
    expect(gate).not.toContain("This instance has 42 scenes");
    expect(gate).toContain("`template.reload`, `rest.reload`");
    expect(gate).toContain("are PLATFORMS whose entities live in `sensor` and");
    expect(gate).toContain("enumerate by `pl` in the entity registry, not by domain");
    expect(gate).toContain("has no registry row at all");
    expect(gate).toContain("the reload escalates instead of reporting a clean scan");
    expect(gate).toContain('"No target" is not evidence of no impact');
  });
  it("does not read an empty consumer scan as an absence of consumers", () => {
    const gate = flat(indirectActuation);
    expect(gate).toContain('A zero-hit scan is NOT "no consumers"');
    expect(gate).toContain("consumer-discovery-preflight.md");
    expect(gate).toContain('"no consumers in the checked families"');
    // The generic unreadable-member fallback must not reach trigger sources:
    // there, the thing that could not be read is the consumer itself.
    // A clean outcome must be reachable: not-checkable only breaks coverage
    // when the family is actually installed, which is the common case being
    // absent — otherwise every helper write lands on the typed tier.
    expect(gate).toContain("only breaks coverage when it is actually PRESENT");
    expect(gate).toContain("that is the ordinary-tier path, and it is the common case");
    expect(gate).toContain("the unread thing IS the consumer");
    expect(gate).toContain("an installed-but-unreadable Node-RED escalates");
  });
  it("does not render the ordinary confirmation menu on the typed tier", () => {
    const doc = flat(skillDoc);
    // A gate that assigns a tier and then offers "apply" has assigned nothing.
    expect(doc).toContain("Unless Safety put this call on the typed tier");
    expect(doc).toContain("the only accepted answer is the exact `confirm:<token>`");
    expect(doc).toContain("including when the tier came from an EXPANDED member");
  });
  it("lets the read state, not the alias, decide whether a call is a run", () => {
    const doc = flat(skillDoc);
    expect(doc).toContain("Entering the gate is not the same as being a run");
    expect(doc).toContain("`homeassistant.turn_off` always stops");
    expect(doc).toContain("`toggle` is the one the state decides");
    expect(doc).toContain("UNLESS the script is already running in `mode: single`");
    expect(flat(indirectActuation)).toContain("the direct service is not exempt");
    // A tier that depends on a live state has to be re-checked at apply time.
    expect(flat(indirectActuation)).toContain("the gate's whole conclusion is re-checked at apply time");
    expect(flat(indirectActuation)).toContain("a script edited during the pause has different members");
    expect(flat(indirectActuation)).toContain("the gate's verdict is part of what was shown");
    expect(flat(indirectActuation)).toContain("EVERY spelling of a start");
    // The Flow entry must not pre-classify the alias as a run.
    expect(doc).not.toContain("on `script.open_door` is a script run");
  });
  it("fails closed on an unresolved cover target", () => {
    const gate = flat(indirectActuation);
    // A blind and a garage door take the same call; only device_class
    // separates them, and it cannot be read before the template resolves.
    expect(gate).toContain("a blind and a garage door take the same call");
    expect(gate).toContain("an unresolved cover target fails CLOSED");
  });
  it("keeps native automation lifecycle services out of the run class", () => {
    const gate = flat(indirectActuation);
    // automation.turn_on enables; only automation.trigger executes.
    expect(gate).toContain("`automation.turn_on|turn_off|toggle`");
    expect(gate).toContain("only flip whether it may fire later");
  });
  it("treats a cycle between fully-read members as resolved", () => {
    const gate = flat(indirectActuation);
    // Escalating every cut cycle puts harmless retry loops on the typed tier.
    expect(gate).toContain("a cycle between fully-read members is resolved, not cut");
    expect(gate).toContain("do not become access-capable by looping");
    expect(gate).toContain("A cycle through a member you could NOT read is still unresolved");
  });
  it("applies the Template-carrier rule to switches as well as buttons", () => {
    const gate = flat(indirectActuation);
    expect(gate).toContain("Any entity on the `template` platform can carry the action itself");
    expect(gate).toContain("the carrier is the platform, not the domain");
    expect(gate).toContain("the ordinary-device-control exemption does NOT apply to it");
    expect(gate).toContain('an arbitrary stored sequence on a `pl: "template"` light');
    expect(gate).toContain("a Template cover its `open_cover`/`close_cover`");
  });
  it("classifies the device-action form, not only the service form", () => {
    const gate = flat(indirectActuation);
    // The automation editor emits domain/type/device_id for a device-picked
    // step; a door unlock built in the UI must not walk past the gate.
    expect(gate).toContain("The DEVICE form (`domain: lock`, `type: unlock`");
    expect(gate).toContain("Classify a device action by its `domain` + `type`");
    expect(gate).toContain("`scene: scene.open_house` is a scene activation written");
    expect(gate).toContain("a shorthand by the domain it names");
    // Legacy YAML still runs, and a blueprint hides its actions entirely.
    expect(gate).toContain("the legacy `service: lock.unlock` and `service_template:`");
    expect(gate).toContain("reads back as `use_blueprint` with inputs, not");
    expect(gate).toContain("It is an unresolved branch, not an empty one");
  });
  it("fails closed for every cover action that can open", () => {
    const gate = flat(indirectActuation);
    expect(gate).toContain("`open_cover`, `toggle` (a closed garage opens), and `set_cover_position`");
    expect(gate).toContain("Only `close_cover` is safe on an unknown target");
  });
  it("carries an owning skill's typed tier through an expansion", () => {
    const gate = flat(indirectActuation);
    expect(gate).toContain("carries that tier into the run");
    expect(gate).toContain("a member whose tier cannot be DETERMINED escalates");
    expect(gate).toContain("Physical access is not the only reason a member is gated");
  });
  it("does not read a local-presence check as proof of no consumer", () => {
    const gate = flat(indirectActuation);
    expect(gate).toContain("Presence detection sees LOCAL installs only");
    expect(gate).toContain('"no locally installed consumer manager"');
    expect(gate).toContain("never call the coverage complete on that basis");
  });

  it("separates an integration scene from an unreadable user-authored one", () => {
    const gate = flat(indirectActuation);
    // Live registry: 42 scenes, all hue/smartthings — blanket escalation here
    // would gate every scene activation on this instance.
    expect(gate).toContain("A scene on an INTEGRATION platform");
    expect(gate).toContain('"bounded" only helps if the bound excludes access');
    expect(gate).toContain("a YAML scene declared without an `id:`");
    expect(gate).toContain("its members are arbitrary and unknown, and it escalates");
    expect(gate).toContain("The same holds for a YAML AUTOMATION without an `id`");
  });

  it("escalates a nested run whose own target is templated", () => {
    const gate = flat(indirectActuation);
    expect(gate).toContain("A nested RUN whose own target is templated");
    expect(gate).toContain("Only a run whose target resolves statically can be expanded");
  });

  it("expands a legacy group before classifying it", () => {
    const gate = flat(indirectActuation);
    expect(gate).toContain("A legacy `group.*` target forwards the call to its members");
    expect(gate).toContain("recursively: a group can contain a group");
  });

  it("states the rules the enumerated cases are instances of", () => {
    const gate = flat(indirectActuation);
    // Each round added another special case; the rules are what decide an
    // unlisted one.
    expect(gate).toContain("the enumeration is illustration, the rules are the contract");
    expect(gate).toContain("The target you see is not always the target that acts");
    expect(gate).toContain("A stored action belongs to whoever authored it");
    expect(gate).toContain("Unread is not empty");
    expect(gate).toContain("`python_script.*`, `shell_command.*` and `rest_command.*` are terminals only");
    expect(gate).toContain("Treat them as unread, not as concrete");
  });
});
