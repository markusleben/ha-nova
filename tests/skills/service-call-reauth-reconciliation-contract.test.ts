// tests/skills/service-call-reauth-reconciliation-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("service-call reauth reconciliation (#586)", () => {
  const skill = read("skills/service-call/SKILL.md");
  const section = skill.match(
    /### Generic 500 with a reauth side effect[\s\S]*?(?=\n## )/,
  )?.[0];

  it("carries the reconciliation section and wires it into execute", () => {
    expect(section).toBeDefined();
    expect(flat(skill)).toContain(
      "snapshot pending reauth flows first (Error Handling → Generic 500 with a reauth side effect)",
    );
  });

  it("detects NEW flows against a pre-call snapshot, never pre-existing ones", () => {
    const s = flat(section ?? "");
    expect(s).toContain('{"type":"config_entries/flow/progress"}');
    expect(s).toContain('`context.source == "reauth"`');
    expect(s).toContain("A flow counts as NEW only when it is absent from the snapshot");
    expect(s).toContain("a pre-existing flow is never reported as this call's side effect");
  });

  it("matches by domain and entry, treats logs as corroboration only", () => {
    const s = flat(section ?? "");
    expect(s).toContain("its `handler` equals the target's integration domain");
    expect(s).toContain("the flow's `context.entry_id` equals it");
    expect(s).toContain("corroboration, never a match by itself");
    expect(s).toContain("A flow for another domain or entry does not match");
  });

  it("reports both facts on a match and stays uncertain without one", () => {
    const s = flat(section ?? "");
    expect(s).toContain(
      "the call failed AND Home Assistant started reauthentication",
    );
    expect(s).toContain("hand the reauth to `ha-nova:integration-setup`");
    expect(s).toContain("hand off to `ha-nova:diagnose` instead of guessing");
  });

  it("never retries automatically and never surfaces secrets", () => {
    const s = flat(section ?? "");
    expect(s).toContain("Never retry the service call automatically after a 500");
    expect(s).toContain(
      "never surface credentials or key fragments from flows or logs",
    );
  });
});
