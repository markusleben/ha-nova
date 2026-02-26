import { constants, mkdtempSync, mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

describe("macOS onboarding script contract", () => {
  it("provides executable onboarding script", () => {
    const file = "scripts/onboarding/macos-onboarding.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
  });

  it("provides executable local skill installer script", () => {
    const multiInstaller = "scripts/onboarding/install-local-skills.sh";
    const multiInstallerStats = statSync(multiInstaller);
    const multiInstallerContent = readFileSync(multiInstaller, "utf8");

    expect((multiInstallerStats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(multiInstallerContent).toContain('SKILL_NAME="ha-nova"');
    expect(multiInstallerContent).toContain('SOURCE_SKILL_FILE="${SOURCE_SKILL_DIR}/SKILL.md"');
    expect(multiInstallerContent).toContain('codex) user_skills_dir="${HOME}/.agents/skills"');
    expect(multiInstallerContent).toContain('claude) user_skills_dir="${HOME}/.claude/skills"');
    expect(multiInstallerContent).toContain('opencode) user_skills_dir="${HOME}/.config/opencode/skills"');
    expect(multiInstallerContent).toContain("render_skill_file");
    expect(multiInstallerContent).toContain("ha-nova-managed-install repo_root");
  });

  it("provides executable split onboarding command scripts", () => {
    const scripts = [
      "scripts/onboarding/macos-lib.sh",
      "scripts/onboarding/macos-setup.sh",
      "scripts/onboarding/macos-doctor.sh",
      "scripts/onboarding/macos-env.sh"
    ];

    for (const file of scripts) {
      const stats = statSync(file);
      const content = readFileSync(file, "utf8");
      expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
      expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
    }
  });

  it("uses Keychain as primary secret storage", () => {
    const content = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");

    expect(content).toContain("security add-generic-password");
    expect(content).toContain("security find-generic-password");
    expect(content).toContain("ha-nova.relay-auth-token");
    expect(content).toContain("ha-nova.ha-llat");
  });

  it("supports setup, doctor, and env commands only", () => {
    const content = readFileSync("scripts/onboarding/macos-onboarding.sh", "utf8");

    expect(content).toContain(" setup");
    expect(content).toContain(" doctor");
    expect(content).toContain(" env");
    expect(content).not.toContain("start");
    expect(content).not.toContain("codex");
    expect(content).not.toContain("claude");
  });

  it("contains no contributor bootstrap or SSH flow", () => {
    const content = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");

    expect(content).not.toContain('"bootstrap"');
    expect(content).not.toContain("HA_SSH_KEY");
    expect(content).not.toContain("dev:app:bootstrap");
    expect(content).toContain("homeassistant.local");
  });

  it("auto-detects and validates Home Assistant host during setup", () => {
    const content = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");

    expect(content).toContain("probe_home_assistant_host");
    expect(content).toContain("/api/discovery_info");
    expect(content).toContain("Could not validate Home Assistant host");
    expect(content).toContain("Cannot continue without a valid Home Assistant host");
    expect(content).toContain("Retry host entry");
    expect(content).toContain("Continue with unverified host");
    expect(content).toContain("probe_relay_health");
    expect(content).toContain("/health");
    expect(content).toContain("Install/start NOVA Relay App in Home Assistant");
  });

  it("reuses existing relay token and surfaces degraded WS diagnostics", () => {
    const content = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");

    expect(content).toContain("leave empty to keep existing or auto-generate");
    expect(content).toContain("Using existing relay auth token from Keychain");
    expect(content).toContain("ha_ws_connected=false");
    expect(content).toContain("optional HA_LLAT can unlock full-scope WS features");
  });

  it("fails fast on non-interactive setup input", () => {
    const result = spawnSync("bash", ["scripts/onboarding/macos-onboarding.sh", "setup"], {
      cwd: process.cwd(),
      input: "n\n",
      encoding: "utf8",
      timeout: 15000
    });

    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("Interactive input required. Re-run in a terminal.");
  });

  it("preserves HA URL port when continuing with unverified host", () => {
    if (process.platform !== "darwin") {
      return;
    }

    const workDir = mkdtempSync(join(tmpdir(), "ha-nova-onboarding-"));
    const binDir = join(workDir, "bin");
    mkdirSync(binDir);

    const securityPath = join(binDir, "security");
    writeFileSync(
      securityPath,
      "#!/usr/bin/env bash\nexit 0\n",
      { encoding: "utf8", mode: 0o755 }
    );

    const curlPath = join(binDir, "curl");
    writeFileSync(
      curlPath,
      "#!/usr/bin/env bash\nexit 1\n",
      { encoding: "utf8", mode: 0o755 }
    );

    const opensslPath = join(binDir, "openssl");
    writeFileSync(
      opensslPath,
      "#!/usr/bin/env bash\necho deadbeefdeadbeefdeadbeefdeadbeef\n",
      { encoding: "utf8", mode: 0o755 }
    );

    const input = [
      "192.168.1.5:18123",
      "n",
      "y",
      "",
      "",
      "y",
      "",
      "",
      ""
    ].join("\n");

    const result = spawnSync("bash", ["scripts/onboarding/macos-onboarding.sh", "setup"], {
      cwd: process.cwd(),
      input,
      encoding: "utf8",
      timeout: 20000,
      env: {
        ...process.env,
        HOME: workDir,
        PATH: `${binDir}:${process.env.PATH ?? ""}`
      }
    });

    expect(result.status).toBe(0);

    const config = readFileSync(join(workDir, ".config/ha-nova/onboarding.env"), "utf8");
    expect(config).toContain("HA_HOST=192.168.1.5");
    expect(config).toContain("HA_URL=http://192.168.1.5:18123");
  });

  it("installs managed local skill files for codex, claude, and opencode", () => {
    const workDir = mkdtempSync(join(tmpdir(), "ha-nova-skill-install-"));
    const result = spawnSync("bash", ["scripts/onboarding/install-local-skills.sh", "all"], {
      cwd: process.cwd(),
      encoding: "utf8",
      timeout: 20000,
      env: {
        ...process.env,
        HOME: workDir
      }
    });

    expect(result.status).toBe(0);

    const expectedPaths = [
      join(workDir, ".agents/skills/ha-nova/SKILL.md"),
      join(workDir, ".claude/skills/ha-nova/SKILL.md"),
      join(workDir, ".config/opencode/skills/ha-nova/SKILL.md")
    ];

    for (const skillFile of expectedPaths) {
      const content = readFileSync(skillFile, "utf8");
      expect(content).toContain("name: ha-nova");
      expect(content).toContain("ha-nova-managed-install repo_root:");
      expect(content).toContain(process.cwd());
      expect(content).not.toContain("__HA_NOVA_REPO_ROOT__");
    }
  });

  it("exposes npm shortcuts", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };

    expect(pkg.scripts?.["onboarding:macos"]).toBe(
      "bash scripts/onboarding/macos-onboarding.sh setup"
    );
    expect(pkg.scripts?.["install:codex-skill"]).toBe(
      "bash scripts/onboarding/install-local-skills.sh codex"
    );
    expect(pkg.scripts?.["install:claude-skill"]).toBe(
      "bash scripts/onboarding/install-local-skills.sh claude"
    );
    expect(pkg.scripts?.["install:opencode-skill"]).toBe(
      "bash scripts/onboarding/install-local-skills.sh opencode"
    );
    expect(pkg.scripts?.["install:skills"]).toBe(
      "bash scripts/onboarding/install-local-skills.sh all"
    );
  });

  it("documents canonical Codex one-link install entrypoint", () => {
    const install = readFileSync(".codex/INSTALL.md", "utf8");
    const onboardingAlias = readFileSync(".codex/ONBOARDING.md", "utf8");
    const userDoc = readFileSync("docs/user-onboarding-macos.md", "utf8");
    const codexSkill = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(install).toContain("First-Time Setup (No Local Repo Yet)");
    expect(install).toContain("git clone https://github.com/markusleben/ha-nova.git");
    expect(install).toContain("npm install");
    expect(install).toContain("npm run install:codex-skill");
    expect(install).toContain("npm run install:skills");
    expect(install).toContain("No special launcher is required");
    expect(install).toContain("npm run onboarding:macos");
    expect(install).not.toContain("npm run onboarding:macos:start");
    expect(install).toContain("bash scripts/onboarding/macos-onboarding.sh doctor");
    expect(userDoc).toContain(
      "https://raw.githubusercontent.com/markusleben/ha-nova/main/.codex/INSTALL.md"
    );
    expect(userDoc).toContain("npm run install:skills");
    expect(userDoc).toContain("No special launcher required");
    expect(onboardingAlias).toContain("/.codex/INSTALL.md");
    expect(codexSkill).toContain("name: ha-nova");
    expect(codexSkill).toContain("__HA_NOVA_REPO_ROOT__");
    expect(codexSkill).toContain("Do not ask user to paste tokens in chat.");
  });
});
