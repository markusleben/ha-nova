#!/usr/bin/env -S npx tsx
// NOVA page for administrators (docs/work/2026-11-12-nova-page-admins-spec.md)
// with the test bed's real Home Assistant users: it creates an administrator
// and a plain user, then runs the NOVA page's handlers with the users exactly
// as config/auth/list returns them. The bed has no Supervisor, so the ingress
// identity is simulated (peer and header, as tests/http do); the role check
// reads the real list on every request. Then the gate's part of the page with
// a real ask table on a lane clock: five unanswered asks pause the phone (a
// computer's own cancels don't count), a real administrator ends the pause
// with the page's POST, and a plain user can't; the approval view names the
// computer; the live poll answers administrators only. Removes both users at the end.
//
//   npm run testbed:up
//   npx tsx scripts/e2e/testbed/nova-page-admins.mjs
//
// Prints PASS/FAIL lines; exits non-zero on any FAIL.
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { createGateAsks } from "../../../nova/src/http/gate-asks.ts";
import { createApprovePageHandler } from "../../../nova/src/http/handlers/nova-approve.ts";
import { createLiveHandler } from "../../../nova/src/http/handlers/nova-page-live.ts";
import { createNovaActionHandler, createNovaPageHandler } from "../../../nova/src/http/handlers/nova-page.ts";
import { PAGE_KEYS, pageTexts } from "../../../nova/src/http/page-texts.ts";
import { createCsrfStore } from "../../../nova/src/security/csrf.ts";
import { openDeviceRegistry } from "../../../nova/src/security/device-registry.ts";
import { opaqueReady } from "../../../nova/src/security/opaque-server.ts";
import { createPairingV1Manager } from "../../../nova/src/security/pairing-v1.ts";

const STATE = join(dirname(fileURLToPath(import.meta.url)), ".state");
const token = readFileSync(join(STATE, "ha-llat"), "utf8").trim();
let failed = 0;
const report = (name, problems) => {
  if (problems.length) failed++;
  console.log(problems.length ? `FAIL  ${name}: ${problems.join("; ")}` : `PASS  ${name}`);
};

// One WS connection to the bed's Home Assistant (127.0.0.1 only).
const ws = new WebSocket("ws://127.0.0.1:18123/api/websocket");
let next = 1;
const pending = new Map();
ws.onmessage = (event) => {
  const msg = JSON.parse(String(event.data));
  if (msg.type === "auth_required") ws.send(JSON.stringify({ type: "auth", access_token: token }));
  if (msg.type === "auth_ok") pending.get(0)?.resolve();
  if (msg.type === "auth_invalid") pending.get(0)?.reject(new Error("auth_invalid"));
  if (msg.type === "result") {
    const p = pending.get(msg.id);
    pending.delete(msg.id);
    msg.success ? p?.resolve(msg.result) : p?.reject(new Error(JSON.stringify(msg.error)));
  }
};
await new Promise((resolve, reject) => pending.set(0, { resolve, reject }));
const call = (type, data = {}) => new Promise((resolve, reject) => {
  const id = next++;
  pending.set(id, { resolve, reject });
  ws.send(JSON.stringify({ id, type, ...data }));
});

