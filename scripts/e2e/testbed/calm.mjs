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
// integration's). Then the service-call checks (calls.mjs, MAR-127): a light
// call stays ordinary beside the cover watchdog, a cover call still asks, a
// light call or edit that sets off an area, floor, or label trigger asks
// (MAR-142), and on this real Home Assistant every listener a plan's calls set off is among
// its consumers. The CLI runs in a new home in .state. On a gated Relay (0.30,
// /health gate 1) a typed plan asks the linked phone, not with a code: the
// lane links the phone lane's fake iPhone (phone.mjs, a fresh Relay) and taps
// each typed apply there. Prints PASS/FAIL lines; exits non-zero on any FAIL.
import { execFile, execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { callChecks } from "./calls.mjs";
import { answered, approve, failures, fixture, link, resetPushes } from "./phone.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const STATE = join(HERE, ".state");
const HA = "http://127.0.0.1:18123";
const LLAT = readFileSync(join(STATE, "ha-llat"), "utf8").trim();
// Its own home in the bed's state (testbed:down wipes it): an empty undo list on
// every run, and codes in the answer (no confirmation window on a desktop).
const HOME = join(STATE, `calm-home-${Date.now()}`), TMP = join(HOME, "drafts");
for (const dir of [".config/ha-nova", ".cache/ha-nova", "drafts"]) mkdirSync(join(HOME, dir), { recursive: true, mode: 0o700 });
writeFileSync(join(HOME, ".config/ha-nova/config.json"), JSON.stringify({ ha_url: HA, relay_base_url: "http://127.0.0.1:18791", confirm_window: "off" }), { mode: 0o600 });
writeFileSync(join(HOME, ".test-relay-token"), readFileSync(join(STATE, "relay-token"), "utf8"), { mode: 0o600 });
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
const envelope = (out) => { try { return JSON.parse(out.split("\n").find((l) => l.startsWith("{")) ?? "{}"); } catch { return {}; } };
function cli(...args) {
  let out;
  try {
    out = execFileSync("bash", [join(HERE, "cli.sh"), ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"], env: { ...process.env, TESTBED_CLI_HOME: HOME } });
  } catch (err) {
    out = err.stdout ?? "";
  }
  return envelope(out);
}
const reasons = (d) => (d.reasons ?? []).map((r) => r.code).join(",") || "-";
const gated = (await (await fetch("http://127.0.0.1:18791/health", { headers: { authorization: `Bearer ${readFileSync(join(STATE, "relay-token"), "utf8").trim()}` } })).json()).data?.gate === 1;
// A typed plan asks a person: on a gated Relay the linked phone (no code), else a four-digit code.
const asksPerson = (plan) => plan.tier === "typed" && plan.plan?.confirm === "code" && (gated ? plan.plan.channel === "relay" && plan.plan.code === undefined : /^\d{4}$/.test(plan.plan?.code ?? ""));
// Applies a plan; a relay plan only after the linked fake phone's tap.
async function apply(plan) {
  const args = ["apply", "--plan", plan.plan.id, ...(plan.plan.code ? ["--confirm", plan.plan.code] : [])];
  if (plan.plan.channel !== "relay") return cli(...args);
  let out = "";
  const child = execFile("bash", [join(HERE, "cli.sh"), ...args], { env: { ...process.env, TESTBED_CLI_HOME: HOME } });
  child.stdout.on("data", (d) => (out += d));
  const done = new Promise((resolve) => child.on("close", resolve));
  try {
    await answered(await approve(60_000));
  } catch (err) {
    child.kill();
    throw err;
  }
  await done;
  return envelope(out);
}

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

// One WebSocket command (helpers have no REST API).
async function ws(type, payload) {
  const socket = new WebSocket(`${HA.replace(/^http/, "ws")}/api/websocket`);
  try {
    return await new Promise((resolve, reject) => {
      socket.onerror = () => reject(new Error("Home Assistant's WebSocket failed"));
      socket.onmessage = (event) => {
        const m = JSON.parse(event.data);
        if (m.type === "auth_required") socket.send(JSON.stringify({ type: "auth", access_token: LLAT }));
        else if (m.type === "auth_ok") socket.send(JSON.stringify({ id: 1, type, ...payload }));
        else if (m.id === 1) m.success ? resolve(m.result) : reject(new Error(`${type}: ${m.error?.message}`));
      };
    });
  } finally {
    socket.close();
  }
}

// One at a time: each save rewrites automations.yaml and reloads it.
async function loadAutomations(configs) {
  const ids = Object.keys(configs);
  let loaded = 0;
  for (const [i, [id, config]] of Object.entries(configs).entries()) {
    await ha("POST", `/api/config/automation/config/${id}`, config);
    for (let tries = 0; tries < 30 && loaded <= i; tries++, await sleep(500)) loaded = (await ha("GET", "/api/states")).filter((s) => ids.includes(s.attributes?.id) && s.state === "on").length;
  }
  return { loaded, ids };
}

let listener;
try {
  if (gated) {
    await fixture();
    await resetPushes();
    await link("ios");
  }
  listener = await holdListener("automation_reloaded");
  const { loaded, ids } = await loadAutomations({ ...SHAPES, [TARGET]: edited(light("light.turn_on")) });
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
    check(`an edit that adds ${name} ${target} still asks a person`, asksPerson(plan), `tier ${plan.tier ?? plan.error?.code}, channel ${plan.plan?.channel ?? "-"}, reasons ${reasons(plan)}`);
  }
  const reload = cli("plan", "call", "automation.reload", "--set", `id=${TARGET}`);
  check("a reload you call is still rated by what it sets off: asks a person", asksPerson(reload), `tier ${reload.tier ?? reload.error?.code}, channel ${reload.plan?.channel ?? "-"}, reasons ${reasons(reload)}`);
  await callChecks({ ha, ws, cli, apply, check, watchdogID: "rclane_home_cover_watchdog", loadAutomations, planWrite, gated });
} catch (err) {
  check("calm check ran", false, err.message);
} finally {
  listener?.close();
}
process.exit(failed || failures() ? 1 : 0);
