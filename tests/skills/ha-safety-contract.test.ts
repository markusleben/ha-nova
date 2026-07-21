import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha safety contract", () => {
  it("enforces tiered confirmations and no-guessing policy", () => {
    const router = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    expect(router).toContain("Never guess entity IDs, service names, or config IDs.");
    expect(router).toContain("create`/`update`: natural confirmation");
    expect(router).toContain("typed confirmation code `confirm:<token>`");
    expect(writeSkill).toContain("Confirmation: create/update=natural, delete=typed confirmation code `confirm:<token>`");
    // Safety Core wording (A3): the no-guessing rule now arrives via the
    // byte-identical core block in every mutation-capable skill.
    expect(writeSkill).toContain("Never guess entity, service, or config IDs — resolve them or ask.");
  });

  it("rejects pre-preview blanket approval for live writes", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const applyAgent = readFileSync("skills/ha-nova/agents/apply-agent.md", "utf8");
    const architecture = readFileSync("docs/reference/skill-architecture.md", "utf8");
    const relayApi = readFileSync("skills/ha-nova/relay-api.md", "utf8");

    expect(context).toContain("### Active Preview Confirmation");
    expect(context).toContain("A user instruction given before the preview exists is never valid write confirmation.");
    for (const phrase of ["implement the plan", "do it", "go ahead", "make the changes", "apply the plan"]) {
      expect(context).toContain(`"${phrase}"`);
    }
    expect(context).toContain("permission to prepare the draft, run checks, and show the preview");
    expect(context).toContain("Confirmation is bound to the displayed operation, target set, endpoint/service, and exact payload/diff/manifest");
    expect(context).toContain("confirmation expires");
    expect(context).toContain("Multi-target confirmation is valid only where the owning skill supports multi-target writes");

    expect(applyAgent).toContain("Apply precondition");
    expect(applyAgent).toContain("pre-preview-only");
    expect(applyAgent).toContain("BLOCKED: confirmation missing or stale");

    expect(architecture).toContain("pre-preview approval is never write confirmation");
    expect(relayApi).toContain("Relay API examples are not write authorization");
  });

  it("keeps mutation-capable skills tied to active-preview confirmation", () => {
    const mutationDocs = [
      "skills/write/SKILL.md",
      "skills/helper/SKILL.md",
      "skills/dashboard/SKILL.md",
      "skills/scene/SKILL.md",
      "skills/organize/SKILL.md",
      "skills/todo/SKILL.md",
      "skills/backup/SKILL.md",
      "skills/updates/SKILL.md",
      "skills/energy/SKILL.md",
      "skills/maintenance/SKILL.md",
      "skills/service-call/SKILL.md",
      "skills/review/SKILL.md",
      "skills/fallback/SKILL.md",
    ];

    for (const file of mutationDocs) {
      const content = readFileSync(file, "utf8");
      expect(content, file).toContain("Active Preview Confirmation");
    }
  });

  it("preserves explicit user constraints across multi-step changes", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
    const grouped = readFileSync("skills/ha-nova/grouped-change-set.md", "utf8");

    // Four distinct kinds, never merged.
    expect(context).toContain("## Decision Memory (multi-step tasks)");
    expect(context).toContain("**hard requirements**");
    expect(context).toContain("**accepted choices**");
    expect(context).toContain("**rejected alternatives**");
    expect(context).toContain("**unresolved assumptions**");
    expect(context).toContain("four kinds, never merged");

    // Supersession semantics: replaced is not a conflict.
    expect(context).toContain("**Last explicit choice wins:**");
    expect(context).toContain(
      "replaces exactly the older choice it contradicts",
    );
    expect(context).toContain("unrelated earlier constraints stay active");
    expect(context).toContain(
      "A replaced decision is not a conflict — two still-active contradicting requirements are.",
    );

    // The gate: block and explain, never silently resolve.
    expect(context).toContain(
      "Validate every plan, preview, and mutation draft against the active user decisions",
    );
    expect(context).toContain("block that output and explain in plain language");
    expect(context).toContain("never silently pick a side");
    expect(context).toContain("never quietly drop the older constraint");

    // Unresolved assumptions surface with the Uncertain tone.
    expect(context).toContain(
      "surface in the preview with the Uncertain tone",
    );

    // No persistence, no internal identifiers in output.
    expect(context).toContain("no persistent store");
    expect(context).toContain(
      "no internal requirement labels or identifiers in user-facing output",
    );

    // Multi-turn worked example: superseded, retained, conflict, unresolved.
    expect(context).toContain("Worked example:");
    expect(context).toContain("replaces the old requirement");
    expect(context).toContain("the hallway decision stays untouched");

    // Carried through multi-target and grouped flows.
    expect(writeSafety).toContain("context skill → Decision Memory");
    expect(grouped).toContain("context skill → Decision Memory");
  });

  it("requires structured failure output", () => {
    const router = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

    expect(router).toContain("what failed");
    expect(router).toContain("why it failed");
    expect(router).toContain("next concrete step");
    expect(writeSkill).toContain("No raw curl/JSON in output.");
    expect(writeSkill).toContain("## References");
  });

  it("enforces fallback skill as mandatory for raw relay writes", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const fallback = readFileSync("skills/fallback/SKILL.md", "utf8");

    // Context skill: dispatch table marks fallback as mandatory
    expect(context).toContain("mandatory fallback");
    expect(context).toContain("never skip");

    // Context skill: safety baseline blocks raw relay writes without a skill
    expect(context).toContain("No raw relay writes without a skill");
    expect(context).toContain("ha-nova:fallback");

    // Context skill: concrete scary example (lovelace overwrite)
    expect(context).toContain("lovelace/config/save");
    expect(context).toContain("full-document overwrites");
    expect(context).toContain("Skipping it risks data loss");

    // Fallback skill: description says mandatory
    expect(fallback).toContain("Mandatory fallback");

    // Fallback skill: anti-patterns
    expect(fallback).toContain("Anti-Patterns");
    expect(fallback).toContain("lovelace/config/save");
    expect(fallback).toContain("trial-and-error");
    expect(fallback).toContain("Probing write endpoints");

    // Fallback skill: write safety by endpoint type
    expect(fallback).toContain("Full-document overwrite");
    expect(fallback).toContain("Field-level list replace");
    expect(fallback).toContain("Write Safety by Endpoint Type");
  });

  it("requires concise correction of invalid Home Assistant premises", () => {
    const router = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
    const fallbackSkill = readFileSync("skills/fallback/SKILL.md", "utf8");

    expect(router).toContain("invalid Home Assistant premises");
    expect(router).toContain("briefly and technically");
    expect(writeSkill).toContain("Correct invalid premises");
    expect(fallbackSkill).toContain("invalid Home Assistant premises");
    expect(fallbackSkill).toContain("wrong premise");
  });

  it("keeps non-mutation skills on an explicit handoff boundary", () => {
    const readSkill = readFileSync("skills/read/SKILL.md", "utf8");
    const reviewSkill = readFileSync("skills/review/SKILL.md", "utf8");
    const discoverySkill = readFileSync("skills/entity-discovery/SKILL.md", "utf8");
    const onboardingSkill = readFileSync("skills/onboarding/SKILL.md", "utf8");

    expect(readSkill).toContain("MUST NOT issue `POST`, `PUT`, `PATCH`, or `DELETE` relay requests.");
    expect(readSkill).toContain("MUST NOT call service endpoints or any other mutation path learned during the read flow.");
    expect(readSkill).toContain("hand off to `ha-nova:write`");

    expect(reviewSkill).toContain("No `POST`, `PUT`, `PATCH`, or `DELETE` config writes through the relay.");
    expect(reviewSkill).toContain("hand off to `ha-nova:write`");
    expect(reviewSkill).toContain("hand off to `ha-nova:helper`");
    expect(reviewSkill).toContain("The Quick-Fix service call in Step 4 is the only write exception in this skill.");
    expect(reviewSkill).toContain("Bulk review is stricter: no Quick-Fix, no service calls, no write exception.");

    expect(discoverySkill).toContain("No `POST`, `PUT`, `PATCH`, or `DELETE` relay writes.");
    expect(discoverySkill).toContain("hand off to the write-capable skill");

    expect(onboardingSkill).toContain("Diagnostics only.");
    expect(onboardingSkill).toContain("Do not use this skill for config writes");
  });
});
