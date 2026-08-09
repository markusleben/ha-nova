// tests/skills/reference-truth-contract.test.ts
//
// #516/#517: the two documents agents consult to learn what EXISTS. A missing
// row here does not produce an error — it produces an agent that improvises,
// or one that tells the user a capability is unavailable when it ships.
import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync, statSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const fallback = read("skills/fallback/SKILL.md");
const matrix = read("docs/reference/ha-api-matrix.md");
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("fallback capability map is complete and truthful (#516)", () => {
  it("covers the surfaces that previously had no row at all", () => {
    // Flow step 1 looks the request up in this map; no row means no tier.
    for (const row of [
      "Integration entry lifecycle",
      "Matter / Thread",
      "Assist custom sentences",
      "Creating a calendar",
      "Device category assignment",
    ]) {
      const line = fallback
        .split("\n")
        .find((l) => l.startsWith("|") && l.includes(row));
      expect(line, `capability map has no row for "${row}"`).toBeTruthy();
    }
  });

  it("splits every status the Flow branches on, so no claim is unreachable", () => {
    // Flow step 1 reads the STATUS column. A Relay-Ready claim in prose under
    // a Roadmap/External row is never reached.
    const rows = fallback.split("\n").filter((l) => l.startsWith("|"));
    const find = (needle: string) => rows.find((r) => r.includes(needle));
    expect(find("Event capture — bounded window")).toContain("Relay-Ready");
    expect(find("Event streaming — continuous")).toContain("Roadmap");
    expect(find("Matter / Thread status")).toContain("Relay-Ready");
    expect(find("Matter / Thread commissioning")).toContain("External");
    // A Relay-Ready row needs mechanics the Flow can execute.
    expect(fallback).toContain("### Assist Custom Sentences -- RELAY-READY");
    expect(fallback).toContain("### Matter And Thread Status -- RELAY-READY");
    expect(flat(fallback)).toContain("`/config/custom_sentences/<lang>/<name>.yaml` — NOT `/config/ha_nova/`");
    // Detecting the outage is not handling it: a failed test must restore.
    expect(flat(fallback)).toContain("restore immediately");
    expect(flat(fallback)).toContain("re-test one known-good phrase before reporting");
  });

  it("teaches the owning skill the bounded callback path it advertises", () => {
    // Advertising a capability in fallback while the skill that owns the user
    // flow still says it is impossible leaves it unreachable.
    const notify = flat(read("skills/notify/SKILL.md"));
    expect(notify).toContain("A bounded in-chat window can CATCH a tap");
    expect(notify).toContain("say how long you will wait");
    // The relay is request/response: the window opens AFTER the send and HA
    // does not replay the event, so an empty window is not proof.
    expect(notify).toContain("cannot guarantee one");
    expect(notify).toContain("a press in that gap is simply gone");
    expect(notify).toContain("never report an empty window as proof that nobody tapped");
  });

  it("stops claiming bounded event capture is unavailable", () => {
    // The envelope ships and ha-nova:mqtt uses it; only continuous streams
    // are still blocked.
    const f = flat(fallback);
    expect(f).toContain("CONTINUOUS streams are Phase 1c");
    expect(f).toContain("BOUNDED capture already works");
    expect(f).toContain("`ha-nova:mqtt` uses exactly this pattern");
    expect(f).not.toContain("Blocked by: No SSE streaming endpoint in Relay.");
  });

  it("corrects the Supervisor premise instead of repeating it", () => {
    const f = flat(fallback);
    expect(f).toContain("is only half true");
    expect(f).toContain("the `hassio` integration proxies it under `/api/hassio/*`");
    // Management stays external; lifecycle and updates have owners.
    expect(f).toContain("App *management* (install, uninstall, configure, store browsing) is Supervisor");
    expect(f).toContain("App UPDATES are `ha-nova:updates`");
  });

  it("routes the durable actionable-notification path instead of shelving it", () => {
    const row = fallback
      .split("\n")
      .find((l) => l.startsWith("|") && l.includes("Actionable-notification"));
    expect(row).toBeTruthy();
    expect(row).toContain("write");
    expect(row).not.toContain("Roadmap");
  });

  it("warns that removing a config entry takes its devices with it", () => {
    const f = flat(fallback);
    expect(f).toContain("### Integration Entry Lifecycle -- RELAY-READY");
    expect(f).toContain(
      "deletes every device and entity that entry owns and is not undoable",
    );
    expect(f).toContain("take the typed confirmation code");
  });
});

describe("ha-api-matrix lists the surfaces skills actually pin (#517)", () => {
  it("names the families that were missing entirely", () => {
    for (const family of [
      "`search/related`",
      "`system_log/list`",
      "`config/entity_registry/list_for_display`",
      "backup/generate",
      "`person/*`",
      "assist_pipeline/pipeline/list",
      "recorder/validate_statistics",
      "device_automation/trigger/list",
      "`mqtt/subscribe`",
      "media_source/resolve_media",
      "`camera/stream`",
      "persistent_notification/get",
      "update/release_notes",
      "`logger/log_info`",
      "`diagnostics/list`",
      "`todo/item/move`",
      "/api/conversation/process",
    ]) {
      expect(matrix, `matrix does not mention ${family}`).toContain(family);
    }
  });

  it("says what the document is for, so it is not read as a command inventory", () => {
    const m = flat(matrix);
    expect(m).toContain("It is not a command inventory");
    expect(m).toContain("holds the calling contract");
    // HACS is not an HA API — saying so stops it being filed as a gap.
    expect(m).toContain("not a Home Assistant API");
  });

  it("drops the row no skill ever used", () => {
    expect(matrix).not.toContain("| `config/automation/list` |");
  });

  it("assigns every owning skill in the new table to a real skill", () => {
    const skillsRoot = resolve(__dirname, "../../skills");
    const skills = new Set(
      readdirSync(skillsRoot).filter((d) =>
        statSync(resolve(skillsRoot, d)).isDirectory(),
      ),
    );
    const section = matrix.split("## Surfaces the skills pin (grouped)")[1] ?? "";
    expect(section.length).toBeGreaterThan(0);
    const owners = section
      .split("\n")
      .filter((l) => l.startsWith("|"))
      .map((l) => (l.split("|")[3] ?? "").trim())
      .flatMap((cell) => cell.split(/[,/]/))
      .map((t) => t.trim().replace(/^`|`$/g, ""))
      .filter((t) => /^[a-z][a-z-]+$/.test(t));
    expect(owners.length).toBeGreaterThan(10);
    for (const owner of owners) {
      if (skills.has(owner)) continue;
      // prose words in the owner cell ("every write-capable skill", "flows")
      if (["every", "write-capable", "skill", "flows", "and", "bulk"].includes(owner)) continue;
      expect.fail(`matrix assigns "${owner}", which has no skills/${owner}/`);
    }
  });
});
