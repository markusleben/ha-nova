// Expansion half of the indirect-actuation contract.
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (relative: string): string =>
  readFileSync(resolve(__dirname, "../../", relative), "utf-8");

const skillDoc = read("skills/service-call/SKILL.md");
const contextSkill = read("skills/ha-nova/SKILL.md");
const testRunDoc = read("skills/ha-nova/test-run.md");
const assistSkill = read("skills/assist/SKILL.md");
const indirectActuation = read("skills/ha-nova/indirect-actuation.md");
const writeSkill = read("skills/write/SKILL.md");
const tierScan = read("cli/tier_scan.go");
const tierWalk = read("cli/tier_walk.go");
const tierListeners = read("cli/tier_listeners.go");
const tierRules = read("cli/tier_rules.go");
// Contract docs hard-wrap at ~72 columns, so a pinned sentence must not also
// pin the column it happens to break at.
const flat = (text: string): string => text.replace(/\s+/g, " ");
describe("indirect actuation and tier classification (#513)", () => {
  describe("indirect actuation cannot bypass the high-consequence tier (#513)", () => {
    it("routes every indirect-run service through the shared gate", () => {
      // Since PR4 the gate runs in the CLI tier engine for every entity call;
      // the skill routes through it and the prose stays the engine's spec.
      expect(skillDoc).toContain("Entity service calls run only through the typed verbs");
      expect(skillDoc).toContain("the `members[]` a scene or script run actuates, the `consumers[]` that react");
      expect(tierScan).toContain("// scanConsumers runs once per acted entity");
      expect(tierWalk).toContain('if name == "scene.apply" {');
      expect(tierListeners).toContain('if eventType == "call_service" {');
      // The gate itself has to say it too — pinning only service-call left the
      // shared contract free to drop the rule, which a mutation proved.
      expect(flat(indirectActuation)).toContain(
        "does not expand STORED MEMBERS",
      );
      expect(flat(indirectActuation)).toContain("It still carries the CONSUMER scan");
      // homeassistant.turn_on with a script target is a script run under
      // another name — a service-name list alone would wave it through.
      const gate = flat(indirectActuation);
      expect(gate).toContain("Classify by the TARGET's domain first");
      expect(gate).toContain("`entity_id: script.open_door` is a script run wearing another name");
      expect(gate).toContain("whatever service was used to get there");
      // Stopping performs none of the member actions — gating it would
      // demand a typed code to make behavior END.
      expect(gate).toContain("Only an actual run expands stored members");
      expect(gate).toContain("enabling or disabling an AUTOMATION");
      expect(gate).toContain("`automation.trigger` is the one that runs it");
      expect(gate).toContain("still take the CONSUMER scan below");
      expect(gate).toContain("findings set the tier");
      expect(tierRules).toContain("// isRun reports whether the call STARTS stored members of the target.");
      // The observed running state must NOT lower the tier: a mode: single
      // script that is busy at preview time can finish while the confirmation
      // waits, and then the call that "runs nothing" runs everything.
      expect(gate).toContain("A script's RUNNING state never lowers the tier");
      expect(gate).toContain("that observation expires");
      // Helper domains exist to drive automations; a counter crossing a
      // threshold is a trigger like any other.
      for (const helper of ["`counter`", "`timer`", "`schedule`", "`input_number`"]) {
        expect(gate, `trigger-source list missing ${helper}`).toContain(helper);
      }
    });

    it("binds the tier to the performed action, not the entity or the service", () => {
      // Entity-scoped wording would make LOCKING a door high-consequence.
      expect(skillDoc).toContain(
        "take the code — also when a scene, script, or automation run reaches one. Locking, closing, and arming stay ordinary.",
      );
      expect(contextSkill).toContain("The tier follows the action that ends up performed");
      expect(contextSkill).toContain("Locking, closing, and arming stay ordinary");
    });

    it("keeps disruptive services distinct from the access-granting tier", () => {
      // restart/stop reload config (or stop HA), so they are typed like a
      // reload (2026-10-17); siren stays disruptive/preview.
      expect(tierRules).toContain('"siren.turn_on": true, "siren.toggle": true');
      expect(tierRules).toContain('service == "homeassistant.restart" || service == "homeassistant.stop"');
      expect(skillDoc).toContain("`homeassistant.restart`/`stop` and App/host control (`hassio.*`) take the one-time code");
      expect(flat(testRunDoc)).toContain(
        "a deliberate superset of the context skill's high-consequence confirmation tier",
      );
      expect(flat(testRunDoc)).toContain(
        "Members that grant physical access still take the one-time code",
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
      expect(gate).toContain("Resolve `area_id` and `device_id` targets");
      expect(gate).toContain("stored `floor_id` and `label_id` selectors as unresolved");
      expect(gate).toContain("never invent partial membership");
      expect(gate).toContain("This never rewrites a payload");
      expect(gate).toContain("no stored config exists");
      expect(gate).toContain("`search/related` on the target");
      expect(gate).toContain("helper toggle that another automation answers by unlocking a door");
      // Every service call emits an event before its handler; relation scans
      // cannot see an automation that listens to that event.
      expect(gate).toContain("Every requested or expanded service call also emits a `call_service` event");
      expect(gate).toContain("`search/related` does not index those listeners");
      expect(gate).toContain("apply their literal `event_data` filters");
      expect(gate).toContain("an unenumerable listener escalates");
      expect(gate).toContain("Repeat for nested calls");
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
      expect(gate).toContain("never from a state attribute");
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
        "the card confirmation never replaces the one-time code",
      );
      expect(flat(skillDoc)).toContain(
        "if the CLI put the run on the typed tier, the card choice never replaces the code",
      );
    });

    it("gates utterance execution in the skill it defers to", () => {
      expect(flat(assistSkill)).toContain(
        "An utterance is the least enumerable indirect actuation there is",
      );
      expect(assistSkill).toContain("it takes the one-time code");
      expect(assistSkill).toContain("including the re-run proof after an exposure fix");
    });
  });
});
