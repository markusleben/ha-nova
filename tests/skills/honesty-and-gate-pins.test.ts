// tests/skills/honesty-and-gate-pins.test.ts
//
// #515: rules that were load-bearing but untested. Two classes:
//
//   1. Honesty guarantees that docs/reference/safety.md cites a test for,
//      where the cited test does not actually assert them. A green suite plus
//      a promise in the safety table is worse than an honest gap.
//   2. Gates whose deletion would silently downgrade a tier or lose data,
//      ranked by what regressing them costs the user.
import { describe, it, expect } from "vitest";
import { existsSync, readdirSync, readFileSync, statSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
// Contract docs hard-wrap; pin sentences, not wrap columns.
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("honesty guarantees safety.md names but nothing asserted (#515)", () => {
  it("keeps camera frames in client-private scratch and out of the workspace", () => {
    const camera = flat(read("skills/camera/SKILL.md"));
    expect(camera).toContain("A camera frame is private data");
    expect(camera).toContain("client-private scratch storage");
    expect(camera).toContain("never send it anywhere outside this conversation");
  });

  it("never reports an accepted notification as delivered", () => {
    // The descriptive sentence alone is not the guarantee: pin the
    // prohibition, so rewording the report side cannot keep this green.
    const notify = flat(read("skills/notify/SKILL.md"));
    expect(notify).toContain("There is no delivery receipt");
    // The prohibition, not just the observation: a successful call is
    // acceptance, and the report must not upgrade it to delivery.
    expect(notify).toContain(
      "a successful service call means Home Assistant accepted it, not that the phone displayed it",
    );
  });

  it("distinguishes an empty MQTT window from a retained replay", () => {
    const mqtt = flat(read("skills/mqtt/SKILL.md"));
    expect(mqtt).toContain("Say explicitly when nothing arrived");
    // The guarantee is the SEPARATION, not the phrase: a contract that
    // counted replays as live traffic could still contain the words.
    expect(mqtt).toContain(
      "how many were live (`retain: false`) versus retained replays",
    );
    expect(mqtt).toContain("never call the device silent on one missed window");
  });

  it("binds conclusions to evidence with named confidence tiers", () => {
    const context = flat(read("skills/ha-nova/SKILL.md"));
    expect(context).toContain("## Claim-Evidence Binding (Critical)");
    expect(context).toContain("Data-target match");
    expect(context).toContain(
      "Never present \"likely\" or \"uncertain\" in the same tone as \"verified.\"",
    );
    expect(context).toContain(
      "Wrong confident answer is worse than honest",
    );
  });
});

describe("gates whose loss would silently downgrade a tier (#515)", () => {
  // Ranked by silent-regression cost: a broker-persistent write, then the
  // account guards, then host-file overwrite, then a full-document revert.
  it("keeps retained MQTT publishes on the typed tier", () => {
    const mqtt = flat(read("skills/mqtt/SKILL.md"));
    expect(mqtt).toContain("persists on the broker");
    expect(mqtt).toContain("can create or destroy entities");
    expect(mqtt).toContain(
      "Retained publishes therefore require the typed `confirm:<token>`",
    );
  });

  it("refuses the owner, system accounts, and the relay's own account", () => {
    const admin = flat(read("skills/admin/SKILL.md"));
    expect(admin).toContain("Never delete the owner");
    expect(admin).toContain("Never delete a `system_generated` account");
    expect(admin).toContain("Never delete the account whose token this relay uses");
    // Without current_user the relay cannot know which account is its own.
    expect(admin).toContain("if that call fails, do not delete any account");
  });

  it("previews the exact host path before a camera write that can overwrite", () => {
    const camera = flat(read("skills/camera/SKILL.md"));
    expect(camera).toContain("write files on the Home Assistant server");
    expect(camera).toContain("preview the exact path and confirm");
    expect(camera).toContain("they can overwrite an existing file");
  });

  it("stops a dashboard save when the live document changed during the pause", () => {
    // scene and grouped-change-set carry pinned equivalents; dashboard is the
    // family where the failure mode is a silent full-document revert.
    // Pin the CONDITION, not the message: the guarantee is that a foreign
    // change during the confirmation pause blocks the save and forces a fresh
    // preview. The words alone could survive the reread being removed.
    const dashboard = flat(read("skills/dashboard/SKILL.md"));
    expect(dashboard).toContain("STOP — confirmation expired");
    expect(dashboard).toContain("conversation paused");
    expect(dashboard).toContain("compare the FULL document against the merge basis");
    expect(dashboard).toContain("on any foreign change");
  });

  it("rolls back an invalid YAML write before reporting, and never reloads it", () => {
    const yaml = flat(read("skills/yaml-config/SKILL.md"));
    expect(yaml).toContain('`{"result":"invalid"}`');
    expect(yaml).toContain("roll back immediately");
    expect(yaml).toContain("do NOT reload");
    // One .bak deep: a second write would overwrite the rollback.
    expect(yaml).toContain("the `.bak` holds only ONE step");
  });

  it("restores the recorded log level by name, never a hard-coded default", () => {
    const diagnose = flat(read("skills/diagnose/SKILL.md"));
    expect(diagnose).toContain("restore the recorded level as its NAME");
    expect(diagnose).toContain("never a hard-coded `warning`");
    // An override survives until restart, so "reset" means setting it back.
    expect(diagnose).toContain("an override persists until the next restart");
  });
});

describe("every dispatch target exists as a skill (#515)", () => {
  // The class behind the 0.23.0 scare: a dispatch row or capability-map row
  // naming a skill that is not in the tree. Packaging copies skills/
  // wholesale, so tree completeness IS bundle completeness — this belongs in
  // the PR suite, not a release script.
  const skillsRoot = resolve(__dirname, "../../skills");
  // A directory without SKILL.md is not installable: install-local-skills.sh
  // enumerates skills/*/SKILL.md, so the entrypoint file is what makes a
  // dispatch target real.
  const skillNames = new Set(
    readdirSync(skillsRoot).filter((d) => {
      const dir = resolve(skillsRoot, d);
      if (!statSync(dir).isDirectory()) return false;
      return existsSync(resolve(dir, "SKILL.md"));
    }),
  );
  expect(skillNames.size).toBeGreaterThan(25);

  const referenced = (doc: string): string[] => [
    ...new Set(
      [...doc.matchAll(/ha-nova:([a-z][a-z-]*)/g)].map((m) => m[1] as string),
    ),
  ];

  it("resolves every ha-nova:<skill> named in the dispatch table", () => {
    const context = read("skills/ha-nova/SKILL.md");
    const dispatch = context.split("## Skill Dispatch (Critical)")[1] ?? "";
    const names = referenced(dispatch);
    // Guard against a vacuous pass: if the heading or the reference syntax
    // ever changes, an empty extraction would satisfy the loop below without
    // checking anything.
    expect(dispatch.length).toBeGreaterThan(0);
    expect(names.length).toBeGreaterThanOrEqual(skillNames.size - 2);
    for (const name of names) {
      if (skillNames.has(name)) continue;
      expect.fail(`dispatch names ha-nova:${name}, which has no skills/${name}/`);
    }
  });

  it("resolves every owner named in the fallback capability map", () => {
    // The map's owner column uses BARE skill names ("read / write"), not the
    // ha-nova: prefix — so this parses that column rather than the prefix,
    // which is where a dead hand-off would actually appear.
    const fallback = read("skills/fallback/SKILL.md");
    const owners = fallback
      .split("\n")
      .filter((line) => line.startsWith("|") && /\|\s*(Covered|Relay-Ready)\s*\|/.test(line))
      .flatMap((line) => (line.split("|")[3] ?? "").split(/[/,]/))
      .map((token) => token.trim().replace(/^`|`$/g, ""))
      .filter((token) => /^[a-z][a-z-]*$/.test(token));

    expect(owners.length).toBeGreaterThan(15);
    for (const owner of owners) {
      // "this" = the fallback skill itself; multi-word prose is not an owner.
      if (owner === "this" || skillNames.has(owner)) continue;
      expect.fail(
        `fallback capability map assigns "${owner}", which has no skills/${owner}/`,
      );
    }
  });

  it("gives every skill in the tree a dispatch entry", () => {
    // The reverse direction: a shipped skill nothing routes to is dead weight
    // no user can reach. Scope this to the dispatch section — searching the
    // whole context skill would let a later example or hand-off stand in for
    // a missing dispatch row.
    const context = read("skills/ha-nova/SKILL.md");
    const dispatch = context.split("## Skill Dispatch (Critical)")[1] ?? "";
    // TABLE ROWS only. The section also holds example lines, and those are
    // not routing: deleting a row while an example still names the skill must
    // fail this test, not satisfy it.
    const rows = dispatch.split("\n").filter((line) => line.trim().startsWith("|"));
    expect(rows.length).toBeGreaterThan(20);
    const routed = new Set(referenced(rows.join("\n")));
    for (const name of skillNames) {
      if (name === "ha-nova") continue;
      expect(
        routed.has(name),
        `skills/${name}/ has no dispatch entry — an example or hand-off elsewhere does not make it routable`,
      ).toBe(true);
    }
  });
});
