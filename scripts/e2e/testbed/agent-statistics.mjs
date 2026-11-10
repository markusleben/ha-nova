#!/usr/bin/env node
// GitHub #976: an agent that follows the helper skill creates a Statistics
// helper through all three steps of Home Assistant's flow (user →
// state_characteristic → options) with state_characteristic `change`,
// max_age 3 hours, no sampling_size, and keep_last_sample on. Home Assistant
// is the oracle: one new entry, its options read back from an options flow
// this script aborts, its entity through the registry, and its state
// hydrated from the recorder with a valid source. The agent's own run must
// show the terminal `result.entry_id` and the config entries read back.
//
// A second run hides the terminal id: the bed's ha-nova drops `entry_id`
// from every `create_entry` result it prints or writes, so the agent can
// only find its helper by the skill's before/after diff of the config
// entries.
//
//   node scripts/e2e/testbed/agent-statistics.mjs     (a running bed you own)
//
// Shell path only (agent.mjs): the helper skill is the plugin's. Writes to
// the bed (two helpers, removed at the end, and sensor values). Steps agents
// still skip (#1105) report XFAIL. PASS/FAIL/XFAIL/SKIP lines; exits non-zero
// on FAIL or a broken run; SKIP exits 0.
import { execFileSync } from "node:child_process";
import { chmodSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { HA, agentCleanup, agentSetup, agentTurn } from "./agent.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const SOURCE = "sensor.testbed_temperature", TOPIC = "testbed/climate/temperature";
let failed = 0, llat;
const line = (status, name, detail) => {
  if (status === "FAIL") failed++;
  console.log(status.padEnd(6) + name + (detail ? " (" + detail + ")" : ""));
};
const check = (ok, name, detail) => line(ok ? "PASS" : "FAIL", name, detail);
const known = (ok, name, detail) => line(ok ? "PASS" : "XFAIL", name, (detail ? detail + ", " : "") + "#1105");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function ha(method, path, body) {
  const res = await fetch(HA + path, { method, headers: { authorization: "Bearer " + llat, "content-type": "application/json" }, body: body && JSON.stringify(body) });
  if (!res.ok) throw new Error(method + " " + path + ": HTTP " + res.status + " " + (await res.text()));
  return res.json();
}
const entries = async () => (await ha("GET", "/api/config/config_entries/entry?domain=statistics")).map((e) => e.entry_id);
const publish = (value) => execFileSync("docker", ["exec", "ha-nova-testbed-mqtt", "mosquitto_pub", "-h", "127.0.0.1", "-r", "-t", TOPIC, "-m", String(value)]);

// The current options, read as the helper skill reads them: start the
// entry's options flow, take the suggested values, abort it.
async function options(entry) {
  const flow = await ha("POST", "/api/config/config_entries/options/flow", { handler: entry });
  await ha("DELETE", "/api/config/config_entries/options/flow/" + flow.flow_id);
  return Object.fromEntries((flow.data_schema ?? []).map((f) => [f.name, f.description?.suggested_value]));
}

// The fixture: the bed's ha-nova, but every create_entry result loses its
// entry_id, on stdout and in an --out file. Each stripped id is logged.
function hideTerminalID(agent) {
  const strip = join(agent.bin, "strip.mjs"), stripped = join(agent.bin, "stripped.log");
  writeFileSync(stripped, "");
  writeFileSync(strip, `import { spawnSync } from "node:child_process";
import { appendFileSync, existsSync, readFileSync, writeFileSync } from "node:fs";
const args = process.argv.slice(2);
const run = spawnSync("bash", [${JSON.stringify(join(HERE, "cli.sh"))}, ...args], { encoding: "utf8", stdio: ["inherit", "pipe", "inherit"] });
const walk = (v) => {
  if (Array.isArray(v)) return v.forEach(walk);
  if (!v || typeof v !== "object") return;
  if (v.type === "create_entry" && v.result && typeof v.result === "object" && "entry_id" in v.result) {
    appendFileSync(${JSON.stringify(stripped)}, v.result.entry_id + "\\n");
    delete v.result.entry_id;
  }
  Object.values(v).forEach(walk);
};
const hide = (text) => text.split("\\n").map((l) => { try { const j = JSON.parse(l); walk(j); return JSON.stringify(j); } catch { return l; } }).join("\\n");
const whole = (text) => { try { const j = JSON.parse(text); walk(j); return JSON.stringify(j, null, 2) + "\\n"; } catch { return hide(text); } };
process.stdout.write(whole(run.stdout ?? ""));
const out = args[args.indexOf("--out") + 1];
if (args.includes("--out") && out && existsSync(out)) writeFileSync(out, whole(readFileSync(out, "utf8")));
process.exit(run.status ?? 1);
`);
  writeFileSync(join(agent.bin, "ha-nova"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + agent.log + "'\nTESTBED_CLI_HOME='" + agent.home + "' TESTBED_CLI_BIN='" + (process.env.TESTBED_CLI_BIN || join(HERE, ".state/bin/ha-nova")) + "' exec node '" + strip + "' \"$@\"\n");
  chmodSync(join(agent.bin, "ha-nova"), 0o755);
  return () => readFileSync(stripped, "utf8").split("\n").filter(Boolean);
}

// One create, turn by turn: the agent previews from the live form and asks;
// the user says yes (and types a code if the plan shows one), up to 4 turns.
async function create(agent, name, before) {
  const prompt = "Create a Statistics helper named \"" + name + "\" for " + SOURCE + ": characteristic change, max age 3 hours, no sampling size limit, and keep the last sample.";
  const turns = [];
  for (let i = 0, session; i < 4; i++) {
    const code = turns.at(-1)?.reply.match(/\*\*(\d{4})\*\*/)?.[1];
    const t = await agentTurn(agent, { path: "shell", prompt: i === 0 ? prompt : "Yes, create it as shown." + (code ? " The code is " + code + "." : ""), resume: session });
    session = t.session;
    turns.push(t);
    if ((await entries()).some((e) => !before.includes(e))) break;
  }
  const added = (await entries()).filter((e) => !before.includes(e));
  const all = (key) => turns.flatMap((t) => t[key]);
  return { added, turns, results: all("results").join("\n"), ran: all("ran"), reply: turns.at(-1).reply, cost: turns.reduce((n, t) => n + t.cost, 0) };
}

// One WebSocket command (the entity registry has no REST API).
async function ws(type) {
  const socket = new WebSocket(HA.replace(/^http/, "ws") + "/api/websocket");
  try {
    return await new Promise((resolve, reject) => {
      socket.onerror = () => reject(new Error("Home Assistant's WebSocket failed"));
      socket.onmessage = (event) => {
        const m = JSON.parse(event.data);
        if (m.type === "auth_required") socket.send(JSON.stringify({ type: "auth", access_token: llat }));
        else if (m.type === "auth_ok") socket.send(JSON.stringify({ id: 1, type }));
        else if (m.id === 1) m.success ? resolve(m.result) : reject(new Error(type + ": " + m.error?.message));
      };
    });
  } finally {
    socket.close();
  }
}
// The helper's entity, through the registry: the row its entry owns.
const entityOf = async (entry) => (await ws("config/entity_registry/list")).filter((r) => r.config_entry_id === entry).map((r) => r.entity_id);

const agent = agentSetup();
if (!agent) {
  line("SKIP", "agent statistics helper", "needs claude on PATH and a signed-in ~/.claude-ha-nova-test");
  process.exit(0);
}
llat = readFileSync(join(HERE, ".state/ha-llat"), "utf8").trim();
const baseline = await entries();
const removeNew = async () => { for (const e of (await entries().catch(() => [])).filter((e) => !baseline.includes(e))) await ha("DELETE", "/api/config/config_entries/entry/" + e).catch(() => {}); };
process.on("SIGINT", async () => { await removeNew(); agentCleanup(agent); process.exit(130); });
let cost = 0;
try {
  for (const v of [20.5, 21.5, 22.0]) { publish(v); await sleep(3000); }
  await sleep(6000); // the recorder commits every 5 s: the helper finds these samples at its start

  const run = await create(agent, "Agent temperature change", baseline);
  cost += run.cost;
  check(run.added.length === 1, "one new Statistics helper", run.added.join(",") || "none after " + run.turns.length + " turns: " + run.reply.slice(0, 200));
  if (run.added.length === 1) {
    const entry = run.added[0];
    await sleep(2000);
    const opts = await options(entry);
    const row = (await ha("GET", "/api/config/config_entries/entry?domain=statistics")).find((e) => e.entry_id === entry);
    check(row?.title === "Agent temperature change" && row?.state === "loaded", "its config entry reads back loaded, under its name", JSON.stringify({ title: row?.title, state: row?.state }));
    check(opts.state_characteristic === "change" && opts.max_age?.hours === 3 && !opts.max_age?.minutes && !opts.max_age?.seconds && opts.keep_last_sample === true && opts.sampling_size == null,
      "its options flow reads back change, 3 h, no sampling size, keep the last sample", JSON.stringify(opts));
    const owned = await entityOf(entry);
    check(owned.length === 1 && owned[0].startsWith("sensor."), "its one sensor resolves through the entity registry", owned.join(",") || "no registry row");
    const entity = owned.length === 1 ? owned[0] : undefined;
    const state = entity && (await ha("GET", "/api/states/" + entity));
    check(state && !Number.isNaN(Number(state.state)) && state.attributes.source_value_valid === true && typeof state.attributes.age_coverage_ratio === "number",
      "its sensor is hydrated from the recorder with a valid source", state ? entity + " = " + state.state + ", " + JSON.stringify({ valid: state.attributes.source_value_valid, coverage: state.attributes.age_coverage_ratio, buffer: state.attributes.buffer_usage_ratio }) : "no state");
    check(/create_entry/.test(run.results) && run.results.includes(entry), "the agent saw the terminal result.entry_id");
    const terminal = run.ran.findLastIndex((l) => /config_entries\/flow\//.test(l));
    check(terminal >= 0 && run.ran.slice(terminal + 1).some((l) => /relay ws/.test(l)), "the agent read the config entries back after the last flow step", run.ran.slice(terminal + 1).join(" | ") || "nothing after it");
    const reopened = run.ran.findIndex((l) => /POST.*options\/flow( |$)/.test(l)), aborted = run.ran.findIndex((l) => /DELETE.*options\/flow\//.test(l));
    known(reopened >= 0 && aborted > reopened && !run.ran.some((l) => /POST.*options\/flow\//.test(l)), "the agent reopened the options flow and aborted it, unsubmitted", run.ran.filter((l) => /options\/flow/.test(l)).join(" | ") || "no options flow call");
    check(terminal >= 0 && run.ran.slice(terminal + 1).some((l) => /api\/states\/|(^|\s)(get|state)\s/.test(l) && l.includes(entity)), "the agent read the new sensor's state after the create", run.ran.slice(terminal + 1).join(" | ") || "nothing after it");
    // Results name entities by their Home Assistant name, not their id (output-rules.md).
    check(state && run.reply.includes(state.attributes.friendly_name), "the reply names the new sensor", state?.attributes.friendly_name);
  }

  const reveal = hideTerminalID(agent);
  const before = await entries();
  const hidden = await create(agent, "Agent temperature change hidden id", before);
  cost += hidden.cost;
  const stripped = reveal();
  check(stripped.length > 0, "fixture: the terminal result lost its entry_id", stripped.join(",") || "no create_entry result passed the bed's ha-nova: the case proves nothing");
  check(hidden.added.length === 1 && stripped.includes(hidden.added[0]), "fixture: one new helper, the one whose id was hidden", hidden.added.join(",") || "none: " + hidden.reply.slice(0, 200));
  if (hidden.added.length === 1) {
    // The terminal result lost the id, so the agent only has it from the entries it read after;
    // the diff needs the baseline it read before the flow started.
    const start = hidden.ran.findIndex((l) => /config_entries\/flow/.test(l)), end = hidden.ran.findLastIndex((l) => /config_entries\/flow\//.test(l));
    const [hiddenEntity] = await entityOf(hidden.added[0]);
    const named = (await ha("GET", "/api/states/" + hiddenEntity)).attributes.friendly_name;
    check(hidden.ran.slice(end + 1).some((l) => /relay ws/.test(l)) && (hidden.results.includes(hidden.added[0]) || hidden.reply.includes(named)) && !/ambiguous/i.test(hidden.reply),
      "fixture: the agent found its helper in the entries it read after the flow", named + ": " + hidden.ran.slice(end + 1).join(" | ").slice(0, 300));
    known(start > 0 && hidden.ran.slice(0, start).some((l) => /relay ws/.test(l)), "fixture: the agent read the entries before the flow, the diff's baseline",
      hidden.ran.slice(0, Math.max(start, 0)).join(" | ").slice(0, 300) || "no command before the flow");
  }
} catch (err) {
  line("FAIL", "the run itself", err.message);
} finally {
  await removeNew();
  agentCleanup(agent);
  console.log("cost  $" + cost.toFixed(2));
}
process.exit(failed ? 1 : 0);
