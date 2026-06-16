import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
const refactorGuide = readFileSync("skills/ha-nova/safe-refactoring.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");

describe("write delete safety contract", () => {
  it("requires delete previews to surface consumer-check results before confirmation", () => {
    expect(writeSkill).toContain("Delete preview MUST include the consumer-check result before confirmation");
    expect(writeSkill).toContain("either the affected consumers or an explicit no-consumer result");
  });

  it("requires destructive writes to stay unclaimed until verification proves the target is gone", () => {
    expect(writeSkill).toContain("Do not report destructive success until verification proves the target is gone");
    expect(refactorGuide).toContain("A delete is not done until follow-up verification confirms the target is gone");
    expect(refactorGuide).toContain("Do not present a destructive change as complete when consumer impact is still unresolved");
  });

  it("wires pre-write diff, pre-write impact, and update-revert into the write flow", () => {
    expect(writeSkill).toContain("## Changes");
    expect(writeSkill).toContain("Pre-Write Impact");
    expect(writeSkill).toContain("Update-Revert");
    expect(writeSkill).toContain("skills/ha-nova/write-safety.md");
    // YAML is opt-in, not dumped by default, for a non-technical audience.
    expect(writeSkill).toContain("show yaml");
  });

  it("defines diff + durable update-revert in the shared owner doc", () => {
    expect(writeSafety).toContain("## Changes");
    expect(writeSafety).toContain("Update-Revert");
    // Drift baseline is the post-write verified read-back, not the draft.
    expect(writeSafety).toContain("expected_after");
    expect(writeSafety).toContain("VERIFICATION.observed");
    // The snapshot must keep the COMPLETE read-back body, not core fields only —
    // dropping a non-empty field (description/max) reads as drift and blocks revert.
    expect(writeSafety).toContain("complete");
    expect(writeSafety).toContain("**not** reduce it to core fields");
    expect(writeSafety).toContain("ha-nova snapshot verify");
    // Restore goes through the normal write path; create/delete fall back to backups.
    expect(writeSafety).toContain("Home Assistant Backups");
  });

  it("forbids surfacing internal bookkeeping in write previews/results", () => {
    expect(writeSafety).toContain("Output hygiene");
    // The live test leaked a raw config-id and "Best-Practice-Snapshot is older".
    expect(writeSafety).toContain("no raw numeric config-id");
    expect(writeSafety).toContain("bp_status");
    expect(writeSafety).toContain("internal gate input only");
    // On a simple change, staleness must be silent (no jargon in user output).
    expect(writeSafety).toContain("continue silently");
  });

  it("renders the diff deterministically via the CLI, printed verbatim", () => {
    expect(writeSafety).toContain("ha-nova diff");
    expect(writeSafety).toContain("verbatim");
    expect(writeSkill).toContain("ha-nova diff");
  });

  it("forces the diff/snapshot tools to be run, not hand-computed", () => {
    // The diff must be RUN and printed verbatim — never authored from context.
    expect(writeSkill).toContain("**run** `ha-nova diff`");
    expect(writeSkill).toContain("never write it yourself");
    expect(writeSafety).toContain("There is no hand-computed fallback");
    // The example is de-realized so it cannot be copied as a hand-rendered block.
    expect(writeSafety).toContain("paste the ha-nova diff stdout");
    // Snapshot save is an explicit command; revert reads before_config only from it.
    expect(writeSkill).toContain("**run `ha-nova snapshot save`**");
    expect(writeSafety).toContain("only** source of `before_config`");
    expect(writeSafety).toContain("reconstruct the previous config from memory");
  });

  it("keeps delete a typed token even under menu pressure", () => {
    expect(writeSkill).toContain("delete is the typed token, never a menu");
  });
});
