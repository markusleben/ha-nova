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
const indirectActuation = read("skills/ha-nova/indirect-actuation.md");
// Contract docs hard-wrap at ~72 columns, so a pinned sentence must not also
// pin the column it happens to break at.
const flat = (text: string): string => text.replace(/\s+/g, " ");
describe("indirect actuation and tier classification (#513)", () => {
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
    expect(gate).toContain("Integration buttons split in two");
    expect(gate).toContain("Where the USER defines it in the device's own configuration");
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
  it("never lets a preview-time state lower the tier", () => {
    const doc = flat(skillDoc);
    expect(doc).toContain("Entering the gate is not the same as being a run");
    // A stop is the ONE alias no timing can turn into a start, so it skips
    // member expansion without skipping consumer classification.
    expect(doc).toContain("`homeassistant.turn_off` on a script or automation never starts its stored members, but still scans consumers");
    expect(doc).toContain("Everything that MAY start a script");
    expect(doc).toContain("expands and classifies regardless of the observed state");
    expect(flat(indirectActuation)).toContain("Only a pure stop (`turn_off`) expands nothing");
    // The apply-time re-check stays, but it is no longer what keeps the tier
    // honest — it cannot be, because it lands after the confirmation.
    expect(flat(indirectActuation)).toContain("re-checked at apply time");
    expect(flat(indirectActuation)).toContain("AND the consumer scan itself");
    expect(flat(indirectActuation)).toContain("cannot discover one that did not exist yet");
    expect(flat(indirectActuation)).toContain("the gate's verdict is part of what was shown");
    expect(flat(indirectActuation)).toContain("Every spelling that may start one");
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
    expect(gate).toContain("a Template switch its `turn_on`/`turn_off`");
    expect(gate).toContain("a Template cover its `open_cover`/`close_cover`");
  });
  it("keeps ordinary switches out of the stored trigger-source family", () => {
    const triggerSources = indirectActuation.match(
      /- writes to a trigger source[\s\S]*?- a test utterance/,
    )?.[0];
    expect(triggerSources).toBeDefined();
    expect(triggerSources ?? "").not.toContain("`switch`");
    expect(flat(indirectActuation)).toContain(
      "Ordinary device control — lights, switches, media, comfort climate, non-access covers",
    );
    expect(flat(indirectActuation)).toContain("a Home Assistant state trigger accepts any entity");
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
    expect(gate).toContain("call the read-only `blueprint/substitute` shape");
    expect(gate).toContain("traverse the expanded actions");
    expect(gate).toContain("A failed substitution is unresolved and escalates");
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
    expect(gate).toContain("Templates are their own not-checkable family");
    expect(gate).toContain("Presence detection also sees LOCAL installs only");
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

  it("expands legacy and modern domain groups before classifying them", () => {
    const gate = flat(indirectActuation);
    expect(gate).toContain("A legacy `group.*` target or a modern domain group");
    expect(gate).toContain("`attributes.entity_id` member array");
    expect(gate).toContain("classify the members recursively");
  });

  it("states the rules the enumerated cases are instances of", () => {
    const gate = flat(indirectActuation);
    // Each round added another special case; the rules are what decide an
    // unlisted one.
    expect(gate).toContain("the enumeration is illustration, the rules are the contract");
    expect(gate).toContain("The target you see is not always the target that acts");
    expect(gate).toContain("A stored action belongs to whoever authored it");
    expect(gate).toContain("Unread is not empty");
    expect(gate).toContain("failed relation scan");
    expect(gate).toContain("an unresolved targetless reload");
    expect(gate).not.toContain("coverage-incomplete relation scan");
    expect(gate).toContain("`python_script.*`, `shell_command.*` and `rest_command.*` are terminals only");
    expect(gate).toContain("Treat them as unread, not as concrete");
  });

  it("does not count a disabled action as a member", () => {
    const gate = flat(indirectActuation);
    // Over-escalation is a real cost: a disabled lock.unlock must not gate
    // the whole run.
    expect(gate).toContain("Skip actions carrying `enabled: false`");
    expect(gate).toContain("a disabled node is known not to run at all");
  });

  it("takes a storage scene's config id from the registry", () => {
    const gate = flat(indirectActuation);
    // ha-nova:scene makes unique_id authoritative; a state attribute can name
    // a different config and the gate would classify the wrong scene.
    expect(gate).toContain("storage scenes and scripts: resolve `config_id` from the entity registry");
    expect(gate).toContain("never from a state attribute");
  });
});
