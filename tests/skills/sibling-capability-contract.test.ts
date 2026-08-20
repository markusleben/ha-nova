// tests/skills/sibling-capability-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

// Issue #564: capability-oriented requests (restart, identify, calibrate,
// reset, siren, ...) must be answered by ALL registry entities of the same
// physical device — including registry-disabled ones — never only by the
// resolved entity's own domain or by friendly-name matching.

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

const DISCOVERY = flat(read("skills/entity-discovery/SKILL.md"));
const SERVICE_CALL = flat(read("skills/service-call/SKILL.md"));
const CAMERA = flat(read("skills/camera/SKILL.md"));

describe("device-sibling capability discovery contract (#564)", () => {
  it("owns the section in entity-discovery with the capability trigger", () => {
    expect(DISCOVERY).toContain("### Device-sibling capability discovery");
    expect(DISCOVERY).toContain(
      "A capability-oriented request (restart, identify, calibrate, reset, siren, ...)",
    );
    expect(DISCOVERY).toContain("take its `device_id`");
  });

  it("lists siblings from the FULL registry because disabled entities exist only there", () => {
    expect(DISCOVERY).toContain(
      "List ALL registry entities of that device from the FULL registry (`config/entity_registry/list`",
    );
    expect(DISCOVERY).toContain("`list_for_display` omits disabled entities");
    expect(DISCOVERY).toContain(
      "the registry row's `disabled_by` decides enabled/disabled status",
    );
  });

  it("never validates a disabled candidate through /api/states", () => {
    expect(DISCOVERY).toContain(
      "NEVER validate a disabled candidate through `/api/states`: it has no state there, and its absence proves nothing about the capability",
    );
  });

  it("selects on the SAME device, never by name match across devices", () => {
    expect(DISCOVERY).toContain(
      "Select by domain, `device_class`, `translation_key`, and entity_id semantics on the SAME device",
    );
    expect(DISCOVERY).toContain(
      "An entity on another device never qualifies merely because its name matches",
    );
  });

  it("bounds ambiguity to one clarification carrying the four status fields", () => {
    expect(DISCOVERY).toContain("Prefer an enabled exact match");
    expect(DISCOVERY).toContain(
      "ONE bounded clarification listing each candidate's entity ID, domain, device, and enabled/disabled status",
    );
  });

  it("reports disabled candidates and hands the enable flow to organize, never auto-enabling", () => {
    expect(DISCOVERY).toContain(
      "Report a disabled candidate as available-but-disabled WITH its `disabled_by` source",
    );
    expect(DISCOVERY).toContain(
      "Never enable anything automatically, and never press or execute from discovery",
    );
  });

  it("stays generic — no brand, camera, or restart-button hard-coding", () => {
    expect(DISCOVERY).toContain(
      "never hard-coded to a brand, to cameras, or to restart buttons",
    );
    expect(read("skills/entity-discovery/SKILL.md")).not.toMatch(/reolink/i);
  });

  it("is reachable from service-call target resolution and camera restart asks", () => {
    expect(SERVICE_CALL).toContain(
      "run `ha-nova:entity-discovery` → Device-sibling capability discovery before concluding the capability does not exist",
    );
    expect(CAMERA).toContain(
      "A restart/reboot ask is neither of these: find the device's own capability entity (often a disabled sibling) via `ha-nova:entity-discovery` → Device-sibling capability discovery",
    );
  });
  it("gives organize a real enable operation for the handoff", () => {
    const org = flat(read("skills/organize/SKILL.md"));
    expect(org).toContain('the write `{"disabled_by": null}` on the entity record');
    expect(org).toContain("never press or actuate the freshly enabled entity from this skill");
  });
  it("proceeds from a device-resolved target without an entity row", () => {
    expect(flat(read("skills/entity-discovery/SKILL.md"))).toContain(
      "supplies its `device_id` directly — no entity row is required to proceed",
    );
  });
  it("routes device-disabled siblings through device enablement", () => {
    expect(flat(read("skills/entity-discovery/SKILL.md"))).toContain(
      "needs its parent DEVICE re-enabled, not the entity alone",
    );
    expect(flat(read("skills/organize/SKILL.md"))).toContain(
      're-enable the DEVICE record (`{"disabled_by": null}` on the device registry entry',
    );
  });
});
