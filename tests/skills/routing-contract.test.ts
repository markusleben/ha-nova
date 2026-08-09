// tests/skills/routing-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const context = read("skills/ha-nova/SKILL.md");
const flat = (text: string): string => text.replace(/\s+/g, " ");
const description = (skill: string): string => {
  const line = read(`skills/${skill}/SKILL.md`)
    .split("\n")
    .find((l) => l.startsWith("description:"));
  return line ?? "";
};

describe("description-level routing (#518)", () => {
  // In most clients the frontmatter description IS the routing signal — the
  // dispatch table only loads together with the context skill. A description
  // written in implementation vocabulary loses the intents users actually
  // speak.
  it("gives service-call the words users say for device control", () => {
    const d = description("service-call");
    for (const word of ["turn", "lights", "covers", "climate", "on, off"]) {
      expect(d, `service-call description missing "${word}"`).toContain(word);
    }
  });

  it("puts the one-off vs automation split into the notify description", () => {
    const d = description("notify");
    expect(d).toContain("NOW");
    expect(d).toContain('For "notify me WHEN something happens" use ha-nova:write');
  });

  it("names concrete helper types instead of the word helper alone", () => {
    const d = description("helper");
    for (const t of ["timers", "counters", "toggles", "dropdowns"]) {
      expect(d, `helper description missing "${t}"`).toContain(t);
    }
  });

  it("excludes Alexa and Google from the assist description", () => {
    expect(description("assist")).toContain(
      "Not for Alexa or Google Assistant",
    );
  });
});

describe("boundary statements between neighbouring skills (#518)", () => {
  it("does not leave a second executable trace-analysis owner", () => {
    // review is independently discoverable, so narrowing read/history alone
    // would leave the same failure question with two owners.
    const review = flat(read("skills/review/SKILL.md"));
    expect(review).toContain("Trace ANALYSIS belongs to `ha-nova:diagnose`");
    expect(review).toContain("never as the answer to a failure question");
    // And the entrypoint must not keep its own contradicting verify gate.
    expect(review).toContain("the canonical sequence lives in");
    expect(review).not.toContain("Only flag as error if confirmed invalid after both checks");
  });

  it("gives trace analysis exactly one owner", () => {
    // history pointed at read, dispatch pointed at diagnose, and read never
    // claimed traces — three pointers, no owner.
    expect(flat(read("skills/history/SKILL.md"))).toContain(
      "Use `ha-nova:diagnose` for traces",
    );
    expect(flat(read("skills/read/SKILL.md"))).toContain(
      "Trace ANALYSIS belongs to `ha-nova:diagnose`",
    );
    expect(flat(read("skills/read/SKILL.md"))).toContain(
      "with no failure question attached",
    );
  });

  it("gives health the reverse boundaries it was missing", () => {
    const h = flat(read("skills/health/SKILL.md"));
    expect(h).toContain("root-causing one named item's concrete incident (`ha-nova:diagnose`)");
    expect(h).toContain("not why one thing failed");
    expect(h).toContain("how long an entity has been unavailable");
    expect(flat(read("skills/maintenance/SKILL.md"))).toContain(
      "the long-unavailable report here exists to qualify cleanup candidates",
    );
  });

  it("names admin as the target organize sends zones, persons, and tags to", () => {
    expect(read("skills/organize/SKILL.md")).toContain(
      "- zones, persons, tags (`ha-nova:admin`)",
    );
  });
});

describe("dispatch examples for the rows that had none (#518)", () => {
  it("encodes the hacs/updates boundary as examples, not only inside the skills", () => {
    expect(context).toContain('**"Install browser_mod from HACS"** → `ha-nova:hacs`');
    expect(context).toContain('**"Update my HACS integration to the latest"** → `ha-nova:updates`');
    expect(context).toContain("an explicit version or a downgrade goes to `ha-nova:hacs`");
  });

  it("gives onboarding an example and hedges the sensor question by transport", () => {
    expect(context).toContain("`ha-nova:onboarding` (diagnostics only, never a config write)");
    // "Is my sensor sending?" → mqtt is right only for MQTT transports, and
    // the example must still resolve to exactly ONE skill per transport.
    expect(flat(context)).toContain(
      "`ha-nova:mqtt` for an MQTT/Zigbee2MQTT device",
    );
    expect(flat(context)).toContain(
      "for any other transport `ha-nova:diagnose` — a sensor that stopped updating is a concrete incident",
    );
  });
});

describe("load-bearing rules referenced from where they apply (#518)", () => {
  it("points the fallback execute step at its own endpoint-type table", () => {
    const fb = flat(read("skills/fallback/SKILL.md"));
    expect(fb).toContain(
      "If the endpoint matches a row in Write Safety by Endpoint Type (below), classify it BEFORE drafting",
    );
    // The table has no row for response-driven flows or blueprint/save, so a
    // blanket classification requirement would stall documented operations.
    expect(fb).toContain("Endpoints the table does not cover");
    expect(fb).toContain("follow the schema the research step returned");
  });

  it("echoes verify-before-flag where findings are generated", () => {
    // checks.md is loaded standalone by write, helper, and yaml-config, which
    // never see the review skill's copy of the rule.
    const checks = flat(read("skills/review/checks.md"));
    expect(checks).toContain("## Verify Before Flagging (Critical)");
    expect(checks).toContain("costs the user more trust than a missed one");
    expect(checks).toContain(
      "the standalone review flow and the post-write phases in write, helper, and yaml-config alike",
    );
    // A standalone loader cannot follow "check the local reference doc" unless
    // the doc is named, and the sequence must not demand two confirmations
    // for something the first source already settled.
    expect(checks).toContain("skills/ha-nova/template-guidelines.md");
    // Every local reference needs its full path: a direct loader cannot open
    // "automation-patterns.md" relative to nothing.
    expect(checks).toContain("skills/ha-nova/automation-patterns.md");
    expect(checks).toContain("skills/ha-nova/helper-flow-schemas.md");
    expect(checks).toContain("home-assistant.io/docs/automation/trigger/");
    // Most of the catalog flags valid-but-risky config, so demanding schema
    // invalidity would suppress even HIGH runtime hazards.
    expect(checks).toContain("Flag it when the settling source confirms the CLAIM you are making");
    expect(checks).toContain(
      "configurations Home Assistant accepts happily and that still misbehave",
    );
    expect(checks).toContain('"valid YAML" never clears them');
    expect(checks).toContain("Unresolved means unresolved");
  });

  it("names the payload shapes that were documented nowhere", () => {
    expect(read("skills/admin/SKILL.md")).toContain('"type":"config/auth/create"');
    expect(flat(read("skills/admin/SKILL.md"))).toContain(
      "`system-admin` in `group_ids` makes the account an administrator",
    );
    expect(read("skills/dashboard/SKILL.md")).toContain('"type":"lovelace/config/save"');
    expect(flat(read("skills/dashboard/SKILL.md"))).toContain(
      "the whole document goes under `config`",
    );
  });

  it("restricts what may be interpolated into the diagnose log filter", () => {
    expect(flat(read("skills/diagnose/SKILL.md"))).toContain(
      "Substitute only an `entity_id`, domain, or integration slug",
    );
    expect(flat(read("skills/diagnose/SKILL.md"))).toContain("never a friendly name");
    // entity_id is not literal either: the domain separator is a wildcard.
    expect(flat(read("skills/diagnose/SKILL.md"))).toContain(
      "the `.` in `sensor.kitchen` is a regex wildcard",
    );
  });
});
