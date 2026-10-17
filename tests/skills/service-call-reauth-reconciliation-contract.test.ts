// tests/skills/service-call-reauth-reconciliation-contract.test.ts
//
// Since PR4 (`ha-nova call`), the CLI owns the reconciliation: it snapshots
// reauth flows before the one POST and re-reads them after an upstream 500.
// The skill keeps the handoff; the Go code is pinned as the rule's source.
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("service-call reauth reconciliation (#586)", () => {
  const skill = flat(read("skills/service-call/SKILL.md"));
  const apply = read("cli/verb_execute.go"); // newReauthFlows moved with executeAction
  const relayWrite = read("cli/relay_write.go");

  it("detects NEW flows against a pre-call snapshot, never pre-existing ones", () => {
    expect(relayWrite).toContain('map[string]any{"type": "config_entries/flow/progress"}');
    expect(relayWrite).toContain('if flow.Context.Source == "reauth" {');
    expect(apply).toContain("_, existed := before[id]");
    expect(apply).toContain("if !existed && matches {");
  });

  it("matches by entry, or by integration only when it has exactly one entry", () => {
    expect(apply).toContain(
      "matches := (flow.entryID != \"\" && targetEntries[flow.entryID]) || (targetPlatforms[flow.handler] && perDomain[flow.handler] == 1)",
    );
  });

  it("reports both facts on a match and stays uncertain without one", () => {
    expect(skill).toContain(
      "`reauth_flows_opened` in a 500 result: report the failure AND that Home Assistant started reauthentication; hand off to `ha-nova:integration-setup`. A 500 without it: `ha-nova:diagnose`.",
    );
  });

  it("never retries automatically", () => {
    expect(skill).toContain("Never retry after exit 6 or 7");
  });
});
