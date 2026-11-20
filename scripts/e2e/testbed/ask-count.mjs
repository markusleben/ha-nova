#!/usr/bin/env node
// Ask count (0.32 friction budget; spec
// docs/work/2026-11-16-confirmation-simplification-0.32-spec.md, "Friction
// budget" and "Ask count"): how many times a person must answer before an
// action is done, per row and column of the friction table, with the words
// they read. It runs the real 0.32 CLI (linux, in a throwaway container with a
// fake zenity) against the bed's Relay image started as a NOVA App
// (app-relay.mjs: the real App, safety-check worker, registry, and NOVA page;
// only the Supervisor's self-API and ingress are stood in for), the bed's
// real Home Assistant users, and the phone lane's fake phones.
//
// A stage is one answer a person gives: a window Confirm, a phone tap, a
// typed code, or the chat "yes" a preview asks for (counted from the plan:
// that one is the assistant's, not HA NOVA's). The lane answers every
// surface that asks, on the surface the row has, until the command ends, so
// a second ask for one action shows as a second stage. Surfaces one stage
// shows at once (window and phone for one ask) count once: the lane answers
// one of them. It also counts phone questions with sound, which must be 0
// on the owner's own computer while the window is answered.
//
//   npm run testbed:up
//   node scripts/e2e/testbed/ask-count.mjs
//
// Rows: S1 owner's own computer declared on the NOVA page, phone linked; S2
// the same before the phone is linked; S5 the same with no display
// (headless); S3 the same after the owner turned it off on the NOVA page;
// S4 a computer nobody declared (a second administrator's), its own phone;
// S5H a bot's host (MAR-244): no display and no Secret Service, paired with a
// code, its key in a private file; without a phone, with the owner's chat
// code, then with a phone, which ends the code.
// M: Markus's home (MAR-209) on S1, through mrow.mjs, without and with a
// logbook open (everyday, shutter, garage). Not staged here, and
// printed so: S1L (Home Assistant Link transport), S6-S8 (chat apps,
// unchanged in 0.32: the chat acceptance lane), a computer paired before
// 0.32 (its Relay side: tests/security/window-confirm.test.ts).
// Prints the table and the words; exits non-zero on any FAIL.
import { execFileSync, spawn, spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, statSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { failures, fixture, read, relock, report } from "./phone.mjs";
import { mRowChecks } from "./mrow.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, "../../..");
const HA = "http://127.0.0.1:18123", PUSH = "http://127.0.0.1:18792";
const BOOT = 18796, SECURE = 18797, FRONT = 18798, RELAY = "ha-nova-testbed-ask-count-" + process.pid, KEYRING = "ha-nova-testbed-keyring";
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const LLAT = read("ha-llat");

async function ha(method, path, body) {
  const res = await fetch(HA + path, { method, headers: { authorization: "Bearer " + LLAT, "content-type": "application/json" }, body: body && JSON.stringify(body) });
  if (!res.ok) throw new Error(method + " " + path + ": HTTP " + res.status + " " + await res.text());
  return res.json();
}
async function ws(type, payload = {}) {
  const socket = new WebSocket(HA.replace(/^http/, "ws") + "/api/websocket");
  try {
    return await new Promise((resolve, reject) => {
      socket.onerror = () => reject(new Error("Home Assistant's WebSocket failed"));
      socket.onmessage = (event) => {
        const m = JSON.parse(event.data);
        if (m.type === "auth_required") socket.send(JSON.stringify({ type: "auth", access_token: LLAT }));
        else if (m.type === "auth_ok") socket.send(JSON.stringify({ id: 1, type, ...payload }));
        else if (m.id === 1) m.success ? resolve(m.result) : reject(new Error(type + ": " + m.error?.message));
      };
    });
  } finally {
    socket.close();
  }
}
async function loadAutomations(configs) {
  const ids = Object.keys(configs);
  let loaded = 0;
  for (const [i, [id, config]] of Object.entries(configs).entries()) {
    await ha("POST", "/api/config/automation/config/" + id, config);
    for (let tries = 0; tries < 30 && loaded <= i; tries++, await sleep(500)) loaded = (await ha("GET", "/api/states")).filter((s) => ids.includes(s.attributes?.id) && s.state === "on").length;
  }
  return { loaded, ids };
}

// The NOVA page as a Home Assistant user, through the ingress stand-in.
const page = async (user) => (await fetch("http://127.0.0.1:" + FRONT + "/", { headers: { "x-lane-user": user } })).text();
async function act(user, html, action, deviceId) {
  const form = new RegExp('name="csrf" value="([^"]+)"><input type="hidden" name="action" value="' + action + '">' + (deviceId ? '<input type="hidden" name="device_id" value="' + deviceId + '">' : "")).exec(html);
  if (!form) throw new Error("the NOVA page has no " + action + " button" + (deviceId ? " for " + deviceId : ""));
  const res = await fetch("http://127.0.0.1:" + FRONT + "/action", { method: "POST", redirect: "manual", headers: { "x-lane-user": user, "content-type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({ csrf: form[1], action, ...(deviceId ? { device_id: deviceId } : {}) }) });
  if (res.status !== 303) throw new Error(action + ": HTTP " + res.status);
}
const rowText = (html, id) => { const at = html.indexOf("device=" + id); return at < 0 ? "(no row)" : text(html.slice(html.lastIndexOf("<li", at), html.indexOf("</li>", at))); };
const text = (html) => html.replace(/<[^>]+>/g, " ").replace(/&[a-z#0-9]+;/g, (e) => ({ "&amp;": "&", "&quot;": '"', "&#39;": "'", "&lt;": "<", "&gt;": ">" })[e] ?? e).replace(/\s+/g, " ").trim();

// Pushes the fake phones received, and a tap on one of their questions.
let seen = 0;
async function newPushes() {
  const list = await (await fetch(PUSH + "/pushes")).json();
  const fresh = list.slice(seen);
  seen = list.length;
  return fresh.map((p) => ({ ...p, data: p.body.data ?? {}, question: (p.body.data?.actions ?? []).length > 0 }));
}
async function tap(push) {
  const res = await fetch(HA + "/api/webhook/" + push.body.registration_info.webhook_id, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ type: "fire_event", data: { event_type: "mobile_app_notification_action", event_data: { action: push.data.actions[0].action } } }) });
  if (!res.ok) throw new Error("tap -> HTTP " + res.status);
}

// The computers: one isolated home each, the CLI in a throwaway linux container.
const tmp = realpathSync(mkdtempSync(join(tmpdir(), "ha-nova-ask-count-")));
const linux = process.platform === "linux";
const host = linux ? "127.0.0.1" : "host.docker.internal";
const boxes = new Set();
let relayStarted = false, imageBuilt = false; // only what this run created is removed
process.on("exit", () => {
  for (const name of [...boxes, ...(relayStarted ? [RELAY] : [])]) spawnSync("docker", ["rm", "-f", name], { stdio: "ignore" });
  if (imageBuilt && process.env.ASK_COUNT_KEEP_IMAGE !== "1") spawnSync("docker", ["rmi", KEYRING], { stdio: "ignore" });
  rmSync(tmp, { recursive: true, force: true });
});
for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => process.exit(130));
let bin, n = 0;
function boxArgs(home, args, display, keyring = true) {
  const id = "ha-nova-testbed-ask-" + process.pid + "-" + n++;
  boxes.add(id);
  const env = { HOME: "/h", XDG_CONFIG_HOME: "/h/.config", XDG_CACHE_HOME: "/h/.cache", XDG_DATA_HOME: "/h/.local/share", HA_NOVA_ALLOW_INSECURE_TEST_KEYRING: "1", HA_NOVA_TEST_KEYRING_FILE: "/h/.test-relay-token", HA_NOVA_NO_BROWSER: "1", ...(display ? { DISPLAY: ":99" } : {}) };
  const net = linux ? ["--network", "host"] : ["--add-host=host.docker.internal:host-gateway"];
  return [id, ["run", "--rm", "--name", id, "--user", process.getuid() + ":" + process.getgid(), ...net, ...Object.entries(env).flatMap(([k, v]) => ["-e", k + "=" + v]),
    "-v", home + ":/h", "-v", join(tmp, "zenity") + ":/usr/bin/zenity:ro", "-v", bin + ":/usr/local/bin/ha-nova:ro", KEYRING,
    // A real Secret Service (gnome-keyring, its login keyring in the home): pairing keeps the device key there, as on a desktop. Without one (S5H), no session bus at all.
    ...(keyring ? ["dbus-run-session", "--", "sh", "-c", "printf lane | gnome-keyring-daemon --unlock --components=secrets >/dev/null 2>&1; exec ha-nova \"$@\"", "sh"] : ["ha-nova"]), ...args]];
}
function box(home, args, { display = true, keyring = true } = {}) {
  const [id, argv] = boxArgs(home, args, display, keyring);
  const child = spawn("docker", argv, { stdio: ["ignore", "pipe", "pipe"] });
  let stdout = "", stderr = "";
  child.stdout.on("data", (d) => (stdout += d));
  child.stderr.on("data", (d) => (stderr += d));
  return new Promise((resolve) => child.on("close", (code) => { boxes.delete(id); resolve({ code, stdout, stderr }); }));
}
const relayLog = () => { const l = spawnSync("docker", ["logs", "--tail", "40", RELAY], { encoding: "utf8" }); return (l.stdout + l.stderr).slice(-4000); };
const envelope = (out) => { try { return JSON.parse(out.split("\n").find((l) => l.startsWith("{")) ?? "{}"); } catch { return {}; } };
function computer(name) {
  const home = join(tmp, name);
  mkdirSync(join(home, ".config/ha-nova"), { recursive: true });
  writeFileSync(join(home, ".config/ha-nova/config.json"), JSON.stringify({ ha_url: "http://" + host + ":18123", relay_base_url: "http://" + host + ":" + BOOT }), { mode: 0o600 });
  return { name, home, display: true };
}
// The window: every dialog's arguments, and Confirm 2 s later when the lane says so and the window has a Confirm button (a waiting window's "Open in Home Assistant" approves nothing).
const RS = "\x1e", US = "\x1f";
const windows = (c) => existsSync(join(c.home, "zenity.log")) ? readFileSync(join(c.home, "zenity.log"), "utf8").split(RS).filter(Boolean).map((r) => { const a = r.split(US); return { args: a.slice(0, -1), pressed: a.at(-1) === "pressed" }; }) : [];

async function pair(c, admin) {
  await act(admin, await page(admin), "generate_code");
  const code = /<p class="code">([^<]+)<\/p>/.exec(await page(admin))?.[1]?.replace(/\D/g, "");
  if (!code) throw new Error("the NOVA page shows no pairing code");
  const before = new Set((await registry()).map((d) => d.id));
  const done = await box(c.home, ["pair", "--code", code], c);
  if (done.code !== 0) throw new Error("pair " + c.name + ": exit " + done.code + ": " + (done.stdout + done.stderr).slice(0, 400));
  c.id = (await registry()).find((d) => !before.has(d.id))?.id;
  if (!c.id) throw new Error("pair " + c.name + ": no new computer on the NOVA page");
}
// The NOVA page's computers: the device id in each one's Revoke link.
async function registry() {
  const html = await page(owner.id);
  return [...new Set([...html.matchAll(/device=([A-Za-z0-9_-]+)/g)].map((m) => m[1]))].map((id) => ({ id }));
}
async function linkPhone(c, platform, name) {
  writeFileSync(join(c.home, "zenity.answer"), "confirm"); // a window that asks about the link itself is the link's, not an action's
  await newPushes();
  const run = box(c.home, ["setup", "confirm-phone", name], c);
  let asked;
  for (let i = 0; i < 60 && !asked; i++, await sleep(500)) asked = (await newPushes()).find((p) => p.question && p.path.endsWith("/" + platform));
  if (!asked) throw new Error("linking " + name + " to " + c.name + ": no question on the phone");
  await tap(asked);
  const done = await run;
  if (done.code !== 0) throw new Error("confirm-phone " + c.name + ": exit " + done.code + ": " + (done.stdout + done.stderr).slice(0, 400));
}

// One action on one computer: plan, then apply, answering whatever asks.
// window: "confirm" (the row's window can confirm) or "wait".
async function measure(c, args, { window, phone }) {
  writeFileSync(join(c.home, "zenity.answer"), window);
  const shown = windows(c).length;
  await newPushes();
  const planned = envelope((await box(c.home, ["plan", ...args], c)).stdout);
  const id = planned.plan?.id;
  const out = { plan: planned, tier: planned.tier ?? planned.error?.code, preview: planned.tier === "preview" ? 1 : 0, code: 0, taps: 0, sound: 0, notes: 0, windows: [], buttons: [], say: planned.say ?? "" };
  if (!id) return { ...out, clicks: 0, stranded: 0, stages: out.preview, failed: "no plan: " + JSON.stringify(planned).slice(0, 300) };
  const apply = ["apply", "--plan", id];
  if (planned.plan?.code !== undefined) { out.code = 1; apply.splice(1, 0, "--confirm", String(planned.plan.code)); }
  // Every push with sound counts, a question or a note alike (quiet: iOS passive, as phone_text.go sends an update).
  const count = (p) => { if (!p.question) out.notes++; if (p.data.push?.["interruption-level"] !== "passive") out.sound++; };
  let finished = false;
  const run = box(c.home, apply, c).then((r) => { finished = true; return r; });
  for (const end = Date.now() + 120_000; !finished && Date.now() < end; await sleep(300)) {
    for (const p of await newPushes()) {
      count(p);
      if (p.question) out.buttons.push(p.data.actions[0]?.title ?? "");
      if (p.question && phone) { await tap(p); out.taps++; }
    }
  }
  const done = await run;
  // Late pushes still belong to this action: drain until 5 s pass without one (at most 20 s).
  for (let quiet = Date.now(), end = Date.now() + 20_000; Date.now() - quiet < 5_000 && Date.now() < end; await sleep(500)) {
    for (const p of await newPushes()) { count(p); quiet = Date.now(); }
  }
  out.windows = windows(c).slice(shown);
  out.clicks = out.windows.filter((w) => w.pressed).length;
  out.stranded = window === "confirm" ? out.windows.filter((w) => !w.pressed).length : 0; // an ask the row's window can't answer is one more ask
  out.offered = window === "wait" ? out.windows.filter((w) => w.args.includes("--ok-label=Confirm")).length : 0; // a Confirm this computer must not have
  out.applied = envelope(done.stdout);
  out.exit = done.code;
  out.stages = out.preview + out.code + out.clicks + out.taps + out.stranded;
  return out;
}

const results = [], words = [];
function record(row, column, m, want) {
  const p = [];
  if (m.failed) p.push(m.failed);
  else if (m.exit !== 0) p.push("apply exit " + m.exit + ": " + String(JSON.stringify(m.applied)).slice(0, 300));
  if (m.stages !== want.stages) p.push(m.stages + " stages, the table says " + want.stages);
  if (want.on && !(want.on === "window" ? m.clicks : m.taps)) p.push("not answered on " + want.on);
  if (want.silent && m.sound) p.push(m.sound + " phone push(es) with sound while the window answered");
  if (m.offered) p.push(m.offered + " window(s) with a Confirm button on a computer that must not confirm in its window");
  report(row + " " + column + ": " + m.stages + " ask(s)" + (m.stages ? " on " + [m.clicks && "the window", m.taps && "the phone", m.code && "a code", m.preview && "the chat (preview yes)", m.stranded && "a window that can't confirm"].filter(Boolean).join(" + ") : ""), p);
  results.push({ row, column, plan: (m.tier ?? "-") + " / " + (m.plan.plan?.confirm ?? "-"), stages: m.stages, how: [m.clicks && m.clicks + " window", m.taps && m.taps + " phone", m.code && "code", m.preview && "chat yes", m.stranded && m.stranded + " window that can't confirm"].filter(Boolean).join(" + ") || "-", sound: m.sound, notes: m.notes, windowsShown: m.windows.length });
  if (m.say) words.push([row + " " + column + ": say", m.say]);
  for (const w of m.windows) words.push([row + " " + column + ": window" + (w.pressed ? " (confirmed)" : " (waited)"), w.args.filter((a) => a.startsWith("--text=") || a.startsWith("--ok-label=")).map((a) => a.replace(/^--/, "")).join(" | ")]);
}

// The five columns, each a fresh target; returns what it measured.
async function columns(row, c, { window, phone, on, silent }) {
  const want = (stages) => ({ stages, on: stages ? on : undefined, silent });
  record(row, "everyday", await measure(c, ["call", "light.turn_on", "light.bed_light"], { window, phone }), want(0));
  record(row, "bigger change", await measure(c, ["call", "button.press", "button.modem_restart"], { window, phone }), { stages: 1, silent });
  await relock();
  record(row, "risky", await measure(c, ["call", "lock.unlock", "lock.front_door"], { window, phone }), want(1));
  const dash = "lane-ask-" + row.toLowerCase();
  await ws("lovelace/dashboards/create", { url_path: dash, title: "Lane " + row, mode: "storage" });
  created.dashboards.push(dash);
  record(row, "dashboard delete", await measure(c, ["delete", "--domain", "dashboard", "--id", dash], { window, phone }), want(1));
  const auto = "lane_ask_" + row.toLowerCase();
  created.automations.push(auto);
  await loadAutomations({ [auto]: { alias: "Lane ask " + row, triggers: [], conditions: [], actions: [] } });
  const entity = (await ha("GET", "/api/states")).find((s) => s.attributes?.id === auto)?.entity_id;
  record(row, "delete", await measure(c, ["delete", entity ?? "automation." + auto], { window, phone }), want(1));
}

let owner, owned = false; // the bed is this tree's (fixture checked it); only then is anything cleaned up
const created = { dashboards: [], automations: [] }; // what this run made, and only that is removed
try {
  if (Number(execFileSync("df", ["-k", tmpdir()], { encoding: "utf8" }).trim().split("\n").at(-1).split(/\s+/)[3]) < 12 * 1024 * 1024) throw new Error("less than 12 GB free disk");
  await fixture(); // the fake phones, their users, and the push service
  owned = true;
  const fx = JSON.parse(readFileSync(join(HERE, ".state/phone.json"), "utf8"));
  const users = await ws("config/auth/list");
  owner = users.find((u) => u.is_owner);
  const goarch = { aarch64: "arm64", x86_64: "amd64" }[spawnSync("docker", ["info", "--format", "{{.Architecture}}"], { encoding: "utf8" }).stdout?.trim()];
  bin = process.env.TESTBED_CLI_BIN_LINUX ?? join(tmp, "ha-nova");
  if (!process.env.TESTBED_CLI_BIN_LINUX) execFileSync("go", ["build", "-o", bin, "."], { cwd: join(ROOT, "cli"), env: { ...process.env, CGO_ENABLED: "0", GOOS: "linux", GOARCH: goarch }, stdio: ["ignore", "ignore", "inherit"] });
  execFileSync("docker", ["build", "-q", "-t", KEYRING, "--build-arg", "LANE_UID=" + process.getuid(), "-"], { stdio: ["pipe", "ignore", "inherit"],
    input: "FROM ubuntu:24.04\nARG LANE_UID\nRUN apt-get update && apt-get install -y --no-install-recommends dbus gnome-keyring && rm -rf /var/lib/apt/lists/* && (getent passwd $LANE_UID >/dev/null || useradd -M -d /h -u $LANE_UID lane)\n" });
  imageBuilt = true;
  writeFileSync(join(tmp, "zenity"), "#!/bin/sh\n{ for a in \"$@\"; do printf '%s\\037' \"$a\"; done; } >> /h/zenity.log\nif [ \"$(cat /h/zenity.answer 2>/dev/null)\" = confirm ] && printf '%s\\n' \"$@\" | grep -qx -- --ok-label=Confirm; then sleep 2; printf 'pressed\\036' >> /h/zenity.log; exit 0; fi\nprintf 'waited\\036' >> /h/zenity.log\nexec sleep 600\n");
  chmodSync(join(tmp, "zenity"), 0o755);

  // The App: the bed's Relay image, its /data in memory (a bind mount can't hold the worker's socket on macOS).
  const image = execFileSync("docker", ["inspect", "-f", "{{.Image}}", "ha-nova-testbed-relay"], { encoding: "utf8" }).trim();
  const network = execFileSync("docker", ["inspect", "-f", "{{range $k, $v := .NetworkSettings.Networks}}{{$k}}{{end}}", "ha-nova-testbed-relay"], { encoding: "utf8" }).trim();
  // A bed with file access (TESTBED_FILES=1) gives the App the same /config to read: its YAML thermostat (up.sh) is read there, as in a home with the App's file access on (MAR-245).
  const files = existsSync(join(HERE, ".state/files")) ? ["-v", join(HERE, ".state/config") + ":/config:ro", "-e", "FILE_ACCESS=read"] : [];
  execFileSync("docker", ["run", "-d", "--name", RELAY, "--network", network, "--user", process.getuid() + ":" + process.getgid(), ...files,
    "-e", "SUPERVISOR_TOKEN=" + fx.relay_llat, "-e", "HA_URL=http://homeassistant:8123", "-e", "LANE_SECURE_HOST_PORT=" + SECURE,
    "--tmpfs", "/data:rw,uid=" + process.getuid() + ",gid=" + process.getgid() + ",mode=0700", "-v", join(HERE, "app-relay.mjs") + ":/lane/app-relay.mjs:ro",
    "-p", "127.0.0.1:" + BOOT + ":8791", "-p", "127.0.0.1:" + SECURE + ":8792", "-p", "127.0.0.1:" + FRONT + ":8794", image, "node", "/lane/app-relay.mjs"], { stdio: ["ignore", "ignore", "inherit"] });
  relayStarted = true;
  let up = false;
  for (let i = 0; i < 60 && !up; i++, await sleep(1000)) up = await page(owner.id).then((h) => h.includes("Connect a device")).catch(() => false);
  if (!up) throw new Error("the App never served the NOVA page: " + relayLog());

  // S2, S1, S5, S3: one computer the owner declares on the NOVA page.
  const own = computer("own");
  await pair(own, owner.id);
  let html = "";
  for (let i = 0; i < 30 && !rowText(html, own.id).includes("one click"); i++, await sleep(1000)) html = await page(owner.id); // its words come from the worker
  if (!rowText(html, own.id).includes("one click")) throw new Error("the NOVA page shows no window line for the new computer: " + rowText(html, own.id) + "\n" + relayLog());
  words.push(["NOVA page row, before the owner turns it on", rowText(html, own.id)]);
  await act(owner.id, html, "window_mine", own.id);
  html = await page(owner.id);
  words.push(["NOVA page row, on", rowText(html, own.id)]);
  await columns("S2", own, { window: "confirm", phone: false, on: "window", silent: true });
  await linkPhone(own, "ios", fx.phones.find((p) => p.platform === "ios").name);
  await columns("S1", own, { window: "confirm", phone: true, on: "window", silent: true });
  // M on S1: beside the watchdog, without and with a logbook open, applied where mrow.mjs stages it.
  const sync = (...args) => envelope(spawnSync("docker", boxArgs(own.home, args, true)[1], { encoding: "utf8" }).stdout ?? "");
  await mRowChecks({ ha, ws, cli: sync, loadAutomations, gated: false, check: (name, ok, detail) => report(name, ok ? [] : [detail]),
    measure: async (stage) => {
      const row = "M (" + stage + ")", at = { window: "confirm", phone: true };
      record(row, "everyday", await measure(own, ["call", "light.turn_off", "light.bed_light"], at), { stages: 0, silent: true });
      record(row, "shutter", await measure(own, ["call", "cover.close_cover", "cover.kitchen_window"], at), { stages: 1, on: "window", silent: true });
      record(row, "garage", await measure(own, ["call", "cover.open_cover", "cover.garage_door"], at), { stages: 1, on: "window", silent: true });
    } });
  own.display = false;
  await columns("S5", own, { window: "confirm", phone: true, on: "phone" });
  own.display = true;
  await act(owner.id, await page(owner.id), "window_off", own.id);
  await columns("S3", own, { window: "wait", phone: true, on: "phone" });

  // S4: a computer nobody declared (here a second administrator's), its own phone.
  const other = computer("other");
  await pair(other, fx.tester_user);
  await linkPhone(other, "android", fx.phones.find((p) => p.platform === "android").name);
  await columns("S4", other, { window: "wait", phone: true, on: "phone" });
  await headless(fx);
} catch (err) {
  report("ask count ran to the end", [err.stack ?? err.message]);
} finally {
  if (owned) {
    await ws("lovelace/dashboards/list").then((all) => Promise.all(all.filter((d) => created.dashboards.includes(d.url_path)).map((d) => ws("lovelace/dashboards/delete", { dashboard_id: d.id })))).catch(() => {});
    for (const id of created.automations) await ha("DELETE", "/api/config/automation/config/" + id).catch(() => {});
    await relock().then((state) => report("the bed's lock is locked again", state === "locked" ? [] : ["lock " + state]), (err) => report("the bed's lock is locked again", [err.message]));
  }
}

console.log("\nAsk count (stages a person answers per action; 0.32 targets: everyday 0, bigger change 1, risky / dashboard delete / delete 1)\n");
console.log("| Row | Column | Plan (tier / confirm) | Asks | Where | Phone pushes with sound | Notes | Windows shown |\n|---|---|---|---|---|---|---|---|");
for (const r of results) console.log("| " + [r.row, r.column, r.plan, r.stages, r.how, r.sound, r.notes, r.windowsShown].join(" | ") + " |");
console.log("\nS5H: the code column counts the one code the person types back; the other S5H checks print above as PASS or FAIL.");
console.log("\nNot staged here: S1L (Home Assistant Link transport; Relay side in tests/security/pairing-window.test.ts), S6-S8 (chat apps, unchanged in 0.32: the chat acceptance lane and the maintainer's client lane for permission prompts), a computer paired before 0.32 (tests/security/window-confirm.test.ts).");
console.log("\nWords, verbatim:\n");
for (const key of ["setup.own.no", "setup.own.prompt", "setup.own.question", "setup.page.found.own_hint", "setup.page.found.own"]) words.unshift(["setup (" + (key.startsWith("setup.page") ? "page" : "terminal") + ", catalog) " + key, setupText(key)]);
for (const [where, said] of words) console.log("- " + where + ": " + said.replace(/\n+/g, " / "));
process.exit(failures() ? 1 : 0);

// S5H (MAR-244): a bot's host, with no display and no Secret Service, paired
// with a code. Pairing puts its key in a private file; without a phone or a
// code the plan names the NOVA page and never a linked phone; with the owner's
// chat code apply shows the App's code at once, a wrong one keeps the step, the
// right one sends it; a linked phone then ends the code and the phone asks.
async function headless(fx) {
  const bot = { ...computer("bot"), display: false, keyring: false };
  await pair(bot, owner.id);
  const key = join(bot.home, ".config/ha-nova/secrets/ha-nova.device-credential");
  report("S5H: pairing without a Secret Service keeps the key in a private file (0600)", existsSync(key) && (statSync(key).mode & 0o777) === 0o600 ? [] : [existsSync(key) ? "mode " + (statSync(key).mode & 0o777).toString(8) : "no private key file after pairing"]);
  record("S5H", "everyday", await measure(bot, ["call", "light.turn_on", "light.bed_light"], { window: "wait", phone: false }), { stages: 0 });
  await relock();
  const unlock = ["plan", "call", "lock.unlock", "lock.front_door"], cli = async (args) => envelope((await box(bot.home, args, bot)).stdout);
  const none = await cli(unlock);
  report("S5H: without a phone or a code the plan names the NOVA page and confirm-phone, never a linked phone", /NOVA page/.test(none.say ?? "") && /confirm-phone/.test(none.say ?? "") && !/if one is linked/.test(none.say ?? "") ? [] : [JSON.stringify(none).slice(0, 400)]);
  words.push(["S5H no phone: plan say", none.say ?? ""]);
  await act(owner.id, await page(owner.id), "code_on", bot.id);
  words.push(["S5H NOVA page row, code on", rowText(await page(owner.id), bot.id)]);
  const planned = await cli(unlock), id = planned.plan?.id ?? "-";
  const asked = Date.now(), shown = await cli(["apply", "--plan", id]), seconds = (Date.now() - asked) / 1000, code = /\b(\d{4})\b/.exec(shown.say ?? "")?.[1];
  const before = (await ha("GET", "/api/states/lock.front_door")).state;
  const wrong = await cli(["apply", "--confirm", code === "0000" ? "1111" : "0000", "--plan", id]), held = (await ha("GET", "/api/states/lock.front_door")).state;
  const right = await cli(["apply", "--confirm", code ?? "", "--plan", id]), after = (await ha("GET", "/api/states/lock.front_door")).state;
  report("S5H code: apply shows the App's code at once, a wrong one keeps the step, the right one sends it", [
    shown.error?.code !== "CONFIRM_CODE" || !code ? "no code shown: " + JSON.stringify(shown).slice(0, 300).replace(/\b\d{4}\b/g, "####") : "",
    seconds > 20 || before !== "locked" ? "apply took " + seconds.toFixed(1) + " s to show the code (it must not wait), lock " + before : "",
    wrong.error?.code !== "CODE_MISMATCH" || held !== "locked" ? "wrong code: " + wrong.error?.code + ", lock " + held : "",
    right.status !== "verified" || after === "locked" ? "right code: " + JSON.stringify(right).slice(0, 300) + ", lock " + after : ""].filter(Boolean));
  results.push({ row: "S5H", column: "risky (chat code)", plan: (planned.tier ?? "-") + " / " + (planned.plan?.confirm ?? "-"), stages: 1, how: "code", sound: 0, notes: 0, windowsShown: 0 });
  words.push(["S5H code: plan say", planned.say ?? ""], ["S5H code: apply say (digits hidden)", (shown.say ?? "").replace(/\b\d{4}\b/g, "####")], ["S5H code: wrong code", wrong.say ?? wrong.error?.message ?? ""]);
  await relock();
  await linkPhone(bot, "ios", fx.phones.find((p) => p.platform === "ios").name);
  const row = rowText(await page(owner.id), bot.id);
  report("S5H: a linked phone ends the chat code", row.includes("four-digit code in the AI chat") ? [row] : []);
  const tapped = await measure(bot, ["call", "lock.unlock", "lock.front_door"], { window: "wait", phone: true });
  record("S5H", "risky (phone)", tapped, { stages: 1, on: "phone" });
  report("S5H: the phone's button names the action", tapped.buttons.length && tapped.buttons.every((b) => /unlock/i.test(b)) ? [] : ["buttons " + JSON.stringify(tapped.buttons)]);
  words.push(["S5H phone: button", tapped.buttons.join(" | ")]);
}

function setupText(key) {
  for (const file of ["cli/setupweb/strings.en.json"]) {
    const path = join(ROOT, file);
    if (existsSync(path)) { const v = JSON.parse(readFileSync(path, "utf8"))[key]; if (v !== undefined) return JSON.stringify(v); }
  }
  return "(not found: " + key + ")";
}
