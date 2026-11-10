/**
 * S-11: Version bump script contract
 */
import { spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, constants } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
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

  it("server.json version matches version.json skill_version", () => {
    const server = JSON.parse(readFileSync("server.json", "utf8"));
    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    expect(readFileSync("scripts/bump-version.sh", "utf8")).toContain('"$REPO_ROOT/server.json"');
    expect(server.version).toBe(versionJson.skill_version);
    expect(server.name).toBe("io.github.markusleben/ha-nova");
  });

  it("--relay bumps the App and the App version the stable notes name together", () => {
    const repo = mkdtempSync(join(tmpdir(), "ha-nova-bump-relay-"));
    try {
      for (const file of ["scripts/bump-version.sh", ".goreleaser.yml", "nova/config.yaml"]) {
        mkdirSync(join(repo, file, ".."), { recursive: true });
        copyFileSync(file, join(repo, file));
      }
      const result = spawnSync("bash", [join(repo, "scripts/bump-version.sh"), "--relay", "9.8.7"], { encoding: "utf8" });
      expect(result.status, result.stdout + result.stderr).toBe(0);
      expect(readFileSync(join(repo, "nova/config.yaml"), "utf8")).toMatch(/^version: "9\.8\.7"$/m);
      const notes = readFileSync(join(repo, ".goreleaser.yml"), "utf8");
      expect(notes).toContain("{{ else }}**Update the NOVA App to 9.8.7.**");
      expect(notes.replace("9.8.7", "")).toBe(readFileSync(".goreleaser.yml", "utf8").replace(/(Update the NOVA App to )\d+\.\d+\.\d+/, "$1"));
    } finally {
      rmSync(repo, { recursive: true, force: true });
    }
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
