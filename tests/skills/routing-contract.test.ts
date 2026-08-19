// tests/skills/routing-contract.test.ts
import { describe, it, expect } from "vitest";
import { parse as parseYaml } from "yaml";
import { readFileSync, readdirSync } from "fs";
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
    expect(d).toMatch(/\bturn lights\b/);
    for (const word of ["covers", "climate", "on, off"]) {
      expect(d, `service-call description missing "${word}"`).toContain(word);
    }
  });

  it("puts the one-off vs automation split into the notify description", () => {
    const d = description("notify");
    expect(d).toContain("notification NOW —");
    expect(d).toContain('For "notify me WHEN something happens" use ha-nova:write');
  });

  it("names concrete helper types instead of the word helper alone", () => {
    const d = description("helper");
    for (const t of ["timers", "counters", "toggles", "dropdowns"]) {
      expect(d, `helper description missing "${t}"`).toContain(t);
    }
  });

  it("excludes Alexa and Google from the assist description", () => {
    const d = description("assist");
    expect(d).toContain("Not for Alexa or Google Assistant");
    // Setup and failure are different intents with different owners; a
    // blanket handoff would route an incident away from root-cause analysis.
    expect(d).toContain("one that STOPPED working is a concrete failure for `ha-nova:diagnose`");
  });
});

