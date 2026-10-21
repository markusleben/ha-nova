import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// The demo's "Start here" view (scripts/demo/hosted/demo.mjs) and the
// automations and scripts HA NOVA made on the test bed (made-with-nova.json).
const work: Array<{ entity_id: string; asked: string; domain: string; id: string; config: Record<string, unknown> }> =
  JSON.parse(readFileSync("scripts/demo/hosted/made-with-nova.json", "utf8"));
const demo = readFileSync("scripts/demo/hosted/demo.mjs", "utf8");
const slug = (alias: string) => alias.toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_|_$/g, "");

describe("hosted demo: HA NOVA's sample work", () => {
  it("says what was asked and keeps HA NOVA's payload shape", () => {
    expect(work.length).toBeGreaterThan(0);
    for (const w of work) {
      const c = w.config;
      expect(c.description).toBe(`Made with HA NOVA from: “${w.asked}”`);
      expect(w.entity_id).toBe(`${w.domain}.${w.domain === "script" ? w.id : slug(String(c.alias))}`);
      const keys = w.domain === "automation" ? ["alias", "triggers", "conditions", "actions", "mode"] : ["alias", "sequence", "mode"];
      for (const key of keys) expect(c, `${w.entity_id} ${key}`).toHaveProperty(key);
      expect(w.id).toMatch(/^[A-Za-z0-9_-]+$/); // the config API path HA NOVA writes to
    }
  });

  it("lists the same tour prompts as ha-nova try", () => {
    const tryText = readFileSync("cli/command_try.go", "utf8");
    const prompts = [...demo.matchAll(/^ {2}"(In the demo house, [^"]+)",$/gm)].map((m) => m[1]);
    expect(prompts.length).toBe(5);
    for (const p of prompts.slice(0, 4)) expect(tryText).toContain(`"${p}"`);
  });
});
