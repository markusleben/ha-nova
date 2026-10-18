import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// 0.27 plain-language pass (2026-10-18 Sonnet run): the audience is not
// technical. One shared block in output-rules.md carries the rules every
// route loads; skills must not contradict it with their own examples.
const flat = (s: string) => s.replace(/\s+/g, " ");
const rules = flat(readFileSync("skills/ha-nova/output-rules.md", "utf8"));
const skillFiles = execFileSync("git", ["ls-files", "skills/**/*.md"], { encoding: "utf8" }).trim().split("\n");

describe("plain-language output rules", () => {
  it("keeps the shared block in the file every route loads", () => {
    expect(rules).toContain("## Plain Language");
    expect(rules).toContain('Keep Home Assistant names verbatim, never translated ("Bed Light", not "Bettlicht").');
    expect(rules).toContain("Never show the user a command to run: you run `ha-nova` yourself");
    expect(rules).toContain("Sole exception: setup or permission steps only the user can do.");
    expect(rules).toContain("Choices are natural questions");
    expect(rules).toContain("What was not checked is one line naming each surface");
    expect(rules).toContain("Choices are natural questions that end your turn");
    expect(rules).toContain("Say each fact once per answer.");
  });

  it("never hands the user a command except the setup step", () => {
    // "ask user to run: `x`", "have the user run `x`", "the user runs `x`".
    const userRuns = /\b(?:user|them)(?: to)? runs?:? `([^`]+)`/g;
    for (const file of skillFiles) {
      for (const [, command] of flat(readFileSync(file, "utf8")).matchAll(userRuns)) {
        expect(command, file).toBe("ha-nova setup");
      }
    }
  });

  it("shows no option tokens in previews", () => {
    for (const file of skillFiles.filter((f) => f !== "skills/ha-nova/output-rules.md")) {
      const text = readFileSync(file, "utf8");
      expect(text, file).not.toContain("show yaml");
      expect(text, file).not.toMatch(/apply · |· cancel/);
    }
    expect(readFileSync("cli/confirm_say.go", "utf8")).toContain('"Save it? I can also show the details."');
  });
});
