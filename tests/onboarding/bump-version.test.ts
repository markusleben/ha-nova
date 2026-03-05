/**
 * S-11: Version bump script contract
 */
import { readFileSync, statSync, constants } from "node:fs";
import { describe, expect, it } from "vitest";

describe("S-11: version bump", () => {
  it("provides executable bump script", () => {
    const file = "scripts/bump-version.sh";
    const stats = statSync(file);
    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);

    const content = readFileSync(file, "utf8");
    expect(content.startsWith("#!/")).toBe(true);
  });

  it("bump script updates version.json", () => {
    const content = readFileSync("scripts/bump-version.sh", "utf8");
    expect(content).toContain("version.json");
  });

  it("version.json contains required fields", () => {
    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    expect(versionJson).toHaveProperty("skill_version");
    expect(versionJson).toHaveProperty("min_relay_version");
    expect(versionJson.skill_version).toMatch(/^\d+\.\d+\.\d+$/);
    expect(versionJson.min_relay_version).toMatch(/^\d+\.\d+\.\d+$/);
  });

  it("package.json version matches version.json skill_version", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8"));
    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    expect(pkg.version).toBe(versionJson.skill_version);
  });

  it("exposes npm bump shortcut", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8"));
    expect(pkg.scripts?.bump).toContain("bump-version.sh");
  });
});
