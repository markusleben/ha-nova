import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
const refactorGuide = readFileSync("skills/ha-nova/safe-refactoring.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const applyAgent = readFileSync("skills/ha-nova/agents/apply-agent.md", "utf8");
const resolveAgent = readFileSync("skills/ha-nova/agents/resolve-agent.md", "utf8");

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
    expect(writeSkill).toContain("Changes slot");
    expect(writeSkill).toContain("Pre-Write Impact");
    expect(writeSkill).toContain("Update-Revert");
    expect(writeSkill).toContain("skills/ha-nova/write-safety.md");
    // YAML is opt-in, not dumped by default, for a non-technical audience.
    expect(writeSkill).toContain("show yaml");
    expect(writeSkill).toContain("cancel");
  });

  it("requires an understandable preview and honest verification wording (issue #274)", () => {
    // The diff states WHAT changes; the summary must state what it DOES —
    // confirmation of an incomprehensible preview is not informed consent.
    expect(writeSafety).toContain("### Behavior narrative (required with every update preview)");
    expect(writeSafety).toContain("never ask for confirmation of a change you cannot describe");
    expect(writeSafety).toContain("the summary MUST name what was added, removed, or nested there");
    expect(writeSkill).toContain("state the behavioral effect in the summary sentence");
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
    expect(writeSafety).toContain("snapshot show --target <target_id>");
    expect(writeSafety).toContain("snapshot show --list");
    expect(writeSafety).toContain("never continue silently into a half-applied state");
    expect(writeSkill).toContain("Multi-target logical changes: present the plan first");
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
    expect(dashboardSkill).toContain("Dashboard writes have no `revert`");
    expect(dashboardSkill).toContain("the recovery path is Home Assistant Backups");
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
    const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8");
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(outputRules).toContain("Scratch payload/filter/result files are internal execution artifacts");
    expect(outputRules).toContain("Do not mention or echo scratch file paths");
    expect(outputRules).toContain("Do not create them under the repo working tree");
    expect(outputRules).toContain("visible command text contains absolute scratch paths");
    expect(outputRules).toContain("scratch location/tooling was wrong");
    expect(outputRules).toContain('"edited files"');
    expect(context).toContain("Scratch files are internal");
    expect(context).toContain("Do not create them under the repo working tree");
    expect(context).toContain("client-private scratch storage outside the project workspace");
    expect(context).toContain("never allocate scratch directories or files from visible shell commands");
    expect(context).toContain("set the tool working directory to the scratch directory outside the command text");
    expect(context).toContain("local filenames, not absolute scratch paths");
    expect(resolveAgent).toContain("Do not allocate scratch directories or files from visible shell commands");
    expect(resolveAgent).toContain("Do not create relay scratch files under the repo working tree");
    expect(resolveAgent).toContain("set the tool working directory to the scratch directory outside the command text");
    expect(resolveAgent).toContain("local filenames, not absolute scratch paths");
    expect(applyAgent).toContain("do not allocate scratch directories or files from visible shell commands");
    expect(applyAgent).toContain("Do not create relay scratch files under the repo working tree");
    expect(applyAgent).toContain("set the tool working directory to the scratch directory outside the command text");
    expect(applyAgent).toContain("local filenames, not absolute scratch paths");
    expect(applyAgent).toContain("never describe them as user-facing edits");
  });

  it("renders the diff deterministically via the CLI, printed verbatim", () => {
    expect(writeSafety).toContain("ha-nova diff");
    expect(writeSafety).toContain("--out <diff-file>");
    expect(writeSafety).toContain("verbatim");
    expect(writeSkill).toContain("ha-nova diff");
    expect(writeSkill).toContain("prefer `--out <diff-file>`");
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
    expect(writeSkill).toContain("**run** `ha-nova diff`");
    expect(writeSkill).toContain("never write it yourself");
    expect(writeSafety).toContain("There is no hand-computed fallback");
    // The example is de-realized so it cannot be copied as a hand-rendered block.
    expect(writeSafety).toContain("paste the ha-nova diff file/stdout");
    // Snapshot save is an explicit command; revert reads before_config only from it.
    expect(writeSkill).toContain("**run `ha-nova snapshot save`**");
    expect(writeSafety).toContain("only** source of `before_config`");
    expect(writeSafety).toContain("reconstruct the previous config from memory");
  });

  it("keeps delete a typed token even under menu pressure", () => {
    expect(writeSkill).toContain("delete is the typed token, never a menu");
  });

  it("requires tokenized delete confirmation for same-session cleanup", () => {
    expect(writeSkill).toContain("Destructive cleanup still requires `confirm:<token>`");
    expect(writeSafety).toContain("even when the item was created earlier in the same session");
    expect(refactorGuide).toContain("cleanup, undo-create, orphan cleanup, failed-create cleanup");
  });

  it("treats post-delete absence evidence as successful verification without alternate delete retries", () => {
    expect(applyAgent).toContain("config read-back not-found after DELETE is expected absence evidence");
    expect(applyAgent).toContain("entity state not-found after DELETE is expected absence evidence");
    expect(applyAgent).toContain("`config/entity_registry/get` may return `UPSTREAM_WS_ERROR` (or `UPSTREAM_WS_COMMAND_ERROR` on Relay App >= 0.2.4) after deletion");
    expect(applyAgent).toContain("do not retry alternate deletes");
    expect(applyAgent).toContain("config/entity_registry/list_for_display");
    expect(applyAgent).toContain("no exact `entity_id` match");
  });
});