const made = [];
const dir = mkdtempSync(join(tmpdir(), "ha-nova-page-admins-"));
try {
  await opaqueReady();
  const admin = await call("config/auth/create", { name: "NOVA lane admin", group_ids: ["system-admin"] });
  made.push(admin.user.id);
  const plain = await call("config/auth/create", { name: "NOVA lane user", group_ids: ["system-users"] });
  made.push(plain.user.id);
  const users = await call("config/auth/list");
  const owner = users.find((u) => u.is_owner === true);

  const now = () => Date.now();
  const registry = openDeviceRegistry(dir);
  const pairing = createPairingV1Manager({ registry, secureEndpoint: () => ({ spkiPin: "p", securePort: 8792 }), now });
  const csrf = createCsrfStore();
  const chatCalls = [];
  const chatApps = new Proxy({}, { get: (_, name) => () => { chatCalls.push(String(name)); if (name === "bridgeDeviceId") return null; throw new Error(String(name)); } });
  const deps = {
    fetchAuthUsers: () => call("config/auth/list"), // fresh on every request, as the App reads it
    csrf, pairing, registry, now, chatApps,
    connection: () => ({ haConnected: true }),
    update: async () => ({ version: "lane", versionLatest: "lane", updateAvailable: false, error: false }),
    relayVersion: "lane",
  };
  const run = async (handler, userId, body = null, url = "/") => {
    const headers = { "x-ingress-path": "/api/hassio_ingress/lane", "x-remote-user-id": userId };
    const request = { url, socket: { remoteAddress: "172.30.32.2" }, headers, rawHeaders: Object.entries(headers).flat() };
    const r = { statusCode: 0, body: "", headers: {}, setHeader(k, v) { this.headers[k.toLowerCase()] = v; }, end(b) { if (b) this.body = b; } };
    await handler(deps)({ request, response: r, path: "/home", body });
    return r;
  };
  const page = (userId) => run(createNovaPageHandler, userId);
  const act = (userId, form) => run(createNovaActionHandler, userId, form);

  const ownerPage = await page(owner.id);
  report("the owner opens NOVA", ownerPage.statusCode === 200 && ownerPage.body.includes("Connect a device") ? [] : [`HTTP ${ownerPage.statusCode}`]);
  chatCalls.length = 0; // the owner's page reads chat apps; the administrator's must not
  const adminPage = await page(admin.user.id);
  report("a real administrator opens NOVA, without chat apps", adminPage.statusCode === 200 && adminPage.body.includes("Connect a device") && adminPage.body.includes("only the owner") && !chatCalls.some((c) => c !== "bridgeDeviceId")
    ? [] : [`HTTP ${adminPage.statusCode}, chat calls ${chatCalls}`]);
  const code = /name="csrf" value="([^"]+)"><input type="hidden" name="action" value="generate_code"/.exec(adminPage.body)?.[1];
  const made_code = await act(admin.user.id, { action: "generate_code", csrf: code ?? "" });
  report("the administrator makes a code", made_code.statusCode === 303 && pairing.getStatus().phase === "active" ? [] : [`HTTP ${made_code.statusCode}, phase ${pairing.getStatus().phase}`]);
  const chat = await act(admin.user.id, { action: "chat_apps_on", csrf: csrf.issue(admin.user.id, "chat_apps_on", now()) });
  report("the administrator can't turn chat apps on", chat.statusCode === 403 && chat.body.includes("Nothing was changed") && !chatCalls.includes("turnOn") ? [] : [`HTTP ${chat.statusCode}`]);
  for (const handler of [createNovaPageHandler, createNovaActionHandler]) {
    const r = await run(handler, plain.user.id, { action: "generate_code", csrf: csrf.issue(plain.user.id, "generate_code", now()) });
    report(`a real plain user is refused (${handler === createNovaPageHandler ? "page" : "action"})`, r.statusCode === 403 && r.body.includes("Only Home Assistant administrators") && !r.body.includes("<form") ? [] : [`HTTP ${r.statusCode}`]);
  }

  // The gate's part: a real ask table, a phone that never answers, a lane clock.
  let clock = Date.now();
  const strings = JSON.parse(readFileSync(new URL("../../../cli/confirm_strings.en.json", import.meta.url), "utf8"));
  const laneTexts = async (name) => ({ lang: "en", texts: Object.fromEntries(PAGE_KEYS.map((k) => [k, String(strings[k]).replace("{name}", `“${name}”`)])) });
  const asks = createGateAsks({
    ask: async () => ({ verdict: "typed", lang: "en", title: "Unlock?", body: "Unlock “Lane door” (lock.lane). Requested by “Lane Mac” through HA NOVA.", from: "Requested by “Lane Mac” through HA NOVA.", button: "Unlock", items: [{ verdict: "typed", text: "Unlock “Lane door” (lock.lane)", entity_ids: ["lock.lane"] }] }),
    prepare: {}, deviceName: () => "Lane Mac", channels: () => ["page"],
    link: () => ({ service: "mobile_app_lane", device_id: "lane-phone", user_id: owner.id }),
    phone: (_l, _t, _ms, stop) => new Promise((resolve) => stop.addEventListener("abort", () => resolve(null))),
    now: () => clock, pollMs: 5,
  });
  const laneMac = { kind: "device", deviceId: "lane-mac" };
  const askFrom = async () => await asks.handler({ request: {}, response: {}, path: "/gate/v1/ask", body: { requests: [{ endpoint: "core", body_b64: Buffer.from("{}").toString("base64") }] }, principal: laneMac });
  const opCancel = async (askId) => await asks.handler({ request: {}, response: {}, path: "/gate/v1/ask", body: { ask_id: askId, op: "cancel" }, principal: laneMac });
  const gatePage = { ...asks, texts: pageTexts(laneTexts, () => clock) };
  const gateDeps = { ...deps, now: () => clock, registry: { list: () => [{ deviceId: "lane-mac", name: "Lane Mac", state: "active", platform: "darwin", client: "lane", createdAtMs: clock, clientInstallId: "lane" }] }, gatePage, page: gatePage, panel: async () => "/app/lane_nova", relayUserId: async () => "lane-relay" };
  const runGate = async (handler, userId, body = null, url = "/") => {
    const headers = { "x-ingress-path": "/api/hassio_ingress/lane", "x-remote-user-id": userId };
    const request = { url, socket: { remoteAddress: "172.30.32.2" }, headers, rawHeaders: Object.entries(headers).flat() };
    const r = { statusCode: 0, body: "", headers: {}, setHeader(k, v) { this.headers[k.toLowerCase()] = v; }, end(b) { if (b) this.body = b; } };
    await handler(gateDeps)({ request, response: r, path: url, body });
    return r;
  };
  for (let i = 0; i < 5; i++) {
    const { ask_id } = await askFrom();
    await opCancel(ask_id); // the computer's own cancel at once: neither counts nor resets
  }
  const notPaused = asks.pausedDevices().length === 0;
  for (let i = 0; i < 4; i++) {
    await askFrom();
    clock += 181_000;
    asks.waiting(); // expires it
  }
  const { ask_id: stopped } = await askFrom();
  clock += 90_000;
  await opCancel(stopped); // the CLI stops waiting at 90 s, unanswered: the fifth
  const paused = asks.pausedDevices();
  report("five unanswered asks pause the phone (one ended by the computer at 90 s); its quick cancels don't", notPaused && paused.length === 1 && paused[0].misses.length === 5 ? [] : [`cancels paused: ${!notPaused}, paused ${JSON.stringify(paused)}`]);
  const adminGate = await runGate(createNovaPageHandler, admin.user.id);
  const resumeToken = /name="csrf" value="([^"]+)"><input type="hidden" name="action" value="resume_phone_asks"><input type="hidden" name="device_id" value="lane-mac">/.exec(adminGate.body)?.[1];
  const refusedResume = await runGate(createNovaActionHandler, plain.user.id, { action: "resume_phone_asks", csrf: csrf.issue(plain.user.id, "resume_phone_asks:lane-mac", clock), device_id: "lane-mac" });
  const resumed = await runGate(createNovaActionHandler, admin.user.id, { action: "resume_phone_asks", csrf: resumeToken ?? "", device_id: "lane-mac" });
  report("a real administrator ends the pause from the NOVA page; a plain user can't", adminGate.body.includes("Phone asks paused") && refusedResume.statusCode === 403 && resumed.statusCode === 303 && asks.pausedDevices().length === 0
    ? [] : [`card ${adminGate.body.includes("Phone asks paused")}, plain HTTP ${refusedResume.statusCode}, admin HTTP ${resumed.statusCode}`]);
  clock += 3_600_000; // a new hour: ten asks so far
  await askFrom();
  const view = await runGate(createApprovePageHandler, admin.user.id, null, "/approve");
  report("the approval view names the computer in the worker's words, shows the ask once, and links back to the NOVA panel", view.statusCode === 200 && view.body.includes("Requested by “Lane Mac” through HA NOVA.") && view.body.split("Unlock “Lane door” (lock.lane)").length === 2 && view.body.includes('<a href="/app/lane_nova">Back to NOVA</a>') && !view.body.includes('http-equiv="refresh"')
    ? [] : [`HTTP ${view.statusCode}`]);
  const live = async (userId) => await runGate(() => createLiveHandler({ fetchAuthUsers: deps.fetchAuthUsers, page: gatePage }), userId, null, "/live");
  const before = await live(admin.user.id);
  const { ask_id: last } = await askFrom(); // replaces the open one: a new ref
  const after = await live(admin.user.id);
  await opCancel(last);
  const plainLive = await live(plain.user.id);
  report("the live poll answers administrators only, and changes with the asks", before.statusCode === 200 && JSON.parse(before.body).state !== JSON.parse(after.body).state && plainLive.statusCode === 403
    ? [] : [`admin HTTP ${before.statusCode}, plain HTTP ${plainLive.statusCode}`]);
  const empty = await runGate(createApprovePageHandler, admin.user.id, null, "/approve");
  report("with nothing left to answer, the approval view goes back to the NOVA panel on its own", empty.statusCode === 200 && empty.body.includes('<meta http-equiv="refresh" content="4;url=/app/lane_nova">') && empty.body.includes("Back to NOVA in a few seconds.")
    ? [] : [`HTTP ${empty.statusCode}`]);
  await call("config/auth/update", { user_id: admin.user.id, group_ids: ["system-users"] });
  const demoted = await page(admin.user.id);
  report("an administrator made a plain user is refused on the next request", demoted.statusCode === 403 ? [] : [`HTTP ${demoted.statusCode}`]);
} finally {
  for (const id of made) await call("config/auth/delete", { user_id: id }).catch((error) => console.log(`WARN  could not remove lane user ${id}: ${error.message}`));
  ws.close();
  rmSync(dir, { recursive: true, force: true });
}
process.exit(failed > 0 ? 1 : 0);
