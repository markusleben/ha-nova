#!/usr/bin/env node
// Calm edits in a home shaped like a real one (0.29.0-rc1 escape, #1055/#1066):
// a real home has listeners that a reload sets off, and an edit of the user's
// own automation must still run without an ask (C2), and so must a second
// edit of it right away (C3), while an edit that adds an unlock or a garage
// opening still asks with a code.
//
//   node scripts/e2e/testbed/calm.mjs      (a running bed; TESTBED_CLI_BIN = another build)
//
// Adds to the bed through Home Assistant's own config API (testbed:down
// removes them): an automation that turns the office light off on every
// automation reload, a panel automation whose templated action follows that
// light (as an NSPanel blueprint does), a cover watchdog on call_service
// filtered to cover, and the automation it edits. While it runs it holds one
// more automation_reloaded listener that no automation owns (as an
// integration's). The CLI runs in a new home in .state. Prints PASS/FAIL lines;
// exits non-zero on any FAIL.
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const STATE = join(HERE, ".state");
const HA = "http://127.0.0.1:18123";
const LLAT = readFileSync(join(STATE, "ha-llat"), "utf8").trim();
const TMP = mkdtempSync(join(tmpdir(), "ha-nova-testbed-calm-"));
const HOME = join(STATE, `calm-home-${Date.now()}`); // an empty undo list on every run
process.on("exit", () => rmSync(TMP, { recursive: true, force: true }));
let failed = 0;
const check = (name, ok, detail) => {
  if (!ok) failed++;
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? ` (${detail})` : ""}`);
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function ha(method, path, body) {
  const res = await fetch(`${HA}${path}`, { method, headers: { authorization: `Bearer ${LLAT}`, "content-type": "application/json" }, body: body && JSON.stringify(body) });
  if (!res.ok) throw new Error(`${method} ${path}: HTTP ${res.status} ${await res.text()}`);
  return res.json();
}
// The CLI prints its envelope as one JSON line.
function cli(...args) {
  let out;
  try {
    out = execFileSync("bash", [join(HERE, "cli.sh"), ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"], env: { ...process.env, TESTBED_CLI_HOME: HOME } });
  } catch (err) {
    out = err.stdout ?? "";
  }
  try { return JSON.parse(out.split("\n").find((l) => l.startsWith("{")) ?? "{}"); } catch { return {}; }
}
const reasons = (d) => (d.reasons ?? []).map((r) => r.code).join(",") || "-";

const OFFICE = "light.office_rgbw_lights";
const TARGET = "rclane_calm_hall";
const edited = (actions) => ({ alias: "RC calm hall", description: "Edited by calm.mjs; its trigger never fires.",
  triggers: [{ trigger: "event", event_type: "rclane_calm_never" }], actions, mode: "single" });
const light = (action) => [{ action, target: { entity_id: "light.bed_light" } }];
let drafts = 0;
function planWrite(actions) {
  const file = join(TMP, `draft-${++drafts}.json`);
  writeFileSync(file, JSON.stringify(edited(actions)));
  return cli("plan", "write", "--domain", "automation", "--id", TARGET, "--file", file);
}
const SHAPES = {
  rclane_home_reload_light: { alias: "RC home office light off on reload", triggers: [{ trigger: "homeassistant", event: "start" }, { trigger: "event", event_type: "automation_reloaded" }],
    actions: [{ action: "light.turn_off", target: { entity_id: OFFICE } }], mode: "single" },
  rclane_home_panel: { alias: "RC home panel follows the office light", variables: { panel_name: "office_panel" }, triggers: [{ trigger: "state", entity_id: OFFICE }],
    actions: [{ action: "esphome.{{ panel_name }}_component_text", data: { text: `{{ states('${OFFICE}') }}` } }], mode: "queued" },
  rclane_home_cover_watchdog: { alias: "RC home cover watchdog", triggers: [{ trigger: "event", event_type: "call_service", event_data: { domain: "cover" } }],
    conditions: [{ condition: "template", value_template: "{{ trigger.event.data.service != 'open_cover' }}" }],
    actions: [{ action: "cover.open_cover", target: { entity_id: "{{ trigger.event.data.service_data.entity_id }}" } }], mode: "queued" },
};

// A listener no automation owns: a WebSocket subscription to the event.
async function holdListener(eventType) {
  const ws = new WebSocket(`${HA.replace(/^http/, "ws")}/api/websocket`);
  await new Promise((resolve, reject) => {
    ws.onerror = () => reject(new Error("Home Assistant's WebSocket failed"));
    ws.onmessage = (event) => {
      const m = JSON.parse(event.data);
      if (m.type === "auth_required") ws.send(JSON.stringify({ type: "auth", access_token: LLAT }));
      else if (m.type === "auth_ok") ws.send(JSON.stringify({ id: 1, type: "subscribe_events", event_type: eventType }));
      else if (m.id === 1) m.success ? resolve() : reject(new Error(`subscribe_events ${eventType} failed`));
    };
  });
  return ws;
}

let listener;
try {
  listener = await holdListener("automation_reloaded");
  // One at a time: each save rewrites automations.yaml and reloads it.
  const ids = [];
  let loaded = 0;
  for (const [id, config] of Object.entries({ ...SHAPES, [TARGET]: edited(light("light.turn_on")) })) {
    await ha("POST", `/api/config/automation/config/${id}`, config);
    ids.push(id);
    for (let i = 0; i < 30 && loaded < ids.length; i++, await sleep(500)) loaded = (await ha("GET", "/api/states")).filter((s) => ids.includes(s.attributes?.id) && s.state === "on").length;
  }
  const events = Object.fromEntries((await ha("GET", "/api/events")).map((e) => [e.event, e.listener_count]));
  check("home shapes loaded: reload light, panel, cover watchdog, edited automation", loaded === ids.length,
    `${loaded}/${ids.length} on; listeners: automation_reloaded ${events.automation_reloaded ?? 0}, call_service ${events.call_service ?? 0}`);

  for (const [step, action] of [["C2 first edit of own automation", "light.turn_off"], ["C3 second edit right away", "light.toggle"]]) {
    const plan = planWrite(light(action));
    const calm = plan.tier === "ordinary" && plan.plan?.confirm === "none";
    check(`${step}: no ask`, calm, `tier ${plan.tier ?? plan.error?.code}, confirm ${plan.plan?.confirm}, reasons ${reasons(plan)}`);
    const applied = calm ? cli("apply", "--plan", plan.plan.id) : {};
    check(`${step}: applies and saves its way back`, applied.status === "verified" && Boolean(applied.undo), `status ${applied.status ?? "not applied"}${applied.error ? ` ${applied.error.code}` : ""}`);
  }
  const undo = cli("plan", "revert", "--domain", "automation", "--target", TARGET);
  const reverted = undo.plan?.id ? cli("apply", "--plan", undo.plan.id) : {};
  const back = (await ha("GET", `/api/config/automation/config/${TARGET}`)).actions?.[0]?.action;
  check("undo previews, then puts back the state before the latest edit", undo.tier === "preview" && reverted.status === "verified" && back === "light.turn_off",
    `tier ${undo.tier ?? undo.error?.code}, apply ${reverted.status ?? "-"}, action now ${back}`);

  for (const [name, target] of [["lock.unlock", "lock.entrance_door"], ["cover.open_cover", "cover.testbed_garage"]]) {
    const plan = planWrite([{ action: name, target: { entity_id: target } }]);
    check(`an edit that adds ${name} ${target} still asks with a code`, plan.tier === "typed" && plan.plan?.confirm === "code", `tier ${plan.tier ?? plan.error?.code}, reasons ${reasons(plan)}`);
  }
  const reload = cli("plan", "call", "automation.reload", "--set", `id=${TARGET}`);
  check("a reload you call is still rated by what it sets off: asks with a code", reload.tier === "typed", `tier ${reload.tier ?? reload.error?.code}, reasons ${reasons(reload)}`);
} catch (err) {
  check("calm check ran", false, err.message);
} finally {
  listener?.close();
}
process.exit(failed ? 1 : 0);
