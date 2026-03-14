import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("install.sh contract", () => {
  const content = readFileSync("install.sh", "utf8");

  it("is executable with proper shebang", () => {
    const stats = statSync("install.sh");
    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
  });

  it("uses a thin Unix bootstrap with macOS and Linux support", () => {
    expect(content).toContain("set -euo pipefail");
    expect(content).toContain('Darwin) printf \'%s\\n\' "macos"');
    expect(content).toContain('Linux) printf \'%s\\n\' "linux"');
    expect(content).toContain("curl");
    expect(content).toContain("tar");
    expect(content).not.toContain("git clone");
    expect(content).not.toContain("npm install");
  });

  it("resolves the version from GitHub Releases latest unless HA_NOVA_VERSION is pinned", () => {
    expect(content).toContain("https://api.github.com/repos/markusleben/ha-nova/releases/latest");
    expect(content).toContain("HA_NOVA_VERSION");
    expect(content).toContain("tag_name");
    expect(content).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/version.json");
  });

  it("downloads a platform bundle and validates bundle.json before install", () => {
    expect(content).toContain("ha-nova-macos");
    expect(content).toContain("ha-nova-linux");
    expect(content).toContain("bundle.json");
    expect(content).toContain(".sha256");
    expect(content).toContain("Downloaded bundle is missing the ha-nova binary.");
    expect(content).toContain(".local/share/ha-nova");
  });

  it("detects legacy installs and prints the dedicated cleanup one-liner", () => {
    expect(content).toContain("legacy-uninstall.sh");
    expect(content).toContain("raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh");
    expect(content).toContain("onboarding.env");
    expect(content).toContain("version-check");
  });

  it("installs ha-nova into ~/.local/bin as a single public command and manages PATH", () => {
    expect(content).toContain("BIN_DIR");
    expect(content).toContain("BIN_LINK");
    expect(content).toContain("install_binary");
    expect(content).toContain("ensure_bin_dir_on_path()");
    expect(content).toContain("write_state()");
    expect(content).toContain('"path_managed"');
    expect(content).toContain('export PATH="$HOME/.local/bin:$PATH"');
    expect(content).not.toContain('cp "${runtime_bin}" "${BIN_DIR}/ha-nova"');
  });

  it("starts ha-nova setup only when interactive and respects HA_NOVA_NO_SETUP", () => {
    expect(content).toContain("has_interactive_tty()");
    expect(content).toContain("HA_NOVA_NO_SETUP");
    expect(content).toContain('run_setup "${BIN_LINK}"');
    expect(content).toContain('echo "  Next step: ha-nova setup"');
    expect(content).toContain('echo "  Need help later? Run: ha-nova doctor"');
  });

  it("does not use sudo or eval", () => {
    expect(content).not.toContain("sudo ");
    expect(content).not.toMatch(/\beval\b/);
  });
});
