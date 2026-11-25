#!/usr/bin/env node
// The dead-reference sweep (#978) and a rename pass (#979) with a headless
// agent that follows the skills, shell path (agent.mjs; the chat apps' tools
// don't carry the sweep yet):
//   1. "Do my automations use anything that no longer exists?" — one sweep,
//      the seeded broken references named, nothing written.
//   2. "Rename the living-room lights to <scheme>, ids too" — one plan, one
//      ask, the seeded automation, script, and dashboard named as consumers;
//      a "yes" renames both and writes no automation or script; then the
//      offered re-points are applied in their own flow.
//   3. "Undo all of it" — both lights and the automation are back.
//
//   node scripts/e2e/testbed/agent-references.mjs        (a running bed you own)
//
// PASS/FAIL/SKIP lines; exits non-zero on FAIL or a broken run. Everything it
// seeds or renames is put back at the end.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { HA, agentCleanup, agentSetup, agentTurn } from "./agent.mjs";

const token = readFileSync(join(dirname(fileURLToPath(import.meta.url)), ".state/ha-llat"), "utf8").trim();
const rest = (method, path, body) => fetch(HA + path, { method, headers: { Authorization: "Bearer " + token, "content-type": "application/json" }, body: body && JSON.stringify(body) });
async function ws(payloads) {
  const sock = new WebSocket(HA.replace(/^http/, "ws") + "/api/websocket");
  const out = [];
  await new Promise((resolve, reject) => {
    let id = 0;
    const next = () => (id < payloads.length ? sock.send(JSON.stringify({ id: id + 1, ...payloads[id] })) : (sock.close(), resolve()));
    sock.onmessage = (e) => {
      const m = JSON.parse(e.data);
      if (m.type === "auth_required") sock.send(JSON.stringify({ type: "auth", access_token: token }));
      else if (m.type === "auth_ok") next();
      else if (m.type === "result") { out.push(m.success ? m.result : { error: m.error }); id++; next(); }
    };
    sock.onerror = reject;
  });
  return out;
}
const exists = async (id) => (await rest("GET", "/api/states/" + id)).ok;

let failed = 0;
const line = (status, name, detail) => {
  if (status === "FAIL") failed++;
  console.log(status.padEnd(6) + name + (detail ? " (" + detail + ")" : ""));
};
const agent = agentSetup();
if (!agent) {
  line("SKIP", "agent-references", "no claude or no signed-in test account in ~/.claude-ha-nova-test");
  process.exit(0);
}

const BROKEN = "hano_refs_broken", USER = "hano_refs_living", SCRIPT = "hano_refs_living_script", BOARD = "hano-refs";
const OLD = ["light.ceiling_lights", "light.living_room_rgbww_lights"], NEW = ["light.wohnzimmer_decke", "light.wohnzimmer_rgbww"];
const brokenCfg = { alias: "Flur Nachtlicht alt", mode: "single",
  triggers: [{ trigger: "state", entity_id: "binary_sensor.hano_flur_bewegung_alt", to: "on" }],
  conditions: [], actions: [{ action: "light.turn_on", target: { entity_id: "light.hano_flur_alt", area_id: "hano_gibts_nicht" } }, { action: "hue.hano_scene_alt" }] };
const userCfg = { alias: "Wohnzimmer Abendlicht", mode: "single", triggers: [{ trigger: "sun", event: "sunset" }], conditions: [],
  actions: [{ action: "light.turn_on", target: { entity_id: OLD } }] };
const scriptCfg = { alias: "Wohnzimmer aus", sequence: [{ action: "light.turn_off", target: { entity_id: "light.ceiling_lights" } }] };
const automation = async (id) => (await rest("GET", "/api/config/automation/config/" + id)).json();
const script = async (id) => (await rest("GET", "/api/config/script/config/" + id)).json();
// Everything a turn could write: the seeded configs, the dashboard, and the two lights' registry rows.
const home = async () => {
  const [reg, board] = await ws([{ type: "config/entity_registry/list" }, { type: "lovelace/config", url_path: BOARD }]);
  return JSON.stringify([await automation(BROKEN), await automation(USER), await script(SCRIPT), board,
    reg.filter((r) => before[r.id]).map((r) => [r.id, r.entity_id, r.name])]);
};
const only = (turn, re) => turn.ran.filter((l) => re.test(l)).length;
// The two lights' registry rows as [entity_id, name], in the order of OLD (registry id order kept).
const lights = async () => { const [reg] = await ws([{ type: "config/entity_registry/list" }]); return Object.keys(before).map((id) => reg.find((r) => r.id === id)).map((r) => r && [r.entity_id, r.name]); };

