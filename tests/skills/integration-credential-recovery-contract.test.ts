// tests/skills/integration-credential-recovery-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("integration-setup credential recovery (#585)", () => {
  const skill = read("skills/integration-setup/SKILL.md");
  const section = skill.match(
    /### Credential Recovery \(no reauth pending\)[\s\S]*?(?=\n## )/,
  )?.[0];

  it("advertises recovery in every routing surface", () => {
    // Discovery selects skills by description and dispatch row; a lane only
    // reachable after selection is a lane discovery never routes to.
    expect(read("skills/integration-setup/SKILL.md")).toContain(
      "or recovering invalid integration credentials when no reauth flow is pending",
    );
    const dispatch = read("skills/ha-nova/SKILL.md");
    expect(dispatch).toContain(
      "or recover invalid integration credentials when no reauth flow is pending",
    );
  });

  it("routes the no-pending-flow case into the recovery lane", () => {
    expect(section).toBeDefined();
    expect(flat(skill)).toContain(
      "if no matching pending flow exists: with credentials reported invalid, continue with Credential Recovery below; otherwise report that Home Assistant is not currently requesting reauthentication; never synthesize a reauth flow",
    );
  });

  it("treats loaded as lifecycle state, not credential validity", () => {
    const s = flat(section ?? "");
    expect(s).toContain("`loaded` is lifecycle state, never proof the stored credential works");
  });

  it("uses only the documented reload surface and preserves the entry", () => {
    const s = flat(section ?? "");
    expect(s).toContain("POST /api/config/config_entries/entry/<entry_id>/reload");
    expect(s).toContain("Preserve the entry and every subentry; never delete or recreate anything");
    expect(s).toContain('A new flow with `context.source == "reauth"` and the same `entry_id`');
  });

  it("fails closed on unsupported upstream triggers", () => {
    const s = flat(section ?? "");
    expect(s).toContain("Still no flow on a settled, SUCCESSFUL re-read");
    // A 200-with-false reload or a non-loaded entry is its own outcome, not
    // evidence that no upstream trigger exists.
    expect(s).toContain("a `false` result or a non-`loaded` entry means the reload itself did not complete");
    // A transient read error after the reload must never be reported as an
    // upstream limitation.
    expect(s).toContain("A FAILED re-read is not that evidence");
    expect(s).toContain(
      "Never synthesize a config flow, edit `.storage`, create a replacement entry, or reach for deprecated integration services",
    );
  });

  it("requires positive evidence for success and avoids paid probes", () => {
    const s = flat(section ?? "");
    expect(s).toContain(
      "Verified success is only a terminal `reauth_successful` for the same `entry_id`",
    );
    // A canceled UI flow disappears exactly like a successful one — flow
    // disappearance alone must never be reported as proven recovery.
    expect(s).toContain("reported as completed but unverified, never as proven recovery");
    expect(s).toContain('Do not spend a paid API request to "test" the credential unless the user asks');
    expect(s).toContain("Secrets and key fragments never appear in previews, output, or logs");
  });
});
