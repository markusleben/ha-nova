#!/usr/bin/env node
// The NOVA App update notice with the CLI's own route to the App (0.29.0-rc1
// escape: over Home Assistant Link, `ha-nova update` said nothing about a
// waiting App update). The bed has no Supervisor, so no App update entity:
// a proxy in front of the bed's Relay adds one, as Home Assistant lists it
// for the NOVA App (update.nova_relay_update, platform hassio), one patch
// above the running App. `ha-nova check-update` must name that update
// through the proxy, and must not without it. This is the home route: no
// lane reaches the App over Link (docs/releasing.md, Cloud checklist
// app_notice).
//
//   node scripts/e2e/testbed/app-notice.mjs      (a running bed; TESTBED_CLI_BIN = another build)
//
// The CLI runs in a new home in .state. Prints PASS/FAIL lines; exits
// non-zero on any FAIL.
import { spawn } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import http from "node:http";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const STATE = join(HERE, ".state");
const RELAY = "http://127.0.0.1:18791";
const TOKEN = readFileSync(join(STATE, "relay-token"), "utf8").trim();
let failed = 0;
const check = (name, ok, detail) => {
  if (!ok) failed++;
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? ` (${detail})` : ""}`);
};

const running = (await (await fetch(`${RELAY}/health`, { headers: { authorization: `Bearer ${TOKEN}` } })).json()).data.version;
const latest = running.replace(/\d+$/, (n) => String(Number(n) + 1));
const ENTITY = "update.nova_relay_update";
const proxy = http.createServer(async (req, res) => {
  const chunks = [];
  for await (const c of req) chunks.push(c);
  const body = Buffer.concat(chunks).toString();
  const headers = { ...req.headers };
  delete headers.host;
  delete headers["content-length"];
  const upstream = await fetch(`${RELAY}${req.url}`, { method: req.method, headers, body: ["GET", "HEAD"].includes(req.method) ? undefined : body });
  let text = await upstream.text();
  const sent = (() => { try { return JSON.parse(body); } catch { return {}; } })();
  const reply = (() => { try { return JSON.parse(text); } catch { return null; } })();
  if (reply?.ok && req.url === "/core" && sent.method === "GET" && sent.path === "/api/states") {
    reply.data.body.push({ entity_id: ENTITY, state: "on", attributes: { title: "NOVA Relay", installed_version: running, latest_version: latest, supported_features: 17, in_progress: false } });
    text = JSON.stringify(reply);
  } else if (reply?.ok && req.url === "/ws" && sent.type === "config/entity_registry/list") {
    reply.data.push({ entity_id: ENTITY, platform: "hassio", unique_id: "2368fcfa_ha_nova_relay_version_latest" });
    text = JSON.stringify(reply);
  }
  const out = {};
  upstream.headers.forEach((v, k) => { if (!["content-length", "content-encoding", "transfer-encoding"].includes(k)) out[k] = v; });
  res.writeHead(upstream.status, out).end(text);
}).listen(0, "127.0.0.1");
await new Promise((r) => proxy.once("listening", r));

function checkUpdate(relayURL) {
  const home = join(STATE, `app-notice-${Date.now()}`);
  mkdirSync(join(home, ".config/ha-nova"), { recursive: true });
  writeFileSync(join(home, ".config/ha-nova/config.json"), JSON.stringify({ ha_url: "http://127.0.0.1:18123", relay_base_url: relayURL }));
  writeFileSync(join(home, ".test-relay-token"), TOKEN, { mode: 0o600 });
  // Not spawnSync: the proxy answers in this process while the CLI runs.
  const child = spawn("bash", [join(HERE, "cli.sh"), "check-update"], { env: { ...process.env, TESTBED_CLI_HOME: home } });
  let out = "";
  for (const stream of [child.stdout, child.stderr]) stream.on("data", (c) => (out += c));
  return new Promise((done) => child.on("close", (code) => done({ out, code })));
}
const say = (out) => out.split("\n").find((l) => l.includes("NOVA App")) ?? `no NOVA App line: ${out.trim().split("\n").slice(-2).join(" / ").slice(0, 200)}`;
try {
  const want = `The NOVA App in Home Assistant has an update: ${running} → ${latest}.`;
  const { out, code } = await checkUpdate(`http://127.0.0.1:${proxy.address().port}`);
  check("check-update names a waiting App update", code === 0 && out.includes(want) && out.includes("Settings → Apps → NOVA Relay → Update"), `exit ${code}: ${say(out)}`);
  const none = await checkUpdate(RELAY);
  check("check-update names none when none waits", none.code === 0 && !/NOVA App in Home Assistant (has|needs) an update/.test(none.out), `exit ${none.code}: ${say(none.out)}`);
} catch (err) {
  check("App notice check ran", false, err.message);
} finally {
  proxy.close();
}
process.exit(failed ? 1 : 0);
