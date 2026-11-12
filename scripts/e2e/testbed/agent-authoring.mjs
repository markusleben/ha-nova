#!/usr/bin/env node
// HA 2026.7+ authoring parity with a headless agent that follows the skills,
// on each path (agent.mjs): it creates an automation with a room trigger,
// explains an existing room-targeted one, and reviews it.
//
//   node scripts/e2e/testbed/agent-authoring.mjs        (a running bed you own)
//   AGENT_PATHS=shell node scripts/e2e/testbed/agent-authoring.mjs
//
// Oracles stay loose: the saved trigger is purpose-specific (`<domain>.<name>`)
// with the living room as its target, the explanation names the floor and the
// excluded room, and the review changes nothing and calls no shape invalid.
// PASS/FAIL/SKIP lines; exits non-zero on FAIL or a broken run.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { HA, agentCleanup, agentSetup, agentTurn } from "./agent.mjs";

const token = readFileSync(join(dirname(fileURLToPath(import.meta.url)), ".state/ha-llat"), "utf8").trim();
const ha = (method, path, body) => fetch(HA + path, { method, headers: { Authorization: "Bearer " + token, "content-type": "application/json" }, body: body && JSON.stringify(body) });
const automations = async () => (await (await ha("GET", "/api/states")).json()).filter((s) => s.entity_id.startsWith("automation.")).map((s) => s.attributes.id).filter(Boolean);

let failed = 0;
const line = (status, name, detail) => {
  if (status === "FAIL") failed++;
  console.log(status.padEnd(6) + name + (detail ? " (" + detail + ")" : ""));
};

const agent = agentSetup();
if (!agent) {
  line("SKIP", "agent-authoring", "no claude or no signed-in test account in ~/.claude-ha-nova-test");
  process.exit(0);
}
const SEEDED = "hano_authoring_ground_floor";
const seeded = {
  alias: "Erdgeschoss Licht Hinweis", description: "", mode: "single",
  triggers: [{ trigger: "light.turned_on", target: { floor_id: "erdgeschoss", exclude: { area_id: ["kuche"] } }, options: { behavior: "first" } }],
  conditions: [],
  actions: [{ action: "persistent_notification.create", data: { message: "Licht im Erdgeschoss an" } }],
};
const before = new Set(await automations());
if (!(await ha("POST", "/api/config/automation/config/" + SEEDED, seeded)).ok) throw new Error("seeding the room-targeted automation failed");
let cost = 0;
try {
  for (const path of (process.env.AGENT_PATHS ?? "shell,mcp").split(",")) {
    let turn = await agentTurn(agent, { path, prompt: "Erstelle eine Automation: Wenn es im Wohnzimmer wärmer als 25 Grad wird, sollen die Lichter im Wohnzimmer ausgehen. Leg sie an." });
    cost += turn.cost;
    if (!(await automations()).some((id) => !before.has(id) && id !== SEEDED)) {
      turn = await agentTurn(agent, { path, prompt: "Ja, leg sie an.", resume: turn.session });
      cost += turn.cost;
    }
    const created = (await automations()).filter((id) => !before.has(id) && id !== SEEDED);
    const configs = await Promise.all(created.map(async (id) => (await ha("GET", "/api/config/automation/config/" + id)).json()));
    const roomTrigger = configs.some((c) => (c.triggers ?? []).some((t) => /^[a-z_]+\.[a-z_]+$/.test(t.trigger ?? "") && JSON.stringify(t.target ?? {}).includes("wohnzimmer")));
    line(roomTrigger ? "PASS" : "FAIL", path + " create: purpose-specific trigger on the living room", JSON.stringify(configs.map((c) => c.triggers)).slice(0, 400));
    if (!roomTrigger) console.log("REPLY " + turn.reply.slice(0, 1200));
    for (const id of created) await ha("DELETE", "/api/config/automation/config/" + id);

    const explain = await agentTurn(agent, { path, prompt: "Was macht meine Automation „Erdgeschoss Licht Hinweis“?" });
    cost += explain.cost;
    const named = /erdgeschoss|ground floor/i.test(explain.reply) && /küche|kuche|kitchen/i.test(explain.reply);
    line(named ? "PASS" : "FAIL", path + " explain: names the floor and the excluded room");
    console.log("REPLY " + explain.reply.slice(0, 900));

    const review = await agentTurn(agent, { path, prompt: "Prüf die Automation „Erdgeschoss Licht Hinweis“ auf Fehler." });
    cost += review.cost;
    const texts = review.calls.map((c) => c.text);
    const wrote = texts.some((t) => /ha-nova\s+(plan|call|apply)\b|mcp__hanova__ha_(plan|call|apply)/.test(t)) || review.calls.some((c) => /ha_(plan|call|apply)$/.test(c.name));
    const misread = /(ungültig|veraltet|unbekannte?r? (trigger|auslöser)|invalid|legacy|unknown trigger)/i.test(review.reply);
    line(!wrote && !misread && review.reply.trim() ? "PASS" : "FAIL", path + " review: nothing changed, the shape is not called invalid");
    console.log("REPLY " + review.reply.slice(0, 900));
  }
} catch (err) {
  line("FAIL", "agent-authoring run", String(err.message ?? err).slice(0, 300));
} finally {
  for (const id of await automations()) if (!before.has(id)) await ha("DELETE", "/api/config/automation/config/" + id);
  agentCleanup(agent);
}
console.log("cost $" + cost.toFixed(2));
process.exit(failed ? 1 : 0);
