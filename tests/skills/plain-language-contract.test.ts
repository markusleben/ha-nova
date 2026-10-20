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
    expect(rules).toContain('Keep Home Assistant names verbatim: "Bed Light", never "Bettlicht" or "Bed Light (Bettlicht)".');
    expect(rules).toContain("Never show the user a command to run: you run `ha-nova` yourself");
    expect(rules).toContain("Sole exception: a step only the user can do (setup, a permission, an update without a shell).");
    expect(rules).toContain("Choices are natural questions");
    expect(rules).toContain("What was not checked is one line naming each surface");
    expect(rules).toContain("Choices are natural questions that end your turn");
    expect(rules).toContain("Say each fact once per answer.");
  });

  // F2 (0.27 regression lane, Sonnet DE): ids, attributes, mode, UTC times,
  // the Relay address, a backup file name, and HTTP/WebSocket text leaked.
  it("keeps technical details, UTC times, and failure internals out", () => {
    expect(rules).toContain("- No technical details: ids (`light.bed_light`) only for a missing or shared name; no attribute keys or modes (`supported_features: 0`, `single`), service names, your own commands, Relay or HA URLs, snapshot file names, or protocol errors (HTTP 502) — say what they mean. Diff rows stay verbatim.");
    expect(rules).toContain('- Times: relative or local (the user\'s or HA\'s zone), never UTC.');
    expect(rules).toContain("- A failure: what did not work and why, plainly; whether anything changed (or unknown); next step or the skill's choices; details only when asked.");
    // The licenses the leaks cited are gone.
    expect(rules).not.toContain("only for safety or evidence");
    expect(rules).not.toContain("identifiers literal when they are evidence");
    expect(flat(readFileSync("skills/ha-nova/write-safety.md", "utf8"))).not.toContain("name/alias and `entity_id`");
    expect(readFileSync("skills/ha-nova/output-cards.md", "utf8")).not.toContain("name/mode/target");
    expect(flat(readFileSync("skills/write/SKILL.md", "utf8"))).toContain("mode only if not `single`, in plain words; ids and the full config only when asked.");
    expect(flat(readFileSync("skills/home-map/SKILL.md", "utf8"))).toContain(
      "Leave out the `entities`, `entities_disabled`, `domains`, and `integrations` numbers (no row, no aside) unless asked.",
    );
  });

  // F2 follow-up (2026-10-20 Windows run, Sonnet): 10 of 13 answers leaked.
  // No transcript read output-rules.md (an "Apply" pointer at the end of each
  // skill), and a test-bed replay read not even the bootstrap. Only the skill
  // body the Skill tool loads is sure to be in context, so the core rides on
  // each skill's pointer line.
  it("carries the core rule in every skill's own output pointer", () => {
    const core = 'Apply `../ha-nova/output-rules.md`: plain words, HA names verbatim; unless asked or names clash, no ids, "entity", modes, Relay/HA URLs, snapshots, raw JSON.';
    for (const file of skillFiles.filter((f) => /^skills\/[^/]+\/SKILL\.md$/.test(f))) {
      if (file === "skills/health/SKILL.md") continue;
      const text = flat(readFileSync(file, "utf8"));
      expect(text, file).toContain(file === "skills/ha-nova/SKILL.md" ? core.slice(core.indexOf(": ") + 2, -1) : core);
    }
    // health keeps literal entity ids (privacy-mode rows) and names its report modes.
    expect(flat(readFileSync("skills/health/SKILL.md", "utf8"))).toContain('plain words, HA names verbatim; unless asked, no "entity", Relay/HA URLs, snapshots, raw JSON.');
    // Card and restore templates model no ids or modes.
    const cards = readFileSync("skills/ha-nova/output-cards.md", "utf8");
    expect(cards).not.toContain("entity IDs, state values");
    expect(cards).not.toContain("| Mode |");
    expect(flat(readFileSync("skills/write/SKILL.md", "utf8"))).toContain("A create summary (or a deleted item's restore) names the item");
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
