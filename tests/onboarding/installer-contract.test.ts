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

  it("warns about PATH if ~/.local/bin not in PATH", () => {
    expect(content).toContain("not in your PATH");
    expect(content).toContain(".zshrc");
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
    expect(content).toContain('exec "${BIN_LINK}" setup');
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
