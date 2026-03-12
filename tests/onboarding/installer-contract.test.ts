/**
 * Installer contract test — validates install.sh structure and safety.
 */
import { constants, readFileSync, statSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("install.sh contract", () => {
  const content = readFileSync("install.sh", "utf8");

  it("is executable with proper shebang", () => {
    const stats = statSync("install.sh");
    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
  });

  it("uses set -euo pipefail", () => {
    expect(content).toContain("set -euo pipefail");
  });

  it("checks prerequisites: macOS, Node >= 20, npm, git", () => {
    expect(content).toContain("Darwin");
    expect(content).toContain("node --version");
    expect(content).toContain("nodejs.org");
    expect(content).toContain("npm");
    expect(content).toContain("xcode-select --install");
  });

  it("clones to ~/.local/share/ha-nova with --depth 1", () => {
    expect(content).toContain("git clone --depth 1");
    expect(content).toContain(".local/share/ha-nova");
  });

  it("links CLI to ~/.local/bin via symlink", () => {
    expect(content).toContain("ln -sfn");
    expect(content).toContain("BIN_LINK");
    expect(content).toContain("BIN_DIR");
  });

  it("makes ha-nova available from any terminal after install", () => {
    expect(content).toContain("ensure_bin_dir_on_path()");
    expect(content).toContain("detect_shell_rc()");
    expect(content).toContain("export PATH=\"${BIN_DIR}:${PATH}\"");
    expect(content).toContain('export PATH="$HOME/.local/bin:$PATH"');
    expect(content).toContain(".zshrc");
    expect(content).toContain(".bash_profile");
    expect(content).toContain(".profile");
  });

  it("handles existing installation (update/reinstall/cancel)", () => {
    expect(content).toContain("Existing HA NOVA installation found");
    expect(content).toContain("Update");
    expect(content).toContain("Reinstall");
    expect(content).toContain("Cancel");
    expect(content).toContain("pull --ff-only");
  });

  it("does not use sudo or eval", () => {
    expect(content).not.toContain("sudo ");
    expect(content).not.toMatch(/\beval\b/);
  });

  it("uses HTTPS only for clone", () => {
    expect(content).toContain("https://github.com/markusleben/ha-nova.git");
    expect(content).not.toContain("git@github.com");
  });

  it("runs npm install with --no-audit --no-fund", () => {
    expect(content).toContain("npm install --no-audit --no-fund");
  });

  it("hands off to ha-nova setup at the end", () => {
    expect(content).toContain('"${BIN_LINK}" setup < /dev/tty');
    expect(content).toContain("Need help later? Run: ha-nova doctor");
  });

  it("uses /dev/tty only for prompts, not by replacing installer stdin globally", () => {
    expect(content).toContain("has_interactive_tty()");
    expect(content).toContain("require_interactive_tty()");
    expect(content).toContain("if has_interactive_tty; then");
    expect(content).toContain('if : </dev/tty 2>/dev/null; then');
    expect(content).toContain('read -r choice < /dev/tty');
    expect(content).not.toContain("exec < /dev/tty");
    expect(content).not.toContain('-r /dev/tty && -w /dev/tty');
    expect(content).not.toContain('exec 3</dev/tty 2>/dev/null');
  });

  it("provides clear error messages for missing prerequisites", () => {
    // Node.js
    expect(content).toContain("Node.js not found");
    expect(content).toContain("Download the LTS version");
    // git
    expect(content).toContain("git not found");
    expect(content).toContain("xcode-select --install");
  });
});
