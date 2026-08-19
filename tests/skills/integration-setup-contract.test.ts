import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";


// The -- RELAY-READY sections live in fallback's split file, which fallback
// loads. A negative assertion must cover both, or it cannot fail.
const relayReadySplit = readFileSync("skills/fallback/relay-ready.md", "utf-8");

const skill = readFileSync("skills/integration-setup/SKILL.md", "utf8");
const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const fallback = readFileSync("skills/fallback/SKILL.md", "utf8");
const architecture = readFileSync("docs/reference/skill-architecture.md", "utf8");

describe("integration setup skill contract", () => {
  it("owns add and pending reauth config flows", () => {
    expect(skill).toContain("name: integration-setup");
    expect(skill).toContain("/api/config/config_entries/flow_handlers");
    expect(skill).toContain('{"type":"config_entries/flow/progress"}');
    expect(skill).toContain('{"type":"config_entries/get"}');
    expect(skill).toContain('{"type":"manifest/list"}');
    expect(skill).toContain("context.source == \"reauth\"");
    expect(skill).toContain("never synthesize a reauth flow");
    expect(skill).toContain('/api/config/config_entries/flow[/<flow_id>]');
  });

  it("keeps the flow loop response-driven and preview-bound", () => {
    expect(skill).toContain("Treat every response as authoritative");
    expect(skill).toContain('`menu`');
    expect(skill).toContain('`form`');
    expect(skill).toContain('`external`');
    expect(skill).toContain('`progress`');
    expect(skill).toContain('`create_entry`');
    expect(skill).toContain('`abort`');
    expect(skill).toContain('reason == "reauth_successful"');
    expect(skill).toContain("use only fields returned by the live `data_schema`");
    expect(skill).toContain("confirmation bound to this preview");
    expect(skill).toContain("selection is the bound confirmation");
    expect(skill).toContain("validation errors");
    expect(skill).toContain("never guess a replacement");
  });

  it("keeps credentials out of chat and preserves HA-owned reauth flows", () => {
    expect(skill).toContain("never collects secrets in chat");
    expect(skill).toContain("Never ask for or echo the secret");
    expect(skill).toContain("Home Assistant UI");
    expect(skill).toContain("Settings > Devices & services");
    expect(skill).toContain("frontend-origin header");
    expect(skill).toContain("User-started flows are omitted from `config_entries/flow/progress`");
    expect(skill).toContain("never build or submit its body");
    expect(skill).toContain("Never delete a pre-existing reauth flow");
    expect(skill).toContain("Never request, echo, persist, or submit credentials from chat");
    expect(skill).toContain("Declared exception to the core delete rule above");
  });

  it("verifies config entries without entity-first guesses", () => {
    expect(skill).toContain("terminal response's `result.entry_id`");
    expect(skill).toContain("exactly one new entry");
    expect(skill).toContain("the same `entry_id` still exists");
    expect(skill).toContain("matching reauth flow is gone");
    expect(skill).toContain("Linked devices/entities are secondary evidence only");
  });

  it("wires dispatch, fallback ownership, and architecture", () => {
    expect(context).toContain(
      "| add an integration, continue a pending integration reauthentication flow, or recover invalid integration credentials when no reauth flow is pending | `ha-nova:integration-setup` |",
    );
    expect(context).toContain('"Add the Hue integration"** → `ha-nova:integration-setup`');
    expect(fallback).toContain(
      "| Integration onboarding (add / re-auth an integration via config flow) | Covered | integration-setup |",
    );
    expect(fallback + relayReadySplit).not.toContain("### Integration Onboarding -- RELAY-READY");
    expect(architecture).toContain("integration-setup/SKILL.md");
    expect(architecture).toContain("## Integration Setup Architecture");
  });
});