let cost = 0, before = {}, board, renamed = false;
try {
  const [regBefore] = await ws([{ type: "config/entity_registry/list" }]);
  before = Object.fromEntries(regBefore.filter((r) => OLD.includes(r.entity_id)).map((r) => [r.id, r]));
  if (Object.keys(before).length !== 2) throw new Error("the bed lacks " + OLD.join(" or "));
  for (const [path, cfg] of [["/api/config/automation/config/" + BROKEN, brokenCfg], ["/api/config/automation/config/" + USER, userCfg], ["/api/config/script/config/" + SCRIPT, scriptCfg]]) {
    if (!(await rest("POST", path, cfg)).ok) throw new Error("seeding " + path + " failed");
  }
  [board] = await ws([{ type: "lovelace/dashboards/create", url_path: BOARD, title: "Wohnzimmer Tafel", mode: "storage", show_in_sidebar: false, require_admin: false }]);
  const [saved] = await ws([{ type: "lovelace/config/save", url_path: BOARD, config: { views: [{ title: "Wohnzimmer", cards: [{ type: "entities", entities: ["light.ceiling_lights"] }] }] } }]);
  if (!board?.id || saved?.error) throw new Error("seeding the dashboard failed");

  const t0 = Date.now(), seeded = await home();
  const sweep = await agentTurn(agent, { path: "shell", prompt: "Verwenden meine Automationen oder Skripte irgendetwas, das es nicht mehr gibt?" });
  cost += sweep.cost;
  const named = /Flur Nachtlicht alt/.test(sweep.reply) && /hano_flur_alt|hano_flur_bewegung_alt/.test(sweep.reply);
  const sweeps = only(sweep, /^get --dead-references\b/), unchanged = (await home()) === seeded;
  line(sweeps === 1 && named && unchanged && !only(sweep, /^(plan|call|apply)\b/) ? "PASS" : "FAIL", "sweep: exactly one call, the broken automation named, nothing changed in Home Assistant",
    JSON.stringify({ sweeps, named, unchanged, calls: sweep.ran.length, s: Math.round((Date.now() - t0) / 1000) }));
  console.log("RAN " + sweep.ran.join(" | ").slice(0, 600) + "\nREPLY " + sweep.reply.slice(0, 1500));

  const ask = await agentTurn(agent, { path: "shell", prompt: "Benenne die beiden Lampen im Wohnzimmer einheitlich um: Name „Wohnzimmer Decke“ und „Wohnzimmer RGBWW“, Entity-IDs light.wohnzimmer_decke und light.wohnzimmer_rgbww." });
  cost += ask.cost;
  const rawPlans = only(ask, /^plan raw\b/), still = (await home()) === seeded;
  const consumers = /Wohnzimmer Abendlicht/.test(ask.reply) && /Wohnzimmer aus/.test(ask.reply) && /Wohnzimmer Tafel/.test(ask.reply);
  line(rawPlans === 1 && still && consumers && !only(ask, /^(apply|call)\b/) ? "PASS" : "FAIL",
    "rename preview: exactly one raw plan, nothing changed yet, automation, script, and dashboard named", JSON.stringify({ rawPlans, still, consumers }));
  console.log("RAN " + ask.ran.join(" | ").slice(0, 800) + "\nREPLY " + ask.reply.slice(0, 1800));

  const yes = await agentTurn(agent, { path: "shell", prompt: "Ja.", resume: ask.session });
  cost += yes.cost;
  const shown = await Promise.all(NEW.map(async (n) => (await rest("GET", "/api/states/" + n)).ok && (await (await rest("GET", "/api/states/" + n)).json()).attributes.friendly_name));
  renamed = JSON.stringify(shown) === JSON.stringify(["Wohnzimmer Decke", "Wohnzimmer RGBWW"]) && !(await exists(OLD[0])) && !(await exists(OLD[1]));
  const applies = only(yes, /^apply\b/);
  const untouched = JSON.stringify((await automation(USER)).actions) === JSON.stringify(userCfg.actions) && JSON.stringify((await script(SCRIPT)).sequence) === JSON.stringify(scriptCfg.sequence);
  line(renamed && applies === 1 && untouched ? "PASS" : "FAIL", "one yes renames both in one apply; automation and script read back unchanged", JSON.stringify({ shown, applies, untouched }));
  console.log("RAN " + yes.ran.join(" | ").slice(0, 800) + "\nREPLY " + yes.reply.slice(0, 1500));

  let repoint = await agentTurn(agent, { path: "shell", prompt: "Ja, pass die Automation und das Skript an die neuen IDs an.", resume: yes.session });
  cost += repoint.cost;
  if (!JSON.stringify(await automation(USER)).includes(NEW[0])) {
    repoint = await agentTurn(agent, { path: "shell", prompt: "Ja, speichern.", resume: repoint.session });
    cost += repoint.cost;
  }
  const userAfter = JSON.stringify(await automation(USER)), scriptAfter = JSON.stringify(await script(SCRIPT));
  const pointed = NEW.every((n) => userAfter.includes(n)) && !OLD.some((o) => userAfter.includes(o)) && scriptAfter.includes(NEW[0]) && !scriptAfter.includes(OLD[0]);
  line(pointed ? "PASS" : "FAIL", "re-points in their own confirmed flow: automation and script name the new ids", repoint.ran.filter((l) => /^(plan|apply)\b/.test(l)).join(" | ").slice(0, 300));
  console.log("REPLY " + repoint.reply.slice(0, 1200));

  let undo = await agentTurn(agent, { path: "shell", prompt: "Mach die ganze Umbenennung wieder rückgängig, auch die Anpassungen.", resume: repoint.session });
  cost += undo.cost;
  let undoAsks = 1;
  for (let i = 0; i < 6 && !((await exists(OLD[0])) && (await exists(OLD[1])) && JSON.stringify(await automation(USER)).includes(OLD[0])); i++) {
    console.log("RAN " + undo.ran.join(" | ").slice(0, 800) + "\nREPLY " + undo.reply.slice(0, 600));
    undo = await agentTurn(agent, { path: "shell", prompt: "Ja.", resume: undo.session });
    cost += undo.cost;
    undoAsks++;
  }
  const back = JSON.stringify(await lights()) === JSON.stringify(Object.values(before).map((r) => [r.entity_id, r.name]));
  const userBack = JSON.stringify((await automation(USER)).actions) === JSON.stringify(userCfg.actions) &&
    JSON.stringify((await script(SCRIPT)).sequence) === JSON.stringify(scriptCfg.sequence);
  line(renamed && back && userBack ? "PASS" : "FAIL", "undo: both lights get their ids and names back, the automation and script their old ids", JSON.stringify({ renamed, back, userBack, undoAsks }));
  console.log("RAN " + undo.ran.join(" | ").slice(0, 800) + "\nREPLY " + undo.reply.slice(0, 1200));
} catch (err) {
  line("FAIL", "agent-references run", String(err.message ?? err).slice(0, 300));
} finally {
  const problems = [];
  try {
    const [regAfter] = await ws([{ type: "config/entity_registry/list" }]);
    const back = regAfter.filter((r) => before[r.id] && (r.entity_id !== before[r.id].entity_id || r.name !== before[r.id].name))
      .map((r) => ({ type: "config/entity_registry/update", entity_id: r.entity_id, new_entity_id: before[r.id].entity_id, name: before[r.id].name }));
    for (const r of back.length ? await ws(back) : []) if (r?.error) problems.push("registry: " + r.error.message);
    for (const path of ["/api/config/automation/config/" + BROKEN, "/api/config/automation/config/" + USER, "/api/config/script/config/" + SCRIPT]) {
      const res = await rest("DELETE", path);
      if (!res.ok && res.status !== 400 && res.status !== 404) problems.push(path + " " + res.status);
    }
    if (board?.id) for (const r of await ws([{ type: "lovelace/dashboards/delete", dashboard_id: board.id }])) if (r?.error) problems.push("dashboard: " + r.error.message);
  } catch (err) {
    problems.push(String(err.message ?? err));
  }
  line(problems.length ? "FAIL" : "PASS", "cleanup: seeds removed and both lights as before", problems.join("; ").slice(0, 300));
  agentCleanup(agent);
}
console.log("cost $" + cost.toFixed(2));
process.exit(failed ? 1 : 0);
