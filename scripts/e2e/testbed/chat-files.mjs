#!/usr/bin/env node
// Chat-app file access under /config at each chat-app level (0.29.0 escape:
// a chat app's file delete failed and the AI blamed the sign-in, with
// file_access on). Through the chat bridge (ha-nova serve, as the App runs
// it) with no model: MCP tool calls over HTTP, as claude.ai or ChatGPT send
// them, for the bed's owner at Ask only, Use the home, and Manage everything.
//
//   TESTBED_FILES=1 npm run testbed:up && node scripts/e2e/testbed/chat-files.mjs
//   (TESTBED_CLI_BIN = another build; SKIP when the bed's Relay has no file access)
//
// Asserts the designed outcome (below Manage everything: refused for the
// level; at Manage everything: reads run, writes and deletes need the
// one-time code, a delete needs its copy saved first, except a picture in
// www, whose copy the App keeps) and that a refusal
// names its real cause, never the sign-in, setup, or the file access
// setting. Leaves /config/custom_components/rclane/camera.py (it must
// survive); the bridge's home and data live in .state. On a gated Relay (0.30, /health
// gate 1) this bridge runs on the shared token, not the App's own bridge
// credential, as on a standalone container (0.31, MAR-154): its typed
// changes ask the phone linked to the container, never a code. On a fresh
// Relay (phone.mjs's fixture) with no phone linked, each write and delete is
// refused in the bridge's own words and the file stays as it was. Then the
// fake iPhone is linked there: a Use-the-home unlock waits for it (nothing
// sent meanwhile), runs after the tap, and sends nothing after Cancel; the
// level still refuses that user's file write; at Manage everything a file
// write runs after the tap. The App's chat-code path can't run on a
// standalone bed (one WARN line).
// Prints PASS/FAIL/WARN lines; exits non-zero on any FAIL.
import { execFileSync, spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import net from "node:net";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { answered, failures as phoneFailures, fixture, link, question, relock, resetPushes } from "./phone.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "../../..");
const STATE = join(HERE, ".state");
const HA = "http://127.0.0.1:18123", RELAY = "http://127.0.0.1:18791";
const LLAT = readFileSync(join(STATE, "ha-llat"), "utf8").trim();
const RELAY_TOKEN = readFileSync(join(STATE, "relay-token"), "utf8").trim();
const WRONG_CAUSE = /sign[- ]?in|credential|ha-nova setup|file_access|file access (setting|option)|turn on file access|can't reach (its|the) (NOVA )?Relay/i;
let failed = 0;
const check = (name, ok, detail) => {
  if (!ok) failed++;
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? ` (${detail})` : ""}`);
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const inHA = (cmd) => execFileSync("docker", ["exec", "ha-nova-testbed-ha", "sh", "-c", cmd], { encoding: "utf8" });

const health = await (await fetch(`${RELAY}/health`, { headers: { authorization: `Bearer ${RELAY_TOKEN}` } })).json();
if (health.data?.file_access !== "readwrite") {
  console.log(`SKIP  chat-app files: the bed's Relay has file access ${health.data?.file_access ?? "unknown"} (TESTBED_FILES=1 npm run testbed:up)`);
  process.exit(0);
}
const gated = health.data?.gate === 1;
if (gated) { // a fresh Relay as Testbed Relay, with no phone linked to it yet (phone.mjs)
  await fixture();
  await resetPushes();
}

// The bed's owner: the bridge lets in only a user Home Assistant lists.
const user = await new Promise((done, fail) => {
  const ws = new WebSocket(`${HA.replace(/^http/, "ws")}/api/websocket`);
  ws.onerror = () => fail(new Error("Home Assistant's WebSocket failed"));
  ws.onmessage = (event) => {
    const m = JSON.parse(event.data);
    if (m.type === "auth_required") ws.send(JSON.stringify({ type: "auth", access_token: LLAT }));
    else if (m.type === "auth_ok") ws.send(JSON.stringify({ id: 1, type: "auth/current_user" }));
    else if (m.id === 1) { ws.close(); m.success ? done(m.result.id) : fail(new Error("auth/current_user failed")); }
  };
});

