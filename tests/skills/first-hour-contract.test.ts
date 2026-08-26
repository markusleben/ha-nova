import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const ROOT = join(__dirname, "..", "..");
const read = (p: string) => readFileSync(join(ROOT, p), "utf8");
const flat = (s: string) => s.replace(/\s+/g, " ");

const context = flat(read("skills/ha-nova/SKILL.md"));
const capability = flat(read("skills/ha-nova/capability-answer.md"));
const proposals = flat(read("skills/ha-nova/starter-proposals.md"));
const outputRules = flat(read("skills/ha-nova/output-rules.md"));
const patterns = flat(read("skills/ha-nova/automation-patterns.md"));

describe("first-hour pack (#528)", () => {
  it("routes the beginner questions to owned surfaces (P3-01/02/03/05/08)", () => {
    expect(context).toContain('"what can you do (here)?"');
    expect(context).toContain("skills/ha-nova/capability-answer.md");
    expect(context).toContain('"show me my home"');
    expect(context).toContain('"can I break something?"');
    expect(context).toContain('"analyze my home and suggest automations"');
    expect(context).toContain("skills/ha-nova/starter-proposals.md");
    expect(context).toContain('"what did you do today / this session?"');
  });

  it("grounds the capability answer in this home without dumping states (P3-01)", () => {
    expect(capability).toContain("ONE bounded aggregate read");
    expect(capability).toContain("never print the raw dump");
    expect(capability).toContain("everyday jobs");
  });

  it("keeps the home overview aggregate-only and location-safe (P3-05)", () => {
    expect(capability).toContain("Aggregate counts are List-Frame-legal; entity dumps are not.");
    expect(capability).toContain("Never include coordinates, tracker positions, or per-person locations");
  });

  it("renders the safety story from enforced guarantees only (P3-03)", () => {
    expect(capability).toContain("Nothing is written without a preview you confirm first");
    expect(capability).toContain("typed confirmation code");
    expect(capability).toContain("the honest limit is part of the story");
  });

  it("gates every starter proposal on inventory evidence (P3-02)", () => {
    expect(proposals).toContain("Evidence-gated");
    expect(proposals).toContain("a pattern with no matching hardware is never proposed");
    expect(proposals).toContain("Read-only until the user accepts an item.");
    expect(proposals).toContain("The table is the catalog ceiling");
    expect(proposals).toContain("hands to the owning skill INDIVIDUALLY");
    expect(proposals).toContain("At most 5 items");
    expect(proposals).toContain("not from name-string guessing");
    expect(proposals).toContain("Duplicate gate, config-evidence only");
    expect(proposals).toContain("Scan each read WHOLE config document");
    expect(proposals).toContain("cap the config reads (default 50, newest-updated first)");
    expect(proposals).toContain("AND every selector value that resolves to them");
    expect(proposals).toContain("ON ONE EXECUTABLE PATH");
    expect(proposals).toContain('never claim "not automated yet" from states rows or aliases');
    expect(proposals).toContain("Area pairing resolves area-first per the architecture reference");
    expect(proposals).toContain("the notify domain's services from `/api/services`");
    expect(proposals).toContain("`ha-nova:history`'s bounded read");
  });

  it("keeps the post-write ban with exactly one named exception (P3-06)", () => {
    expect(outputRules).toContain("never post-write — with ONE named exception, the Replication Line below");
    expect(outputRules).toContain("## Replication Line (the one post-write exception)");
    expect(outputRules).toContain("Applies to `ha-nova:write` AUTOMATION creates ONLY");
    expect(outputRules).toContain("After a VERIFIED create (never after an update, delete, or a failed verification)");
    expect(outputRules).toContain("only when the registries actually prove it");
    expect(flat(read("skills/write/SKILL.md"))).toContain(
      "Sole exception: the one-line Replication Line after a verified create",
    );
  });

  it("replication re-resolves per room and never copies entity ids (P3-06)", () => {
    expect(patterns).toContain("### Replicate Across Rooms");
    expect(patterns).toContain("Replication is re-resolution, never copy-paste");
    expect(patterns).toContain("never substitute a neighboring room's entity");
    expect(patterns).toContain("One normal preview/confirm per room.");
    expect(context).toContain("resolve the kitchen's OWN entities, never copy entity ids");
  });

  it("explain mode leads with the behavior narrative (P3-04)", () => {
    const readSkill = flat(read("skills/read/SKILL.md"));
    expect(readSkill).toContain("Explain mode (");
    expect(readSkill).toContain("offer the YAML instead of dumping it");
    expect(context).toContain("behavior narrative first, YAML only on request");
  });

  it("the context skill owns the read-only structure check, fixes hand to organize (P3-07)", () => {
    expect(capability).toContain('## Structure Check — "Is my setup tidy?"');
    expect(capability).toContain("every FIX hands to `ha-nova:organize`'s normal preview/confirm flow");
    expect(capability).toContain("never the full listing");
    expect(capability).toContain('reports as "possibly default", never as a rename offer');
    expect(capability).toContain("count DEVICE-BOUND entities without an EFFECTIVE area");
    expect(capability).toContain("that IS the provenance signal");
    expect(context).toContain("fixes hand to `ha-nova:organize`");
  });

  it("session recap answers only from verified writes (P3-08)", () => {
    expect(outputRules).toContain("Answer ONLY from this conversation's writes");
    expect(outputRules).toContain("never upgraded to successes in recap");
  });

  it("fuels the suggestion budget for scene, dashboard, and failure alerts (P3-09)", () => {
    expect(outputRules).toContain("a same-area entity the scene plausibly misses");
    expect(outputRules).toContain("a strategy config for the empty shell");
    expect(flat(read("skills/ha-nova/agents/resolve-agent.md"))).toContain("outcome alert");
  });
});
