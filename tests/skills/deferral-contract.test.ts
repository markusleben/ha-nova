// tests/skills/deferral-contract.test.ts
//
// Split from indirect-actuation-contract.test.ts, which passed the repo's
// ~400-line ceiling again. This half asks WHERE a call belongs: owning-skill
// deferrals and the Supervisor lifecycle path. Expansion and tier
// classification stay in the original file.
//
// Split out of service-call-contract.test.ts: the #513 gate contract grew the
// file past the repo's ~400-line ceiling. Same subject, own file, so a future
// change to the gate does not have to be reviewed inside 550 lines of
// unrelated service-call assertions.
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (relative: string): string =>
  readFileSync(resolve(__dirname, "../../", relative), "utf-8");

const skillDoc = read("skills/service-call/SKILL.md");
// Contract docs hard-wrap at ~72 columns, so a pinned sentence must not also
// pin the column it happens to break at.
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("owning-skill deferrals and lifecycle paths (#513)", () => {
  describe("owning-skill deferrals cover every gated service family (#513)", () => {
    // A generic service call must never reach a family whose owning skill
    // carries stricter gates. Each row below exists because the owning skill
    // enforces something this flow does not: a feature bitmask, a backup
    // offer, an impact quantification, or a typed confirmation code.
    it("defers every gated family to its owning skill", () => {
      for (const [service, owner] of [
        ["mqtt.publish", "ha-nova:mqtt"],
        ["update.install", "ha-nova:updates"],
        ["camera.snapshot", "ha-nova:camera"],
        ["camera.turn_on", "ha-nova:camera"],
        ["media_player.*", "ha-nova:media"],
        ["notify.*", "ha-nova:notify"],
        ["logger.set_level", "ha-nova:diagnose"],
        ["recorder.purge", "ha-nova:maintenance"],
        ["recorder.purge_entities", "ha-nova:maintenance"],
        ["calendar.create_event", "ha-nova:calendar"],
        ["todo.add_item", "ha-nova:todo"],
        ["backup.create", "ha-nova:backup"],
        ["conversation.process", "ha-nova:assist"],
      ] as Array<[string, string]>) {
        // A row may name the service outright or cover its whole domain
        // (`camera.*`), so accept either spelling — otherwise a later PR that
        // consolidates a family silently loses its deferral.
        const domainWildcard = `\`${service.split(".")[0]}.*\``;
        const row = skillDoc
          .split("\n")
          .find(
            (line) =>
              line.startsWith("|") &&
              (line.includes(`\`${service}\``) || line.includes(domainWildcard)),
          );
        expect(row, `no deferral row for ${service}`).toBeTruthy();
        expect(row, `${service} must defer to ${owner}`).toContain(owner);
      }
    });

    it("keeps read-only response services and scene activation in this flow", () => {
      expect(skillDoc).toContain("Read-only response services stay here");
      expect(skillDoc).toContain("`calendar.get_events`");
      expect(skillDoc).toContain("`todo.get_items`");
      expect(skillDoc).toContain(
        "`ha-nova:scene` owns scene CRUD, not activation",
      );
    });

    it("keeps Supervisor lifecycle here rather than routing it somewhere weaker", () => {
      // fallback tiers Apps as External, so deferring hassio.* would send the
      // agent to a page that denies the transport works — and fallback has no
      // disruptive tier and no self-amputation rule.
      const rows = skillDoc
        .split("\n")
        .filter((line) => line.startsWith("|") && line.includes("hassio"));
      expect(rows.length).toBeGreaterThanOrEqual(2);
      expect(rows.join(" ")).toContain("disruptive tier");
      // The domain is not a wildcard: restores reboot HA and updates have an
      // owning skill, so both must route away from the generic flow.
      expect(flat(skillDoc)).toContain("The `hassio` domain is NOT a wildcard");
      expect(flat(skillDoc)).toContain("belong to `ha-nova:backup`, which refuses restores outright");
      expect(flat(skillDoc)).toContain("`addon_update` belongs to `ha-nova:updates`");
      expect(flat(skillDoc)).toContain(
        "Refuse outright any call targeting the App that runs this Relay",
      );
      // An App restart takes every device that App serves offline.
      expect(skillDoc).toContain("`hassio.addon_restart`");
      expect(skillDoc).toContain("`hassio.addon_start`");
      expect(flat(skillDoc)).toContain(
        "an MQTT or Z-Wave App takes every device it serves offline while it comes back",
      );
    });
  });
  it("gives Supervisor lifecycle calls a path the entity flow cannot provide", () => {
    const doc = flat(skillDoc);
    // An App is addressed by slug; there is no entity to read before or after.
    expect(doc).toContain("The target is an App SLUG, not an entity");
    // Probed live: the relay passes only /api/..., and HA answers its
    // /api/hassio/... proxy with 403 — so the slug comes from the update
    // entity's entity_picture, and App state is simply not readable here.
    expect(doc).toContain("The Supervisor API is NOT reachable from here");
    expect(doc).toContain("`entity_picture` is `/api/hassio/addons/<slug>/icon`");
    expect(doc).toContain("App state cannot be verified from here");
    expect(doc).toContain("Never infer success from the service call returning");
    // A host reboot takes the transport with it, so success is unobservable.
    expect(doc).toContain("Never report success: you will not be there to see it");
    expect(doc).toContain("needs physical access to come back");
  });
  it("defers clearing completed to-do items to the owning skill", () => {
    expect(flat(skillDoc)).toContain("`todo.remove_completed_items`");
  });
});
