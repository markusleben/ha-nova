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

  it("routes the no-pending-flow case into the recovery lane", () => {
    expect(section).toBeDefined();
    expect(flat(skill)).toContain(
      "if no matching pending flow exists, continue with Credential Recovery below; never synthesize a reauth flow",
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
    expect(s).toContain("Home Assistant exposes no supported trigger");
    expect(s).toContain(
      "Never synthesize a config flow, edit `.storage`, create a replacement entry, or reach for deprecated integration services",
    );
  });

  it("binds success to the reauth verify rule and avoids paid probes", () => {
    const s = flat(section ?? "");
    expect(s).toContain("terminal `reauth_successful` for the same `entry_id`");
    expect(s).toContain('Do not spend a paid API request to "test" the credential unless the user asks');
    expect(s).toContain("Secrets and key fragments never appear in previews, output, or logs");
  });
});
