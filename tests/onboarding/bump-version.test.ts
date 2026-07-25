/**
 * S-11: Version bump script contract
 */
import { spawnSync } from "node:child_process";
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
    expect(content).toContain("nova/version.json");
    expect(content).toContain(".metadata.version = $v");
    expect(content).toContain('.packages[""].version = $v');
  });

  it.each(["01.2.3", "1.02.3", "1.2.03"])(
    "rejects non-canonical bump version %s",
    (version) => {
      const result = spawnSync(
        "bash",
        ["scripts/bump-version.sh", version],
        { encoding: "utf8" },
      );
      expect(result.status).not.toBe(0);
    },
  );

  it("version.json contains required fields", () => {
    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    expect(versionJson).toHaveProperty("skill_version");
    expect(versionJson).toHaveProperty("min_relay_version");
    const canonical = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
    expect(versionJson.skill_version).toMatch(canonical);
    expect(versionJson.min_relay_version).toMatch(canonical);
  });

  it("package.json version matches version.json skill_version", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8"));
    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    expect(pkg.version).toBe(versionJson.skill_version);
  });

  it("App runtime version metadata mirrors version.json", () => {
    const appVersion = JSON.parse(readFileSync("nova/version.json", "utf8"));
    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    expect(appVersion).toEqual(versionJson);
  });

  it("package-lock.json version matches version.json skill_version", () => {
    const pkgLock = JSON.parse(readFileSync("package-lock.json", "utf8"));
    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    expect(pkgLock.version).toBe(versionJson.skill_version);
    expect(pkgLock.packages[""].version).toBe(versionJson.skill_version);
  });

  it("exposes npm bump shortcut", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8"));
    expect(pkg.scripts?.bump).toContain("bump-version.sh");
  });

  it("exposes a next-release version guard", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8"));
    const content = readFileSync("scripts/release/verify-next-release-version.sh", "utf8");
    expect(pkg.scripts?.["verify:next-release-version"]).toContain("verify-next-release-version.sh");
    expect(content.startsWith("#!/")).toBe(true);
    expect(content).toContain("gh");
    expect(content).toContain("--paginate");
    // --slurp is deliberately absent: it is incompatible with --jq, and the
    // per-page --jq projection is what keeps the payload under spawnSync
    // buffers (the v0.14.0 ENOBUFS publish failure).
    expect(content).not.toContain("--slurp");
    expect(content).toContain("--jq");
    expect(content).toContain("latest published stable");
    expect(content).toContain("already exists on GitHub releases");
    expect(content).toContain("HA_NOVA_ALLOW_EXISTING_RELEASE_TAG");
    expect(content).toContain("rerun allowed for existing");
  });
});
