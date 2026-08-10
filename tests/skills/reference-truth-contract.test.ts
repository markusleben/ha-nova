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
const fallback = read("skills/fallback/SKILL.md") + "\n" + read("skills/fallback/relay-ready.md");
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
    // conversation.reload does not load a new intent_script; testing before
    // that lands makes a valid sentence file look broken and rolls it back.
    expect(flat(fallback)).toContain("reloads the SENTENCE matcher");
    expect(flat(fallback)).toContain("do not run the phrase test before the restart");
    expect(flat(fallback)).toContain("restore immediately");
    expect(flat(fallback)).toContain("re-test one known-good phrase before reporting");
  });

  it("teaches the owning skill the bounded callback path it advertises", () => {
    // Advertising a capability in fallback while the skill that owns the user
    // flow still says it is impossible leaves it unreachable.
    const notify = flat(read("skills/notify/SKILL.md"));
    expect(notify).toContain("A bounded in-chat window can CATCH a tap");
    expect(notify).toContain("Say how long you will wait");
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

  it("validates configuration.yaml before reloading, and tells the truth about intent_script", () => {
    const fallback = flat(read("skills/fallback/SKILL.md") + "\n" + read("skills/fallback/relay-ready.md"));
    // An invalid configuration.yaml survives the failed reload and blocks the
    // NEXT boot — unrecoverable from inside this tool.
    expect(fallback).toContain("POST /api/config/core/check_config` FIRST");
    expect(fallback).toContain("stops Home Assistant from starting on the next boot");
    expect(fallback).toContain("restore that file's `.bak` immediately");
    // Verified against a live 2026.8.0 instance: /api/services exposes no
    // intent_script domain at all, so nothing reloads those handlers.
    expect(fallback).toContain("Home Assistant registers no");
    expect(fallback).toContain("`intent_script.reload`");
    expect(fallback).toContain("It takes a restart");
  });

  it("keeps every skill that writes configuration.yaml on the validate-first path", () => {
    // fallback's file mechanics live in its relay-ready split.
    const writers: Array<[string, string]> = [
      ["skills/fallback", read("skills/fallback/SKILL.md") + read("skills/fallback/relay-ready.md")],
      ["skills/yaml-config/SKILL.md", read("skills/yaml-config/SKILL.md")],
    ];
    for (const [name, text] of writers) {
      expect(flat(text), `${name} writes configuration.yaml`).toContain("check_config");
    }
  });

  it("gives every capability status a Flow branch", () => {
    const fallbackDoc = read("skills/fallback/SKILL.md");
    const flow = flat(fallbackDoc);
    // Statuses used in the map must be reachable in the Flow, or a row selects
    // a branch that does not exist.
    expect(flow).toContain('If "Not a Home Assistant surface"');
    expect(flow).toContain("branch on the canonical word");
    expect(flow).toContain("no endpoint to find");

    // Scope to the Capability Map: other tables in this file have their own
    // second column and are not statuses.
    const mapSection = (fallbackDoc.split("## Capability Map")[1] ?? "").split(/\n## /)[0] ?? "";
    expect(mapSection.length).toBeGreaterThan(500);
    const CANONICAL = ["Covered", "Relay-Ready", "Roadmap", "External", "Not a Home Assistant surface"];
    for (const row of mapSection.split("\n").filter((l) => l.startsWith("|"))) {
      const status = (row.split("|")[2] ?? "").trim();
      if (!status || status === "Status" || /^-+$/.test(status)) continue;
      expect(
        CANONICAL.some((c) => status.startsWith(c)),
        `capability status "${status}" has no Flow branch`,
      ).toBe(true);
    }
  });

  it("routes each matrix surface only to a skill that pins it", () => {
    const matrix = read("docs/reference/ha-api-matrix.md");
    const row = matrix.split("\n").find((l) => l.includes("`system_log/list`") && l.startsWith("| System log"));
    expect(row, "System log row missing").toBeTruthy();
    // health neither pins the command nor lists logs in its Relay contract;
    // skill-architecture.md assigns system logs to diagnose alone.
    expect(row).not.toContain("health");
    expect(read("skills/diagnose/SKILL.md")).toContain("system_log/list");
  });

  it("advertises only the lifecycle operations it documents mechanics for", () => {
    const fallbackAll = flat(read("skills/fallback/SKILL.md") + read("skills/fallback/relay-ready.md"));
    // A Relay-Ready row is a promise that this skill can perform it. Enable,
    // options and reconfigure have no documented mechanics, so they are
    // External rather than a row that routes into a section that cannot serve.
    expect(fallbackAll).toContain("Integration entry lifecycle (reload, remove)");
    expect(fallbackAll).toContain("Integration entry enable/disable, options, reconfigure | External");
    // The section behind the row must claim the same scope as the row.
    expect(fallbackAll).toContain("Reload and remove for an existing config entry — those two only");
    // Every Relay-Ready row needs a section behind it, event capture included.
    expect(fallbackAll).toContain("Bounded Event Capture -- RELAY-READY");
    expect(fallbackAll).toContain("do not invent a bare subscription");
    // ws-proxy.ts caps collect_events.timeout_ms at 10_000, and ws-client.ts
    // sets truncated on ANY limit close — including a normal window timeout.
    expect(fallbackAll).not.toContain('"timeout_ms": 15000');
    expect(fallbackAll).toContain("capped at **10000**");
    // An entry id is not a search/related item type — the counts come from
    // the registries, filtered on config_entry_id.
    expect(fallbackAll).toContain("An entry id is not a related-item type at all");
    // Live device rows carry a config_entries ARRAY; entity rows carry the
    // singular config_entry_id. Matching a device by equality undercounts.
    expect(fallbackAll).toContain("a DEVICE row carries a `config_entries` ARRAY");
    expect(fallbackAll).toContain("match on membership in that array, not on equality");
    // A device-level search/related misses its child entities' consumers.
    expect(fallbackAll).toContain("per DEVICE **and** per ENTITY");
    expect(fallbackAll).toContain("Neither covers the other");
    expect(fallbackAll).toContain("does not index dashboards");
    // A button's event type is per-integration; a shared device is detached,
    // not deleted.
    expect(fallbackAll).toContain("Resolve the event type from the button's own integration first");
    // An entity-less remote has no platform to read.
    expect(fallbackAll).toContain("has no `platform` to read");
    expect(fallbackAll).toContain("ASK which integration the remote belongs to");
    expect(fallbackAll).toContain("which hold opaque entry ids");
    expect(fallbackAll).toContain("take the entry's `domain`");
    // A user-reported press that produced nothing is a mistimed window, not
    // proof of silence.
    expect(fallbackAll).toContain("offer one retry with the press timed after you confirm");
    expect(fallbackAll).toContain("a modern `event.*` entity fires no bus event at");
    // Naming the exception is not enough without the payload for it.
    expect(fallbackAll).toContain('"type": "subscribe_trigger"');
    expect(fallbackAll).toContain("puts the button in `attributes.event_type`");
    expect(fallbackAll).toContain("Split the count — deleted versus detached");
    expect(fallbackAll).toContain("which no entity scan sees");
    // Verified on the live instance: list_for_display rows are
    // ei/pl/lb/di/tk/ec/hn/en — no config_entry_id.
    expect(fallbackAll).toContain('`{"type":"config/entity_registry/list"}`');
    expect(fallbackAll).toContain("The compact `list_for_display` cannot answer this");
    expect(fallbackAll).toContain("it is not evidence that events were missed");
    expect(fallbackAll).toContain("point at Settings > Devices & services instead of improvising a payload");
    // get_states is not how any listed owner reads the compact registry.
    const row = read("docs/reference/ha-api-matrix.md")
      .split("\n")
      .find((l) => l.startsWith("| Compact registry"));
    expect(row).not.toContain("get_states");
  });

  it("gives the bounded notification wait an executable path", () => {
    const notify = flat(read("skills/notify/SKILL.md"));
    // The capability map offers this window; the skill has to say how.
    expect(notify).toContain('"event_type":"mobile_app_notification_action"');
    expect(notify).toContain("Bounded Event Collection");
    expect(notify).toContain("this skill does not restate it");
    expect(notify).toContain("notification delivery eats part of it");
    expect(notify).toContain("only when the user says they are holding the phone");
  });

  it("routes a bounded callback wait to notify, not to write", () => {
    const fb = flat(read("skills/fallback/SKILL.md"));
    // write owns the durable automation; the in-chat window is notify's.
    expect(fb).toContain("for a bounded in-chat wait instead, `notify` owns it");
  });

  it("names the Matter/Thread message shapes it can name", () => {
    const rr = flat(read("skills/fallback/relay-ready.md"));
    expect(rr).toContain('`{"type":"otbr/info"}`');
    expect(rr).toContain("takes the device, not the entity");
    // Honest about what is not pinned rather than inventing a schema.
    expect(rr).toContain("this section pins no schema");
  });

  it("names the skills that pin search/related instead of a category", () => {
    const row = read("docs/reference/ha-api-matrix.md")
      .split("\n").find((l) => l.startsWith("| Relations"));
    expect(row).not.toContain("every write-capable skill");
    expect(row).toContain("`entity-discovery`");
  });
});