let bin = process.env.TESTBED_CLI_BIN;
if (!bin) {
  execFileSync("bash", [join(HERE, "cli.sh"), "version"], { stdio: "ignore" }); // builds this checkout's CLI
  bin = join(STATE, "bin/ha-nova");
}
const base = join(STATE, `chat-files-${Date.now()}`), home = join(base, "home"), data = join(base, "data"), secret = randomBytes(32).toString("hex");
mkdirSync(join(home, ".config/ha-nova"), { recursive: true, mode: 0o700 });
mkdirSync(data, { mode: 0o700 });
// The local transport, as the App's bridge reaches its Relay (the App's own
// device credential over pinned TLS; here the bed's relay token).
writeFileSync(join(home, ".config/ha-nova/config.json"), JSON.stringify({ ha_url: HA, relay_base_url: RELAY }), { mode: 0o600 });
writeFileSync(join(home, ".test-relay-token"), RELAY_TOKEN, { mode: 0o600 });
writeFileSync(join(base, "secret"), secret, { mode: 0o600 });
const port = await new Promise((done) => { const s = net.createServer().listen(0, "127.0.0.1", () => { const { port } = s.address(); s.close(() => done(port)); }); });
const bridge = spawn(bin, ["serve", "--listen", `127.0.0.1:${port}`, "--data-dir", data, "--secret-file", join(base, "secret"), "--confirm-wait", "3s"], {
  stdio: "ignore", env: { PATH: process.env.PATH, HOME: home, HA_NOVA_DEV_ROOT: ROOT, XDG_CONFIG_HOME: join(home, ".config"), XDG_CACHE_HOME: join(home, ".cache"), HA_NOVA_NO_BROWSER: "1",
    HA_NOVA_ALLOW_INSECURE_TEST_KEYRING: "1", HA_NOVA_TEST_KEYRING_FILE: join(home, ".test-relay-token"), HA_NOVA_KEYRING_SERVICE: "ha-nova-testbed-chat-files" } });
process.on("exit", () => bridge.kill());

let id = 0;
async function tool(name, args) {
  const res = await fetch(`http://127.0.0.1:${port}/mcp`, { method: "POST", body: JSON.stringify({ jsonrpc: "2.0", id: ++id, method: "tools/call", params: { name, arguments: args } }),
    headers: { authorization: `Bearer ${secret}`, "x-ha-user-id": user, "content-type": "application/json", accept: "application/json, text/event-stream" } });
  const line = (await res.text()).split("\n").map((l) => l.replace(/^data: /, "")).find((l) => l.startsWith("{")) ?? "{}";
  const text = JSON.parse(line).result?.content?.[0]?.text ?? "";
  try { return { ...JSON.parse(text), text }; } catch { return { text: text || line }; }
}
const level = (name) => writeFileSync(join(data, "chat-apps.json"), JSON.stringify({ allowed_users: [user], levels: { [user]: name }, roles: 1 }), { mode: 0o600 });
const words = (d) => [d.error?.code, d.error?.message, d.error?.next_step, d.next_step, d.say].filter(Boolean).join(" | ");
const short = (d) => (words(d) || d.text || "(empty)").replace(/\d{4}/g, "NNNN").slice(0, 220);
const code = (d) => d.plan?.code ?? d.say?.match(/\*\*(\d{4})\*\*/)?.[1];
const fileOp = (action, path, content) => ({ endpoint: "files", payload: content === undefined ? { action, path } : { action, path, content } });
const readFile = (path) => tool("ha_read", { endpoint: "files", payload: { action: "read_file", path } });
const plan = (requests) => tool("ha_plan", { kind: "raw", requests });
// ha_apply until it has an outcome (a job answers applying or waiting_for_phone first).
async function apply(d) {
  let out = {};
  for (let i = 0; i < 120 && d.plan?.id; i++) {
    out = await tool("ha_apply", code(d) ? { plan_id: d.plan.id, confirm_code: code(d) } : { plan_id: d.plan.id });
    if (out.status !== "applying" && out.status !== "waiting_for_phone") break;
  }
  return out;
}
// The ask a typed change gets: the one-time code, or on a gated Relay the phone linked to it.
const asks = (d) => d.tier === "typed" && (gated ? d.plan?.channel === "relay" && !code(d) : Boolean(code(d)));
const lockState = async () => (await (await fetch(`${HA}/api/states/lock.front_door`, { headers: { authorization: `Bearer ${LLAT}` } })).json()).state;
const containerSay = /phone linked to your NOVA Relay container|mit deinem NOVA-Relay-Container verbunden/;

