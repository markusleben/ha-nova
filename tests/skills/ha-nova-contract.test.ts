import { existsSync, readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import { expectedIntentMatrix } from "./helpers/expected-intent-matrix.js";
import { parseIntentMatrix } from "./helpers/intent-matrix.js";

describe("ha-nova contract", () => {
  it("uses streaming jq limit in read-only fast shortcut", () => {
    const content = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(content).toContain("limit($limit;");
    expect(content).toContain("Read-Only Fast Shortcut (Trivial Single-Unit Only)");
    expect(content).toContain("independent_units_count = 1");
    expect(content).toContain("For non-trivial reads (`independent_units_count >= 2`), this shortcut is forbidden");
    expect(content).toContain("return compact domain summary only (result + next step)");
    expect(content).not.toContain(
      'map(select((.entity_id|type)=="string" and (.entity_id|startswith($domain))) | .entity_id)[:$limit][]'
    );
  });

  it("routes intents through canonical intent matrix + discovery protocol", () => {
    const content = readFileSync("skills/ha-nova.md", "utf8");
    const discovery = readFileSync("skills/ha-nova/core/discovery-map.md", "utf8");

    expect(content).toContain("core/intents.md");
    expect(content).toContain("(canonical)");
    expect(content).toContain("required_companions[]");
    expect(content).toContain("modules[]");
    expect(content).toContain("create|update|delete|read|list");
    expect(discovery).toContain("This file is module-loading guidance only");
    expect(discovery).toContain('"$NOVA_REPO_ROOT/skills/ha-nova/core/intents.md"');
    expect(discovery).toContain("Load only `required_companions[]` + `modules[]` from `core/intents.md`.");
  });

  it("keeps router as orchestration layer and contracts in core", () => {
    const content = readFileSync("skills/ha-nova.md", "utf8");
    const contracts = readFileSync("skills/ha-nova/core/contracts.md", "utf8");

    expect(content).toContain("Normative contract is defined only in:");
    expect(content).toContain('"$NOVA_REPO_ROOT/skills/ha-nova/core/contracts.md"');
    expect(content).not.toContain("Automation fields:");
    expect(content).not.toContain("Script fields:");
    expect(content).not.toContain("Failure/debug format:");
    expect(contracts).toContain("## Response Contract (Domain-First)");
    expect(contracts).toContain("## Safety Contract");
    expect(contracts).toContain("## Verification Contract");
  });

  it("hardens confirmation token contract (ttl + replay + binding)", () => {
    const contracts = readFileSync("skills/ha-nova/core/contracts.md", "utf8");

    expect(contracts).toContain("## Confirmation Token Contract");
    expect(contracts).toContain("Token TTL: default 10 minutes");
    expect(contracts).toContain("one-time-use only; replay must hard-fail");
    expect(contracts).toContain("write method/path/target");
    expect(contracts).toContain("preview digest");
    expect(contracts).toContain("On stale/replay/mismatch token:");
    expect(contracts).toContain("hard-fail write");
    expect(contracts).toContain("regenerate preview");
  });

  it("enforces threshold-based subagent fan-out policy", () => {
    const content = readFileSync("skills/ha-nova.md", "utf8");
    const installedSkill = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(content).toContain("Parallelism is mandatory when capability exists.");
    expect(content).toContain("Subagent fan-out is mandatory only for `>=3` substantial independent task units.");
    expect(content).toContain("For `<3` substantial units, use native parallel tool calls in the main agent.");
    expect(content).not.toContain("Subagent fan-out is mandatory for 2+ independent task units.");
    expect(installedSkill).toContain("Subagent fan-out is mandatory only for `>=3` substantial independent task units.");
  });

  it("keeps auth and shell safety policy in router", () => {
    const content = readFileSync("skills/ha-nova.md", "utf8");

    expect(content).toContain("Runtime Prerequisite (macOS)");
    expect(content).toContain("Relay-only auth model");
    expect(content).toContain("Do not ask user to paste tokens in chat.");
    expect(content).toContain("Quoting is shell-dependent");
    expect(content).toContain("Windows: WSL bash or Git Bash");
  });

  it("keeps modular core/module files present", () => {
    const files = [
      "skills/ha-nova/core/blocks.md",
      "skills/ha-nova/core/contracts.md",
      "skills/ha-nova/core/intents.md",
      "skills/ha-nova/core/discovery-map.md",
      "skills/ha-nova/modules/automation/resolve.md",
      "skills/ha-nova/modules/automation/create-update.md",
      "skills/ha-nova/modules/automation/delete.md",
      "skills/ha-nova/modules/automation/read.md",
      "skills/ha-nova/modules/script/resolve.md",
      "skills/ha-nova/modules/script/create-update.md",
      "skills/ha-nova/modules/script/delete.md",
      "skills/ha-nova/modules/script/read.md",
    ];

    for (const file of files) {
      expect(existsSync(file), `Expected file to exist: ${file}`).toBe(true);
    }
  });

  it("validates canonical intent matrix with exact semantic sets", () => {
    const matrix = parseIntentMatrix();

    expect([...matrix.keys()].sort()).toEqual([...expectedIntentMatrix.keys()].sort());

    for (const [intent, expectedValue] of expectedIntentMatrix.entries()) {
      const actual = matrix.get(intent);
      expect(actual, `Missing intent in matrix: ${intent}`).toBeDefined();
      expect(actual?.companions).toEqual(expectedValue.companions);
      expect(actual?.modules).toEqual(expectedValue.modules);
    }
  });

  it("keeps B4 scope aligned to automation and excludes it from script fast-pass", () => {
    const blocks = readFileSync("skills/ha-nova/core/blocks.md", "utf8");
    const scriptCreateUpdate = readFileSync("skills/ha-nova/modules/script/create-update.md", "utf8");

    expect(blocks).toContain("For automation `create`/`update`");
    expect(scriptCreateUpdate).not.toContain("B4_BP_GATE");
  });
});
