import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// #478: first-class HACS lifecycle per docs/archive/work/2026-08-04-hacs-lifecycle-spec.md —
// schema-guarded command map, UNKNOWN-outcome reconcile loops, consumer
// discovery before every uninstall, migration backup gate, category-appropriate
// verification, explicit update ownership.

const skill = readFileSync("skills/hacs/SKILL.md", "utf8");
const commands = readFileSync("skills/hacs/hacs-commands.md", "utf8");
const normalized = `${skill}\n${commands}`.replace(/\s+/g, " ");

describe("hacs lifecycle skill (#478)", () => {
  it("guards the private schema and never leaves the Relay path", () => {
    expect(skill).toContain("name: hacs");
    expect(skill).toContain("description: Use when");
    expect(normalized).toContain("read `hacs/info` first");
    expect(normalized).toContain(
      "fail closed with the HACS-UI pointer on an unrecognized",
    );
    expect(normalized).toContain("Never guess schemas");
    expect(normalized).toContain("never edit `.storage`");
    expect(normalized).toContain("never SSH");
    expect(skill).not.toContain("ssh ");
  });

  it("treats timeouts as unknown outcomes with no blind retries", () => {
    expect(normalized).toContain("UNKNOWN outcome");
    expect(normalized).toContain("settle window");
    expect(normalized).toContain(
      "Non-idempotent mutations NEVER auto-retry",
    );
    expect(normalized).toContain("re-reading identity before every retry");
  });

  it("separates the three lifecycle objects and verifies category-appropriately", () => {
    expect(normalized).toContain(
      "Registration, downloaded files, and config entries are three distinct lifecycle objects",
    );
    expect(normalized).toContain("INSTALLED");
    expect(normalized).toContain("ACTIVE");
    expect(normalized).toContain("requires a Home Assistant restart");
    expect(normalized).toContain("storage resource mode");
    expect(normalized).toContain("user-managed");
  });

  it("runs consumer discovery before every uninstall and gates migrations on a backup", () => {
    expect(normalized).toContain("EVERY uninstall/removal");
    expect(normalized).toContain("search/related");
    expect(normalized).toContain("Lovelace resource");
    expect(normalized).toContain("template consumers");
    expect(normalized).toContain("unscanned");
    expect(normalized).toContain("COMPLETED and CURRENT");
    expect(normalized).toContain("full Home Assistant Backup");
  });

  it("keeps version choices explicit and confirmation gates tiered", () => {
    // Branch-only repositories install from the default branch without a
    // version and verify the commit-ish installed_version.
    expect(normalized).toContain("Branch-only repositories");
    expect(normalized).toContain("verify against the commit-ish `installed_version`");
    expect(normalized).toContain("never silently replaced by a newer release");
    expect(normalized).toContain("prerelease only on explicit request");
    expect(skill).toContain("Delete Card with the one-time code");
    // Install/update/redownload brings third-party code: the one-time code
    // (maintainer decision 2026-10-25, chat-parity spec Decisions 3).
    expect(normalized).toContain(
      "the one-time code for install/update/redownload",
    );
    expect(normalized).not.toContain("natural confirmation for install");
  });

  it("carries the canonical mutation Safety Core and file-based relay usage", () => {
    expect(skill).toContain(
      "- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary`; everything else waits for the user's confirmation of the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes or other destructive operations need the user's window click or phone tap, else their one-time code from HA NOVA; \"yes\" or any other reply is invalid.",
    );
    expect(skill).toContain(
      "Home Assistant is reached exclusively through the `ha-nova` CLI.",
    );
    expect(skill).toContain("--data-file");
    expect(skill).toContain("--out <result-file>");
    expect(skill).toContain("../ha-nova/session-bootstrap.md");
  });

  it("wires ownership across fallback, updates, dispatch, and safety surfaces", () => {
    const fallback = readFileSync("skills/fallback/SKILL.md", "utf8");
    expect(fallback).toContain(
      "| HACS (registration, download, version switching, uninstall, migration) | Covered | hacs |",
    );
    expect(fallback).toContain("never probe\n`hacs/*` WS commands");
    const updates = readFileSync("skills/updates/SKILL.md", "utf8");
    expect(updates).toContain("(`ha-nova:hacs`)");
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    expect(context).toContain("| browse or list installed HACS packages, install, pin or downgrade to a specific version, redownload, or remove HACS packages");
    const writeSafety = (readFileSync("skills/ha-nova/write-safety.md", "utf8") + readFileSync("skills/ha-nova/recovery-coverage.md", "utf8") + readFileSync("skills/ha-nova/update-revert.md", "utf8"));
    expect(writeSafety).toContain("| `hacs` |");
    const snapshots = readFileSync("skills/ha-nova/config-snapshots.md", "utf8");
    expect(snapshots).toContain("HACS package/registration state");
    const architecture = readFileSync(
      "docs/reference/skill-architecture.md",
      "utf8",
    );
    expect(architecture).toContain("34 independent sub-skills");
    expect(architecture).toContain("`ha-nova:hacs` owns the HACS package lifecycle");
  });
});