// The phone linked to the container (gated bed): every chat user's change
// that needs an OK asks it, whoever asked.
async function containerPhone(NOTE) {
  await link("ios");
  level("use");
  const bounded = await plan([fileOp("write_file", NOTE, "rc lane\n")]);
  check("use, a phone linked to the container: a file write is still refused for the level", bounded.error?.code === "CHAT_LEVEL", short(bounded));
  for (const answer of [0, 1]) {
    await relock();
    const unlock = await tool("ha_call", { service: "lock.unlock", entity_ids: ["lock.front_door"] });
    check("use: an unlock plans an ask on the container's phone, no code", unlock.plan?.channel === "relay" && !code(unlock) && containerSay.test(unlock.say ?? ""), short(unlock));
    if (!unlock.plan?.id) continue;
    let waiting = {};
    for (let i = 0; i < 20 && !(waiting.status === "waiting_for_phone" && containerSay.test(waiting.say ?? "")); i++) waiting = await tool("ha_apply", { plan_id: unlock.plan.id });
    const before = await lockState();
    check("use: until the container's phone answers, ha_apply waits on it and nothing is sent", waiting.status === "waiting_for_phone" && before === "locked", `${short(waiting)}; lock ${before}`);
    await answered(await question("ios", "Unlock", { unlock: false, urgent: true, answer }), answer);
    const done = await apply(unlock), after = await lockState();
    if (answer === 0) check("use: the tap on the container's phone applies the unlock", done.status === "verified" && after === "unlocked", `${done.status ?? short(done)}; lock ${after}`);
    else check("use: Cancel on the container's phone sends nothing", done.error?.code === "CONFIRM_DECLINED" && after === "locked", `${short(done)}; lock ${after}`);
  }
  await relock();
  level("manage");
  const wrote = await plan([fileOp("write_file", NOTE, "rc lane phone\n")]);
  const pending = apply(wrote);
  if (wrote.plan?.id) await answered(await question("ios", undefined, { unlock: false, urgent: true }));
  const done = await pending, back = await readFile(NOTE);
  check("manage: a file write asks the container's phone; the tap writes it", asks(wrote) && !done.error && JSON.stringify(back).includes("rc lane phone"), `${short(wrote)}; apply ${done.status ?? short(done)}`);
}

