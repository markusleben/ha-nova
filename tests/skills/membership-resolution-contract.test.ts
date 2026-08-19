// tests/skills/membership-resolution-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

const contract = flat(read("skills/ha-nova/membership-resolution.md"));

describe("shared membership-resolution contract (#571)", () => {
  it("declares one authoritative source per composite type", () => {
    expect(contract).toContain(
      "Every composite type has exactly one authoritative source",
    );
    // One pin per composite family from the issue.
    expect(contract).toContain(
      "the `entity_id` state attribute is the fallback read only when the options are unreadable",
    );
    expect(contract).toContain("when both are readable and disagree, membership is UNRESOLVED");
    // Freeze mode never ships a parent beside its own expanded children.
    expect(contract).toContain(
      "A frozen executable payload contains only the deduplicated concrete leaf entities",
    );
    expect(contract).toContain(
      "`attributes.entity_id` where exposed; otherwise uninspectable",
    );
    expect(contract).toContain(
      "a legacy notify group SERVICE exposes no readable membership",
    );
    expect(contract).toContain(
      "`/api/services` proves it exists, never who it reaches: treat it as uninspectable",
    );
    expect(contract).toContain("the `group_members` state attribute");
    expect(contract).toContain(
      "entity/device registry plus `search/related` on the resolved area",
    );
    expect(contract).toContain(
      "`person.*` states mapped to real notify targets",
    );
  });

  it("bounds nested expansion with cycle detection, dedupe, and ordering", () => {
    expect(contract).toContain("Expand recursively, depth-bounded at 5");
    expect(contract).toContain(
      "a branch still composite at the bound is UNRESOLVED, never silently truncated",
    );
    expect(contract).toContain(
      "A member already visited on the current path is a cycle",
    );
    expect(contract).toContain(
      "Remove duplicates keeping the first occurrence, and preserve deterministic ordering",
    );
  });

  it("names the four resolution outcomes and previews every member", () => {
    for (const outcome of [
      "**fully resolved**",
      "**partially resolved**",
      "**uninspectable**",
      "**changed after preview**",
    ]) {
      expect(contract, `missing outcome ${outcome}`).toContain(outcome);
    }
    expect(contract).toContain(
      "The preview shows every resolved member the action affects",
    );
    expect(contract).toContain(
      "Partially resolved and unresolvable members are shown explicitly",
    );
  });

  it("never reports an unresolvable group as empty", () => {
    expect(contract).toContain(
      "An unresolvable group is NEVER reported as empty",
    );
    expect(contract).toContain(
      "rather than acting on guessed recipients or members",
    );
  });

  it("freezes where equivalent, re-reads before execution otherwise", () => {
    expect(contract).toContain(
      "where calling the members directly is semantically equivalent",
    );
    expect(contract).toContain(
      "execute with the resolved `entity_id` list in the payload",
    );
    expect(contract).toContain(
      "where direct member calls would change the semantics",
    );
    expect(contract).toContain(
      "re-read its membership immediately before execution",
    );
    expect(contract).toContain(
      "ANY change that alters recipients, side effects, or risk invalidates the existing confirmation and produces a new preview",
    );
  });

  it("classifies safety by the highest-risk member and verifies the expansion", () => {
    expect(contract).toContain(
      "Safety classification uses the highest-risk resolved member",
    );
    expect(contract).toContain(
      "an unreadable member escalates rather than defaulting down",
    );
    expect(contract).toContain(
      "verification covers the expanded members, not the collection entity",
    );
  });

  it("names the existing implementations as instances, not re-specifications", () => {
    expect(contract).toContain("`skills/service-call/SKILL.md` Flow step 3");
    expect(contract).toContain(
      "legacy `group.*` targets forward to their members",
    );
    expect(contract).toContain(
      "its unreadable-presence handling stays authoritative",
    );
    // Scene/script/automation expansion stays with the indirect-actuation gate.
    expect(contract).toContain(
      "`skills/ha-nova/indirect-actuation.md` owns indirect actuation",
    );
  });
});

describe("consumer wiring (#571)", () => {
  it("service-call names the shared contract at its step-3 expansion", () => {
    expect(flat(read("skills/service-call/SKILL.md"))).toContain(
      "instantiates the shared membership contract `skills/ha-nova/membership-resolution.md`",
    );
  });

  it("notify routes recipient groups here and keeps presence handling", () => {
    const notify = flat(read("skills/notify/SKILL.md"));
    expect(notify).toContain(
      "follow the shared contract `skills/ha-nova/membership-resolution.md`",
    );
    expect(notify).toContain(
      "the unreadable-presence handling below stays authoritative",
    );
  });

  it("media points speaker grouping at the shared contract", () => {
    const media = flat(read("skills/media/SKILL.md"));
    expect(media).toContain(
      "resolves per the shared contract `skills/ha-nova/membership-resolution.md`",
    );
    expect(media).toContain(
      "`group_members` is authoritative, re-read immediately before execution",
    );
  });
});
