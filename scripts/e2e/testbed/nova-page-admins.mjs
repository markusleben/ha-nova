#!/usr/bin/env -S npx tsx
// NOVA page for administrators (docs/work/2026-11-12-nova-page-admins-spec.md)
// with the test bed's real Home Assistant users: it creates an administrator
// and a plain user, then runs the NOVA page's handlers with the users exactly
// as config/auth/list returns them. The bed has no Supervisor, so the ingress
// identity is simulated (peer and header, as tests/http do); the role check
// reads the real list on every request. Removes both users at the end.
//
//   npm run testbed:up
//   npx tsx scripts/e2e/testbed/nova-page-admins.mjs
//
// Prints PASS/FAIL lines; exits non-zero on any FAIL.
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { createNovaActionHandler, createNovaPageHandler } from "../../../nova/src/http/handlers/nova-page.ts";
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
  await call("config/auth/update", { user_id: admin.user.id, group_ids: ["system-users"] });
  const demoted = await page(admin.user.id);
  report("an administrator made a plain user is refused on the next request", demoted.statusCode === 403 ? [] : [`HTTP ${demoted.statusCode}`]);
} finally {
  for (const id of made) await call("config/auth/delete", { user_id: id }).catch((error) => console.log(`WARN  could not remove lane user ${id}: ${error.message}`));
  ws.close();
  rmSync(dir, { recursive: true, force: true });
}
process.exit(failed > 0 ? 1 : 0);