try {
  level("manage");
  for (let i = 0; i < 60 && !(await fetch(`http://127.0.0.1:${port}/mcp`).then(() => true, () => false)); i++) await sleep(500);
  inHA("mkdir -p /config/custom_components/rclane /config/www/rclane && printf 'pass\\n' > /config/custom_components/rclane/camera.py && printf 'not a real photo' > /config/www/rclane/door.jpg && rm -f /config/rclane-note.txt /config/rclane-run.sh && printf 'rc lane\\n' > /config/rclane-delete.txt");
  const NOTE = "/config/rclane-note.txt", DEL = "/config/rclane-delete.txt";
  const onDisk = (path) => inHA(`test -f ${path} && echo kept || echo gone`).trim();
  // The bridge's refusal when no phone is linked to the container (confirm_strings gate.chat_no_phone, en or de).
  const held = (d) => d.error?.code === "CONFIRM_UNAVAILABLE" && /no phone is linked there|dort ist kein Handy verbunden/.test(words(d)) && !WRONG_CAUSE.test(words(d));
  if (gated) console.log("WARN  App chat-code path not covered on a standalone bed: its Relay gates this bridge's shared token");

  for (const name of ["ask", "use"]) {
    level(name);
    for (const [what, d] of [["read", await readFile("/config/configuration.yaml")], ["write", await plan([fileOp("write_file", NOTE, "rc lane\n")])],
      ["delete", await plan([fileOp("delete_file", "/config/custom_components/rclane/camera.py")])]]) {
      check(`${name}: a file ${what} is refused for the level, naming it`, d.error?.code === "CHAT_LEVEL" && !WRONG_CAUSE.test(words(d)), short(d));
    }
    const call = await tool("ha_call", { service: "light.turn_off", entity_ids: ["light.living_room_rgbww_lights"] });
    check(name === "use" ? "use: an ordinary call still runs" : "ask: a call is refused for the level",
      name === "use" ? call.status === "verified" : call.error?.code === "CHAT_LEVEL", short(call));
  }

  level("manage");
  const read = await readFile("/config/configuration.yaml");
  check("manage: a file read runs", JSON.stringify(read).includes("NOVA Testbed"), short(read));
  const wrote = await plan([fileOp("write_file", NOTE, "rc lane\n")]);
  const wroteApplied = asks(wrote) ? await apply(wrote) : {};
  const back = await readFile(NOTE);
  if (gated) {
    const note = onDisk(NOTE);
    check("manage, gated Relay, no phone linked: a new file asks the container's phone, and none is linked: refused, naming why; nothing is written", asks(wrote) && held(wroteApplied) && note === "gone",
      `plan ${wrote.tier ?? short(wrote)}, apply ${short(wroteApplied)}; file ${note}`);
  } else {
    check("manage: a new file needs the code, then is written", wrote.tier === "typed" && Boolean(code(wrote)) && !wroteApplied.error && JSON.stringify(back).includes("rc lane"),
      `plan ${wrote.tier ?? short(wrote)}, apply ${wroteApplied.status ?? short(wroteApplied)}`);
  }
  const del = await plan([{ endpoint: "backups", payload: { action: "save", category: "yaml", name: "rclane-delete", data: { path: DEL, content: "rc lane\n" } } }, fileOp("delete_file", DEL)]);
  const delApplied = asks(del) ? await apply(del) : {};
  const gone = onDisk(DEL);
  if (gated) {
    check("manage, gated Relay, no phone linked: a delete with its copy saved first asks the container's phone: refused, naming why; the file stays", asks(del) && held(delApplied) && gone === "kept",
      `plan ${del.tier ?? short(del)}, apply ${short(delApplied)}; file ${gone}`);
  } else {
    check("manage: a delete with its copy saved first needs the code, then the file is gone", del.tier === "typed" && Boolean(code(del)) && !delApplied.error && gone === "gone",
      `plan ${del.tier ?? short(del)}, apply ${delApplied.status ?? short(delApplied)}, file ${gone}`);
  }

  // A picture in www (0.30): the code, no copy to save first, the App keeps
  // one; ha_read lists it, and the apply names the undo. A gated Relay holds
  // it like the other deletes: refused, naming why, and no copy is made.
  const pic = await plan([fileOp("delete_file", "/config/www/rclane/door.jpg")]);
  const picApplied = asks(pic) ? await apply(pic) : {};
  const copies = JSON.stringify(await tool("ha_read", { endpoint: "files", payload: { action: "list_deleted" } }));
  const picGone = onDisk("/config/www/rclane/door.jpg");
  const asked = asks(pic) && !/backups save/.test(pic.next_step ?? "");
  if (gated) {
    check("manage, gated Relay, no phone linked: a picture in www asks the container's phone: refused, naming why; the picture stays, no copy",
      asked && held(picApplied) && picGone === "kept" && !copies.includes("/config/www/rclane/door.jpg") && /"copies"/.test(copies),
      `plan ${pic.tier ?? short(pic)}, apply ${short(picApplied)}; file ${picGone}, ${copies.slice(0, 200)}`);
  } else {
    check("manage: a picture in www needs the code, is deleted, and its copy is listed with the undo named",
      asked && !picApplied.error && picGone === "gone" &&
        copies.includes("/config/www/rclane/door.jpg") && (picApplied.next_step ?? "").includes("restore_file"),
      `plan ${pic.tier ?? short(pic)}, apply ${picApplied.status ?? short(picApplied)}, file ${picGone}, ${copies.slice(0, 200)}`);
  }
  inHA("rm -rf /config/www/rclane");

  // The Relay refuses these itself (an HTTP 403): the answer must say why.
  const photo = await plan([fileOp("delete_file", "/config/custom_components/rclane/camera.py")]);
  const kept = inHA("test -f /config/custom_components/rclane/camera.py && echo kept || echo gone").trim();
  check("manage: deleting a file in custom_components is refused, naming why; the file stays", !photo.plan?.code && words(photo).includes("FILE_PATH_DENIED") && !WRONG_CAUSE.test(words(photo)) && kept === "kept", `${short(photo)}; ${kept}`);
  const log = await readFile("/config/home-assistant.log");
  check("manage: reading the log file is refused, naming why", words(log).includes("FILE_PATH_DENIED") && !WRONG_CAUSE.test(words(log)), short(log));
  const script = await plan([fileOp("write_file", "/config/rclane-run.sh", "#!/bin/sh\n")]);
  const scriptApplied = asks(script) ? await apply(script) : script;
  const made = inHA("test -f /config/rclane-run.sh && echo written || echo absent").trim();
  check(`manage: writing a .sh file is refused, naming why${gated ? " (no phone linked to the container yet)" : ""}; nothing is written`,
    (gated ? held(scriptApplied) : words(scriptApplied).includes("FILE_TYPE_DENIED") && !WRONG_CAUSE.test(words(scriptApplied))) && made === "absent",
    `plan ${script.tier ?? "-"}, ${short(scriptApplied)}; ${made}`);
  if (gated) await containerPhone(NOTE);
} catch (err) {
  check("chat-app files check ran", false, err.message);
}
process.exit(failed || phoneFailures() ? 1 : 0);