describe("boundary statements between neighbouring skills (#518)", () => {
  it("does not leave a second executable trace-analysis owner", () => {
    // review is independently discoverable, so narrowing read/history alone
    // would leave the same failure question with two owners.
    const review = flat(read("skills/review/SKILL.md"));
    expect(review).toContain("Trace ANALYSIS belongs to `ha-nova:diagnose`");
    expect(review).toContain("never as the answer to a failure question");
    const traceEvidence = review.match(
      /### Trace Evidence[\s\S]*?### Step 3: Conflict Analysis/,
    )?.[0];
    expect(traceEvidence).toBeDefined();
    expect(traceEvidence ?? "").not.toContain("skills/read/SKILL.md");
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

  it("corrects the nonexistent device-category premise without a fallback loop", () => {
    const organizeRaw = read("skills/organize/SKILL.md");
    const organize = flat(organizeRaw);
    expect(organize).toContain(
      "Device categories do not exist; offer entity categories instead.",
    );
    expect(read("docs/reference/skill-architecture.md")).toContain(
      "device categories do not exist; offer entity categories instead",
    );
    expect(organize).toContain(
      "Config-entry detachment goes to `ha-nova:fallback`",
    );
    const handoffList = organizeRaw.match(
      /Not in scope:[\s\S]*?## Bootstrap/,
    )?.[0];
    expect(handoffList).toBeDefined();
    expect(handoffList ?? "").not.toMatch(/device categor/i);
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
    expect(fb).toContain("split by where their schema comes from");
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
    // Scene/dashboard/cross-item families have no doc page; their evidence is
    // the live data, or the gate silences them by construction.
    // Live data proves the observation; only a source proves the consequence.
    expect(checks).toContain("EXISTENCE and JOIN facts");
    expect(checks).toContain("The BEHAVIOURAL claim attached to them");
    // Each claim class cites the section that actually carries it: the
    // required-field pin lives in D-07, not in the dashboard skill's
    // save-overwrite notes.
    expect(flat(checks)).toContain(
      "that a built-in card field is required — the D-07 allowlist above",
    );
    expect(flat(checks)).toContain(
      "Citing a section that does not carry the claim is the same error as citing nothing",
    );
    expect(checks).toContain(
      "State the observation from the data and the consequence from the source",
    );
    expect(checks).toContain("If only the observation holds, report the observation");
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

  // The suite's own frontmatter helper is a regex, which is more permissive
  // than the YAML parser a client actually uses to discover skills. An
  // unquoted "Assistant: setting" made assist unloadable while every
  // description test still passed.
  it("parses every skill frontmatter with a real YAML parser", () => {
    const broken: string[] = [];
    for (const file of readdirSync("skills")) {
      const path = `skills/${file}/SKILL.md`;
      let raw: string;
      try {
        raw = read(path);
      } catch {
        continue;
      }
      if (!raw.startsWith("---")) continue;
      const frontmatter = raw.split("---")[1] ?? "";
      try {
        const parsed = parseYaml(frontmatter) as unknown;
        if (typeof parsed !== "object" || parsed === null) {
          broken.push(`${path}: frontmatter is not a mapping`);
          continue;
        }
        const { name, description } = parsed as Record<string, unknown>;
        if (typeof name !== "string" || typeof description !== "string") {
          broken.push(`${path}: name/description missing or not a string`);
        }
      } catch (error) {
        broken.push(`${path}: ${(error as Error).message.split("\n")[0]}`);
      }
    }
    expect(
      broken,
      `these skills cannot be loaded by a YAML-parsing client:\n  ${broken.join("\n  ")}`,
    ).toEqual([]);
  });

  it("advertises the long-unavailable boundary on both sides", () => {
    // Subskills are selected from their own frontmatter, so moving a boundary
    // in one skill's prose does nothing until the OTHER skill's description
    // says it owns the request.
    const maintenance = read("skills/maintenance/SKILL.md").split("---")[1] ?? "";
    expect(maintenance).toContain("how long an entity has been unavailable");
    expect(maintenance).toContain("use ha-nova:health");
  });

  it("keeps the log filter bound to the identifier, never OR-ed with a severity", () => {
    const diagnose = flat(read("skills/diagnose/SKILL.md"));
    // `id|ERROR` matches every error line in the log; each one then reads as
    // evidence for an incident it has nothing to do with.
    expect(diagnose).toContain("The identifier is the ONLY selector");
    expect(diagnose).toContain("never OR a severity into it");
    expect(diagnose).toContain("To narrow by severity, AND it");
    expect(diagnose).not.toContain('test("<entity_or_integration>|ERROR"');
    // Finding nothing is an answer, not a reason to loosen the filter.
    expect(diagnose).toContain("rather than widening the filter until something appears");
  });

  it("answers a config-entry flow from the live response, not from research", () => {
    const fallback = flat(read("skills/fallback/SKILL.md"));
    // Fields vary by domain and HA version; the flow response is the only
    // authority for the step being answered.
    expect(fallback).toContain("submit exactly the fields the LIVE response named");
    expect(fallback).toContain("never the fields the research step suggested");
    expect(fallback).toContain(
      "research is how you understand the flow, the response is what you answer",
    );
  });
});

describe("fallback capability-map precedence (#581)", () => {
  it("routes by the operation surface, never the integration's name", () => {
    const raw = read("skills/fallback/SKILL.md");
    const fb = flat(raw);
    expect(fb).toContain("the surface the change would actually call");
    // A row naming the operation itself (Alarmo code management stays External)
    // beats the surface arms, and any residual overlap fails closed.
    expect(fb).toContain("first a row naming the operation itself");
    expect(fb).toContain(
      "take the least permissive matching row (External over Relay-Ready)",
    );
    // A custom integration with a standard OptionsFlow takes the config-entry
    // row; the custom-API row must not name it as an example anymore.
    expect(fb).toContain(
      "even on a custom integration, never the custom-API row",
    );
    const customApiRow = raw
      .split("\n")
      .find((l) => l.includes("Custom-integration configuration APIs"));
    expect(customApiRow).toBeDefined();
    expect(customApiRow ?? "").toContain("OWN endpoints");
    expect(customApiRow ?? "").not.toContain("Adaptive Lighting");
  });

  it("draws the same boundary where the custom-API mechanics live", () => {
    const rr = flat(read("skills/fallback/relay-ready.md"));
    expect(rr).toContain(
      "configured through a standard config-entry OptionsFlow (for example Adaptive Lighting) does not belong here",
    );
    expect(rr).toContain(
      "follow the config-entry rows of the Capability Map (precedence rule)",
    );
  });
});
