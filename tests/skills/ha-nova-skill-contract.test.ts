import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("ha-nova skill contract", () => {
  it("uses streaming jq limit in read-only fast shortcut", () => {
    const content = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(content).toContain("limit($limit;");
    expect(content).toContain("Read-Only Fast Shortcut (Trivial Single-Unit Only)");
    expect(content).toContain("independent_units_count = 1");
    expect(content).toContain("For non-trivial reads (`independent_units_count >= 2`), this shortcut is forbidden");
    expect(content).not.toContain(
      'map(select((.entity_id|type)=="string" and (.entity_id|startswith($domain))) | .entity_id)[:$limit][]'
    );
  });

  it("uses repo-root-aware onboarding env path in entities skill", () => {
    const content = readFileSync("skills/ha-entities.md", "utf8");

    expect(content).toContain('NOVA_REPO_ROOT="${NOVA_REPO_ROOT:-${HA_NOVA_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}}"');
    expect(content).toContain('"$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" env');
    expect(content).not.toContain('eval "$(bash scripts/onboarding/macos-onboarding.sh env)"');
  });

  it("uses object-only state filters for ws get_states parsing", () => {
    const entities = readFileSync("skills/ha-entities.md", "utf8");

    expect(entities).toContain('select(type=="object" and (.entity_id|type)=="string")');
    expect(entities).toContain("Do not run schema-probing jq one-offs in normal flow");
  });

  it("routes automation writes through best-practice refresh skill", () => {
    const content = readFileSync("skills/ha-nova.md", "utf8");

    expect(content).toContain("ha-automation-best-practices");
    expect(content).toContain("for `create`/`update` use");
    expect(content).toContain("for `delete` use");
    expect(content).toContain("$NOVA_REPO_ROOT/skills/ha-control.md");
    expect(content).toContain("$NOVA_REPO_ROOT/skills/ha-safety.md");
    expect(content).toContain("for write intents");
  });

  it("defines branded user response contract sections", () => {
    const content = readFileSync("skills/ha-nova.md", "utf8");
    const installedSkill = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(content).toContain("User Response Contract (Brand Signature)");
    expect(content).toContain("`Outcome`");
    expect(content).toContain("`Current State`");
    expect(content).toContain("`Impact`");
    expect(content).toContain("`Gate`");
    expect(content).toContain("`Next`");
    expect(content).toContain("Subagent fan-out used: yes/no (reason)");
    expect(content).toContain("orchestration evidence line may be omitted");
    expect(content).toContain("preview -> confirm:<token> -> apply -> verify");
    expect(installedSkill).toContain("User Response Contract (Brand Signature)");
    expect(installedSkill).toContain("preview -> confirm:<token> -> apply -> verify");
  });

  it("enforces best-practice refresh gate before automation create/update writes", () => {
    const content = readFileSync("skills/ha-automation-crud.md", "utf8");
    const bestPractices = readFileSync("skills/ha-automation-best-practices.md", "utf8");

    expect(content).toContain("Required Companion Skill for Writes");
    expect(content).toContain("Enforce best-practice refresh snapshot gate");
    expect(content).toContain("no valid best-practice refresh snapshot -> no write");
    expect(content).toContain("do not parse top-level `.result`");
    expect(content).toContain("Keep create/update path under 6 Relay calls.");
    expect(content).toContain("Run independent reads in parallel when possible.");
    expect(content).toContain("Never accept free-text write confirmations; require `confirm:<token>`.");
    expect(content).toContain("Relay injects App-side LLAT; client-side `HA_LLAT` is not required.");
    expect(content).not.toContain("not yet exposed in relay-only MVP path");
    expect(bestPractices).toContain("Refresh Snapshot Gate (Mandatory)");
    expect(bestPractices).toContain("not older than 30 days");
    expect(bestPractices).toContain("${HOME}/.cache/ha-nova/automation-bp-snapshot.json");
    expect(bestPractices).toContain("`delete` operations are exempt from the refresh gate");
  });

  it("keeps installed codex skill routing aligned with delete exemption", () => {
    const installedSkill = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(installedSkill).toContain("for `create`/`update` use");
    expect(installedSkill).toContain("for `delete` use");
    expect(installedSkill).not.toContain("for `create`/`update`/`delete` use");
  });

  it("allows deterministic auto-select for unique exact matches", () => {
    const safety = readFileSync("skills/ha-safety.md", "utf8");

    expect(safety).toContain("If there is exactly one exact match, auto-select it and continue.");
    expect(safety).toContain("If multiple candidates remain, present candidates and ask user to pick one.");
    expect(safety).toContain("confirm:<token>");
    expect(safety).toContain("Use binary gate wording:");
  });

  it("enforces threshold-based subagent fan-out for independent parallel units", () => {
    const content = readFileSync("skills/ha-nova.md", "utf8");
    const installedSkill = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(content).toContain("Parallelism is mandatory when capability exists.");
    expect(content).toContain("Subagent fan-out is mandatory only for `>=3` substantial independent task units.");
    expect(content).toContain("For `<3` substantial units, use native parallel tool calls in the main agent.");
    expect(content).toContain("Native parallel tool calls are also the fallback when subagent capability is unavailable.");
    expect(content).not.toContain("Subagent fan-out is mandatory for 2+ independent task units.");
    expect(content).not.toContain("Use subagents whenever there are 2+ independent units.");
    expect(content).toContain("Parallel Orchestration (MVP/KISS)");
    expect(content).toContain("Subagent Dispatch Protocol (Required)");
    expect(content).toContain("Use subagents whenever there are `>=3` substantial independent units.");
    expect(content).toContain("deterministic fallback");
    expect(content).toContain("Orchestration Hard Gate (First Step, Mandatory)");
    expect(content).toContain("independent_units_count");
    expect(content).toContain("substantial_independent_units");
    expect(content).toContain("subagent_capable");
    expect(content).toContain("fan_out_required");
    expect(content).toContain("Canonical Automation DAG (Create/Update)");
    expect(content).toContain("A1");
    expect(content).toContain("A2");
    expect(content).toContain("A3");
    expect(content).toContain("one-state-snapshot");
    expect(content).toContain("Subagent fan-out used: yes/no (reason)");
    expect(installedSkill).toContain("Parallelism is mandatory when capability exists.");
    expect(installedSkill).toContain("Subagent fan-out is mandatory only for `>=3` substantial independent task units.");
    expect(installedSkill).toContain("For `<3` substantial units, use native parallel tool calls in the main agent.");
    expect(installedSkill).not.toContain("Subagent fan-out is mandatory for 2+ independent task units.");
    expect(installedSkill).not.toContain("Use subagents whenever there are 2+ independent units.");
    expect(installedSkill).toContain("Parallel Orchestration (MVP/KISS)");
    expect(installedSkill).toContain("Subagent Dispatch Protocol (Required)");
    expect(installedSkill).toContain("Orchestration Hard Gate (First Step, Mandatory)");
    expect(installedSkill).toContain("one-state-snapshot");
  });

  it("keeps orchestrator mirror files identical", () => {
    const repoSkill = readFileSync("skills/ha-nova.md", "utf8");
    const installedSkill = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(repoSkill).toBe(installedSkill);
  });

  it("keeps auth and runtime safety policy aligned across orchestrator docs", () => {
    const content = readFileSync("skills/ha-nova.md", "utf8");
    const installedSkill = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(content).toContain("Runtime Prerequisite (macOS)");
    expect(content).toContain("Relay-only auth model");
    expect(content).toContain("Do not ask user to paste tokens in chat.");
    expect(installedSkill).toContain("Runtime Prerequisite (macOS)");
    expect(installedSkill).toContain("Relay-only auth model");
    expect(installedSkill).toContain("Do not ask user to paste tokens in chat.");
  });

  it("documents shell-dependent quoting reliability rules", () => {
    const content = readFileSync("skills/ha-nova.md", "utf8");
    const installedSkill = readFileSync(".agents/skills/ha-nova/SKILL.md", "utf8");

    expect(content).toContain("Quoting is shell-dependent");
    expect(content).toContain('eval "$(bash "$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" env)"');
    expect(content).toContain('eval "$(bash \\"$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh\\" env)"');
    expect(installedSkill).toContain("Quoting is shell-dependent");
    expect(installedSkill).toContain("Windows: WSL bash or Git Bash");
  });
});
