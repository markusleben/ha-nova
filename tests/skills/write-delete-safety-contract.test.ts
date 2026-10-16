import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
const refactorGuide = readFileSync("skills/ha-nova/safe-refactoring.md", "utf8");
const writeSafety = (readFileSync("skills/ha-nova/write-safety.md", "utf8") + readFileSync("skills/ha-nova/recovery-coverage.md", "utf8") + readFileSync("skills/ha-nova/update-revert.md", "utf8"));
  const updateRevert = readFileSync("skills/ha-nova/update-revert.md", "utf8");
const configApply = readFileSync("cli/verb_config_apply.go", "utf8");

describe("write delete safety contract", () => {
  it("requires delete previews to surface consumer-check results before confirmation", () => {
    expect(writeSkill).toContain("Delete preview MUST include the consumer-check result before confirmation");
    // The CLI emits `consumers: []` only for a complete delete scan (plan_config.go).
    expect(writeSkill).toContain("`[]` is an explicit none");
    expect(writeSkill).toContain('absent (`coverage.complete` false) is "consumer check inconclusive", never a no-consumer result');
  });

  it("requires destructive writes to stay unclaimed until verification proves the target is gone", () => {
    expect(writeSkill).toContain("Do not report destructive success until verification proves the target is gone");
    expect(refactorGuide).toContain("A delete is not done until follow-up verification confirms the target is gone");
    expect(refactorGuide).toContain("Do not present a destructive change as complete when consumer impact is still unresolved");
  });

  it("wires pre-write diff, pre-write impact, and update-revert into the write flow", () => {
    expect(writeSkill).toContain("Changes slot");
    expect(writeSkill).toContain("Pre-Write Impact");
    expect(writeSkill).toContain("Update-Revert");
    expect(writeSkill).toContain("../ha-nova/write-safety.md");
    // YAML is opt-in, not dumped by default, for a non-technical audience.
    expect(writeSkill).toContain("show yaml");
    expect(writeSkill).toContain("cancel");
  });

  it("requires an understandable preview and honest verification wording (issue #274)", () => {
    // The diff states WHAT changes; the summary must state what it DOES —
    // confirmation of an incomprehensible preview is not informed consent.
    expect(writeSafety).toContain("### Behavior narrative (required with every update preview)");
    expect(writeSafety).toContain("never ask for confirmation of a change you cannot describe");
    expect(writeSafety).toContain(
      "the summary MUST name what\nwas added, removed, replaced, modified, or nested there",
    );
    expect(writeSkill).toContain("state the behavioral effect in the summary sentence");
  });

  it("blocks confirmation of count-only or type-only previews (issue #390)", () => {
    // The narrative is diff-scoped, per-entry, and nested-aware; a count or
    // type-name transition alone never reaches confirmation.
    expect(writeSafety).toContain("The narrative covers every collection\nthe diff touches");
    expect(writeSafety).not.toContain("for something\nthe user's request touched");
    expect(writeSafety).toContain("Each added, removed, replaced, or modified entry");
    expect(writeSafety).toContain("entries with one shared effect may\nshare a line");
    expect(writeSafety).toContain("at the depth needed to understand its effect");
    expect(writeSafety).toContain("container counts alone never\nqualify");
    expect(writeSafety).toContain("not a description");
    expect(writeSafety).toContain("is not a valid confirmation basis");
    expect(writeSafety).toContain("it may run longer when per-entry coverage requires it");
    // The gate lives with the confirmation owner in the context skill.
    const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    expect(contextSkill).toContain(
      "A preview is a valid confirmation basis only if it explains the behavioral effect of every collection it touches.",
    );
    expect(contextSkill).toContain("cannot proceed to confirmation");
    // Flows with terse local preview wording carry the narrative pointer.
    const helper = readFileSync("skills/helper/SKILL.md", "utf8");
    const scene = readFileSync("skills/scene/SKILL.md", "utf8");
    const dashboard = readFileSync("skills/dashboard/SKILL.md", "utf8");
    const fallback = readFileSync("skills/fallback/SKILL.md", "utf8");
    expect(helper).toContain("a count-only diff row never stands alone");
    expect(helper).toContain(
      "plus the behavior narrative (write-safety → Behavior narrative). Include an explicit not-saved-yet line",
    );
    expect(scene).toContain("never entity counts alone");
    expect(dashboard).toContain("plus a plain-language behavior line");
    expect(fallback).toContain("with a plain-language behavior line");
    // Post-write checks prove persistence, not behavior — wording must say so.
    expect(writeSafety).toContain("## Verification Honesty (post-write wording)");
    expect(writeSafety).toContain('Never a bare "verified"');
    expect(writeSafety).toContain("Runtime behavior was not exercised");
    expect(writeSafety).toContain("it may actuate real devices; never run it unrequested");
    // One logical change over several targets: plan first, honest revert scope.
    expect(writeSafety).toContain("## Multi-Target Changes (one logical change, several items)");
    expect(writeSafety).toContain("update-revert keeps the");
    expect(writeSafety).toContain("last 5 updated targets");
    expect(writeSafety).toContain("one step back per");
    expect(updateRevert).toContain("snapshot show --target <target_id>");
    expect(writeSafety).toContain("snapshot show --list");
    expect(writeSafety).toContain("never continue silently into a half-applied state");
    expect(writeSkill).toContain("Multi-target logical changes: present the plan first");
  });

  it("defines diff + durable update-revert in the shared owner doc", () => {
    expect(writeSafety).toContain("## Changes");
    expect(writeSafety).toContain("Update-Revert");
    // Drift baseline is the post-write verified read-back, not the draft.
    expect(writeSafety).toContain("expected_after");
    expect(writeSafety).toContain("complete post-write verified read-back");
    // The snapshot must keep the COMPLETE read-back body, not core fields only —
    // dropping a non-empty field (description/max) reads as drift and blocks revert.
    expect(writeSafety).toContain("complete");
    expect(writeSafety).toContain("**not** reduce it to core fields");
    expect(writeSafety).toContain("ha-nova snapshot verify");
    // Restore goes through the normal write path; create/delete fall back to backups.
    expect(updateRevert).toContain("Home Assistant Backups");
  });

  it("documents safety-mechanism availability per skill and backup pointers where revert is absent", () => {
    const organizeSkill = readFileSync("skills/organize/SKILL.md", "utf8");
    const dashboardSkill = readFileSync("skills/dashboard/SKILL.md", "utf8");
    const fallbackSkill = readFileSync("skills/fallback/SKILL.md", "utf8");
    // The diff/revert asymmetry is documented, not implicit.
    expect(writeSafety).toContain("## Safety-Mechanism Availability by Skill");
    expect(writeSafety).toContain("Never imply a mechanism a");
    // Skills without revert point to HA Backups before destructive writes.
    expect(organizeSkill).toContain("Registry deletes are irreversible");
    expect(organizeSkill).toContain("Home Assistant Backups (Settings > System > Backups)");
    expect(dashboardSkill).toContain("`revert` undoes the last verified save");
    expect(dashboardSkill).toContain("Dashboard deletes restore partially from their snapshot");
    expect(dashboardSkill).toContain("resources recover via Home Assistant Backups");
    // Fallback full-replace writes verify survival of unrelated content after write.
    expect(fallbackSkill).toContain("verify both the intended change and the survival of unrelated content");
    expect(fallbackSkill).toContain("verify the pre-existing list items survived");
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

  it("keeps scratch files out of user-facing output", () => {
    const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8") + readFileSync("skills/ha-nova/output-cards.md", "utf8");
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(outputRules).toContain("Scratch payload/filter/result files are internal execution artifacts");
    expect(outputRules).toContain("never mentioned (paths, \"edited files\", payload or jq files)");
    expect(outputRules).toContain("never under the repo working tree");
    expect(outputRules).toContain("absolute scratch paths in visible command text");
    expect(outputRules).toContain("scratch location/tooling was wrong");
    expect(outputRules).toContain('"edited files"');
    expect(context).toContain("Scratch files are internal");
    expect(context).toContain("Do not create them under the repo working tree");
    expect(context).toContain("client-private scratch storage outside the project workspace");
    expect(context).toContain("never allocate scratch directories or files from visible shell commands");
    expect(context).toContain("set the tool working directory to the scratch directory outside the command text");
    expect(context).toContain("local filenames, not absolute scratch paths");
    // PR6: the agent templates are gone; the write draft is a client-private scratch file.
    expect(writeSkill).toContain("one JSON object in a client-private scratch\nfile");
  });

  it("renders the diff deterministically via the CLI, printed verbatim", () => {
    expect(writeSafety).toContain("ha-nova diff");
    expect(writeSafety).toContain("--out <diff-file>");
    expect(writeSafety).toContain("verbatim");
    // Automation/script diffs come from `ha-nova plan write`.
    expect(writeSkill).toContain("the plan's `diff` rows **verbatim** in the Changes slot");
    expect(writeSafety).toContain("prefer `--out <diff-file>`");
  });

  it("splits the change table: agent-authored localized header, verbatim CLI rows", () => {
    // The CLI emits data rows only; the two header lines are the agent's — and
    // the ONLY hand-written lines inside the table. This split keeps
    // Before/After in the user's language while every fact stays byte-identical
    // CLI output.
    expect(writeSafety).toContain("| Field | before | after |");
    expect(writeSafety).toContain("exactly two hand-written");
    expect(writeSafety).toContain("the localized table header row");
    expect(writeSafety).toContain("|---|---|---|");
    expect(writeSafety).toContain("do not re-align pipes or pad cells");
    // The truncation marker joins the count-only rule: a cut-off value the
    // request touched must be named in the summary.
    expect(writeSafety).toContain("a truncated (`…`) value");
  });

  it("protects notification copy from unrequested rewrites", () => {
    expect(writeSafety).toContain("User-authored notification copy");
    expect(writeSafety).toContain("Notification text is user-authored copy");
    expect(writeSafety).toContain("Do not silently restyle, relocalize, shorten, expand, or restructure existing");
    expect(writeSafety).toContain("notification text during a rename, refactor, timing change");
    expect(writeSafety).toContain("A count-only array line");
    expect(writeSkill).toContain("preserve notification titles, messages, templates, metadata");
  });

  it("forces the diff/snapshot tools to be run, not hand-computed", () => {
    // The diff must be RUN and printed verbatim — never authored from context.
    expect(writeSkill).toContain("never write it yourself");
    expect(writeSafety).toContain("There is no hand-computed fallback");
    // The example is de-realized so it cannot be copied as a hand-rendered block.
    expect(writeSafety).toContain("paste the CLI diff rows here, unchanged");
    // PR6: `apply` checkpoints an update itself; revert reads before_config only from it.
    expect(writeSkill).toContain("lands on the undo stack automatically");
    expect(configApply).toContain("saveCheckpoint(paths, s, env, action, ctx, expected)");
    expect(updateRevert).toContain("only** source of `before_config`");
    expect(updateRevert).toContain("reconstruct the previous config from memory");
  });

  it("keeps delete a typed confirmation code even under menu pressure", () => {
    expect(writeSkill).toContain("delete is the one-time code, never a menu");
  });

  it("requires the typed confirmation code for same-session cleanup", () => {
    expect(writeSkill).toContain("Destructive cleanup still requires the one-time code");
    expect(writeSafety).toContain("even when the item was created earlier in the same session");
    expect(refactorGuide).toContain("cleanup, undo-create, orphan cleanup, failed-create cleanup");
  });

  it("treats post-delete absence evidence as successful verification without alternate delete retries", () => {
    // PR6: `ha-nova apply` verifies a delete by absence (config 404 and no
    // entity state); a delete answered 400/404 because the item was already
    // gone is verified with a note; nothing is ever retried.
    expect(configApply).toContain('row.Expect = "config and entity gone"');
    expect(configApply).toContain("status == http.StatusNotFound && st == nil");
    expect(configApply).toContain("already gone when the delete arrived");
    expect(writeSkill).toContain("Never retry after exit 6 or 7.");
  });
});
