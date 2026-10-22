#!/usr/bin/env node
// Hosted demo: Add to Claude (docs/work/2026-10-20-demo-add-to-claude-spec.md).
// Home Assistant's ha_nova integration (/api/ha_nova/mcp) passes chat-app
// requests to the bridge container (`ha-nova serve`). Driven by reset.sh and
// guard.sh on the demo host. No dependencies.
//
//   node chat.mjs entry <secret>                      prints the seeded .storage/core.config_entries
//                                                     (before Home Assistant's first boot)
//   node chat.mjs files <dir> <secret> <relay_token> <origin>
//                                                     the bridge's files: secret, demo profile,
//                                                     chat-apps.json letting only the demo user in, plans-since
//   node chat.mjs gate  <ha_url> <owner_llat> <origin>   check, then sign in as demo the way Claude
//                                                        does (its published identity, PKCE), tools work
//   node chat.mjs check <ha_url> <owner_llat> <origin>   minute check: external_url (repaired), entry
//                                                        loaded, the owner is refused
//   node chat.mjs keep    <state_dir> <carry_file>      Home Assistant stopped: the demo user's chat-app
//                                                       sign-ins used in the last 24 h, into carry_file
//   node chat.mjs restore <carry_file> <state_dir>      Home Assistant stopped: adds them, bound to this
//                                                       build's demo user; prints how many

import { createHash, randomBytes } from "node:crypto";
import { closeSync, constants, fstatSync, lstatSync, mkdirSync, openSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";

// Claude's published client identity (CIMD) and its callback. Same host, so
// Home Assistant accepts the callback without fetching anything (the house
// has no egress).
const CLAUDE_CLIENT = "https://claude.ai/oauth/mcp-oauth-client-metadata";
const CLAUDE_CALLBACK = "https://claude.ai/api/mcp/auth_callback";
const BRIDGE = { host: "bridge", port: 8794 }; // docker-compose.yml
const IDS_FILE = new URL("../../e2e/testbed/.state/demo-users.json", import.meta.url); // demo.mjs setup
const CALL_MS = 30_000;

const [cmd, ...args] = process.argv.slice(2);
const hex64 = (s) => /^[0-9a-f]{64}$/.test(s ?? "");

// A hassio-source entry as the NOVA App's discovery would create it; the
// bridge address and the secret of this reset.
function entry([secret]) {
  if (!hex64(secret)) throw new Error("usage: chat.mjs entry <64-hex secret>");
  const now = new Date().toISOString();
  console.log(JSON.stringify({
    version: 1, minor_version: 5, key: "core.config_entries",
    data: { entries: [{
      entry_id: randomBytes(16).toString("hex"), version: 1, minor_version: 1, domain: "ha_nova", title: "HA NOVA",
      data: { ...BRIDGE, secret, slug: "ha_nova_demo" }, options: {}, pref_disable_new_entities: false,
      pref_disable_polling: false, source: "hassio", unique_id: "ha-nova-demo", disabled_by: null,
      created_at: now, modified_at: now, discovery_keys: {}, subentries: [],
    }] },
  }));
}

function files([dir, secret, relayToken, origin]) {
  if (!dir || !hex64(secret) || !/^[0-9a-f]{64}$/.test(relayToken ?? "") || !/^https?:\/\/[a-z0-9.:-]+$/.test(origin ?? "")) {
    throw new Error("usage: chat.mjs files <dir> <secret> <relay_token> <origin>");
  }
  const demo = JSON.parse(readFileSync(IDS_FILE, "utf8")).demo;
  if (!/^[0-9a-f]{32}$/.test(demo ?? "")) throw new Error("no demo user id (demo.mjs setup)");
  const write = (path, text, mode = 0o600) => writeFileSync(`${dir}/${path}`, text, { mode });
  mkdirSync(`${dir}/bridge`, { recursive: true, mode: 0o700 });
  mkdirSync(`${dir}/home/.config/ha-nova`, { recursive: true, mode: 0o700 });
  write("secret", `${secret}\n`);
  // The relay token is public anyway (connect.json); the bridge uses it as
  // `ha-nova try` does.
  write("home/.config/ha-nova/demo.json", JSON.stringify({ ha_url: origin, relay_url: "http://relay:8791", relay_token: relayToken }));
  // Only the demo user: never the owner, the relay's user, or a user a visitor makes.
  write("bridge/chat-apps.json", JSON.stringify({ allowed_users: [demo] }));
  // The plan store is empty from now on: a plan missing soon after was
  // made before this reset (cli/command_try.go demoPlanGone).
  write("bridge/plans-since", `${new Date().toISOString().replace(/\.\d{3}Z$/, "Z")}\n`);
}

async function http(url, init = {}) {
  const res = await fetch(url, { ...init, signal: AbortSignal.timeout(CALL_MS) });
  const text = await res.text();
  let body = null;
  try {
    body = JSON.parse(text);
  } catch {}
  return { status: res.status, body, text };
}

const mcp = (base, token, method, params = {}) => http(`${base}/api/ha_nova/mcp`, {
  method: "POST",
  headers: { authorization: `Bearer ${token}`, "content-type": "application/json", accept: "application/json, text/event-stream" },
  body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }),
});

// A definite break (exit 3: guard.sh resets at once), not a slow answer.
class Broken extends Error {}

// One WebSocket call (config/core/update has no REST twin).
function wsCall(base, token, payload) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`${base.replace(/^http/, "ws")}/api/websocket`);
    const timer = setTimeout(() => (ws.close(), reject(new Error(`${payload.type}: no answer`))), CALL_MS);
    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      if (msg.type === "auth_required") ws.send(JSON.stringify({ type: "auth", access_token: token }));
      else if (msg.type === "auth_ok") ws.send(JSON.stringify({ id: 1, ...payload }));
      else if (msg.type === "auth_invalid" || msg.type === "result") {
        clearTimeout(timer);
        ws.close();
        if (msg.success) resolve(msg.result);
        else reject(new Error(`${payload.type}: ${JSON.stringify(msg.error ?? msg.message)}`));
      }
    };
    ws.onerror = () => (clearTimeout(timer), reject(new Error(`${payload.type}: websocket error`)));
  });
}

// The minute check: the entry is loaded, and the owner's own token reaches
// the bridge only to be refused (Home Assistant -> integration -> bridge
// works, and the allow list holds). Mints no token.
async function check([haUrl, ownerToken, origin]) {
  if (!/^https?:\/\/[a-z0-9.:-]+$/.test(origin ?? "")) throw new Error("usage: chat.mjs check <ha_url> <owner_llat> <origin>");
  const base = haUrl.replace(/\/$/, "");
  const auth = { headers: { authorization: `Bearer ${ownerToken}` } };
  // HA names its OAuth metadata only for this origin, and Settings > Network
  // can change it (config/core/update has no YAML lock): put it back. REST,
  // because WS get_config hides external_url from a local-only user.
  const core = await http(`${base}/api/config`, auth);
  if (core.status !== 200) throw new Error(`GET /api/config: ${core.status}`);
  if (core.body.external_url !== origin || core.body.internal_url) {
    await wsCall(base, ownerToken, { type: "config/core/update", external_url: origin, internal_url: null });
  }
  const entries = await http(`${base}/api/config/config_entries/entry?domain=ha_nova`, auth);
  if (entries.status !== 200) throw new Error(`config entries: ${entries.status}`);
  if (entries.body?.length !== 1 || entries.body[0].state !== "loaded") {
    throw new Broken(`ha_nova entry missing or not loaded: ${entries.text.slice(0, 200)}`);
  }
  const owner = await mcp(base, ownerToken, "tools/list");
  if (owner.status === 200) throw new Broken("the bridge let the owner in");
  if (owner.status !== 403 || owner.body?.error?.code !== -32001) {
    throw new Error(`the bridge did not refuse the owner as expected: ${owner.status} ${owner.text.slice(0, 200)}`);
  }
}

// After every reset: the whole Add to Claude path on the tooling port, as
// claude.ai does it (CIMD client, its callback, PKCE S256), then the refresh
// token is revoked.
async function gate([haUrl, ownerToken, origin]) {
  const base = haUrl.replace(/\/$/, "");
  await check([haUrl, ownerToken, origin]);
  const b64 = (buf) => buf.toString("base64url");
  const verifier = b64(randomBytes(32));
  const json = (body) => ({ method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
  const flow = await http(`${base}/auth/login_flow`, json({
    client_id: CLAUDE_CLIENT, handler: ["homeassistant", null], redirect_uri: CLAUDE_CALLBACK,
    code_challenge: b64(createHash("sha256").update(verifier).digest()), code_challenge_method: "S256",
  }));
  if (flow.status !== 200 || !flow.body?.flow_id) throw new Error(`login flow: ${flow.status} ${flow.text.slice(0, 200)}`);
  const done = await http(`${base}/auth/login_flow/${flow.body.flow_id}`, json({ client_id: CLAUDE_CLIENT, username: "demo", password: "demo" }));
  if (done.body?.type !== "create_entry") throw new Error(`demo sign-in: ${done.status} ${done.text.slice(0, 200)}`);
  const token = await http(`${base}/auth/token`, {
    method: "POST",
    body: new URLSearchParams({ grant_type: "authorization_code", code: done.body.result, client_id: CLAUDE_CLIENT, code_verifier: verifier }),
  });
  const { access_token: access, refresh_token: refresh } = token.body ?? {};
  if (!access || !refresh) throw new Error(`token: ${token.status} ${token.text.slice(0, 200)}`);
  try {
    const init = await mcp(base, access, "initialize", { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "demo-gate", version: "1" } });
    if (init.status !== 200 || !init.body?.result?.serverInfo) throw new Error(`initialize: ${init.status} ${init.text.slice(0, 200)}`);
    const list = await mcp(base, access, "tools/list");
    const names = list.body?.result?.tools?.map((t) => t.name) ?? [];
    if (!names.includes("ha_get") || !names.includes("ha_apply")) throw new Error(`tools/list: ${list.status} ${list.text.slice(0, 200)}`);
    const get = await mcp(base, access, "tools/call", { name: "ha_get", arguments: { entity_id: "light.bed_light" } });
    if (get.body?.result?.structuredContent?.status !== "ok") throw new Error(`ha_get: ${get.status} ${get.text.slice(0, 300)}`);
  } finally {
    await http(`${base}/auth/revoke`, { method: "POST", body: new URLSearchParams({ token: refresh }) });
  }
}

// Chat-app sign-ins that outlive the hourly reset (spec decision 2). Only
// what a chat app needs to refresh: id, token, signing key, client, two
// times. Everything else is set here, never copied: no IP, no name or icon,
// HA's own expiries. A visitor admin may have written the old store
// (local_file), so each field is checked, the files are read without
// following links, and the count is capped.
// The published CIMD client ids, exactly: Claude's, and ChatGPT's two forms
// (developers.openai.com/apps-sdk/build/auth; HA offers no DCR). ChatGPT's
// callback id is the only free part: 64 URL-safe characters at most, no dots,
// so no address or sentence rides along.
const CHAT_CLIENT = /^(https:\/\/claude\.ai\/oauth\/mcp-oauth-client-metadata|https:\/\/chatgpt\.com\/oauth\/([A-Za-z0-9_-]{1,64}\/)?client\.json)$/;
const KEEP_MS = 24 * 3600_000;
const KEEP_MAX = 200;
const STORE_MAX = 32 << 20;

// A regular file inside a real directory, never a link; null if missing.
function readPlain(path) {
  for (const p of [path, path.slice(0, path.lastIndexOf("/"))]) {
    let st;
    try {
      st = lstatSync(p);
    } catch (err) {
      if (err.code === "ENOENT") return null;
      throw err;
    }
    if (p === path ? !st.isFile() : !st.isDirectory()) throw new Error(`${p}: not a plain ${p === path ? "file" : "directory"}`);
  }
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    if (fstatSync(fd).size > STORE_MAX) throw new Error(`${path}: over ${STORE_MAX} bytes`);
    return JSON.parse(readFileSync(fd, "utf8"));
  } finally {
    closeSync(fd);
  }
}

const writeAtomic = (path, value, mode = 0o600) => {
  writeFileSync(`${path}.tmp`, JSON.stringify(value), { mode });
  renameSync(`${path}.tmp`, path);
};

// The tokens to keep: the demo user's, from a chat app, used within 24 h, newest first.
function keepable(tokens, demo, now = Date.now()) {
  const hex = (n) => (s) => typeof s === "string" && s.length === n && /^[0-9a-f]+$/.test(s);
  return (Array.isArray(tokens) ? tokens : [])
    .filter((t) => t && t.user_id === demo && t.token_type === "normal" && CHAT_CLIENT.test(t.client_id ?? "")
      && hex(32)(t.id) && hex(128)(t.token) && hex(128)(t.jwt_key))
    .map((t) => ({ t, created: Date.parse(t.created_at), used: Date.parse(t.last_used_at ?? t.created_at) }))
    .filter(({ created, used }) => created <= used && used > now - KEEP_MS && used <= now + 60_000)
    .sort((a, b) => b.used - a.used)
    .slice(0, KEEP_MAX)
    .map(({ t, created, used }) => ({ id: t.id, client_id: t.client_id, token: t.token, jwt_key: t.jwt_key,
      created_at: new Date(created).toISOString(), last_used_at: new Date(used).toISOString() }));
}

// No previous store or demo user (first boot, a reset that failed before
// up.sh): the last carry file stays for restore, which filters it again.
// Anything present but unreadable, too large, or not a plain file drops it
// (fail closed: a visitor can bloat or replace the store).
function keep([stateDir, carry]) {
  if (!stateDir || !carry) throw new Error("usage: chat.mjs keep <state_dir> <carry_file>");
  let ids, auth;
  try {
    ids = readPlain(`${stateDir}/demo-users.json`);
    auth = readPlain(`${stateDir}/config/.storage/auth`);
  } catch (err) {
    rmSync(carry, { force: true });
    console.error(`chat.mjs keep: previous auth store unreadable (${err.message}); nothing kept`);
    return;
  }
  if (!ids || !auth) {
    console.error("chat.mjs keep: no previous demo user or auth store; the last carry file stays");
    return;
  }
  const all = Array.isArray(auth.data?.refresh_tokens) ? auth.data.refresh_tokens : [];
  const tokens = /^[0-9a-f]{32}$/.test(ids.demo ?? "") ? keepable(all, ids.demo) : [];
  if (tokens.length) writeAtomic(carry, tokens);
  else rmSync(carry, { force: true });
  // Per client host, no secrets: shows whether a chat app's real client id
  // ever falls outside CHAT_CLIENT (journal, ha-nova-demo-reset).
  const kept = new Set(tokens.map((t) => t.id));
  const host = (id) => /^https:\/\/([a-z0-9.-]{1,64})\//.exec(id ?? "")?.[1] ?? "other";
  const dropped = {};
  for (const t of all) {
    if (t?.user_id === ids.demo && t.token_type === "normal" && !kept.has(t.id)) dropped[host(t.client_id)] = (dropped[host(t.client_id)] ?? 0) + 1;
  }
  console.error(`chat.mjs keep: ${tokens.length} chat-app sign-in(s) kept; demo sign-ins dropped by client host: ${JSON.stringify(dropped)}`);
}

function restore([carry, stateDir]) {
  if (!carry || !stateDir) throw new Error("usage: chat.mjs restore <carry_file> <state_dir>");
  const demo = readPlain(`${stateDir}/demo-users.json`)?.demo;
  const path = `${stateDir}/config/.storage/auth`;
  const auth = readPlain(path);
  if (!/^[0-9a-f]{32}$/.test(demo ?? "") || !auth?.data?.refresh_tokens) throw new Error("no demo user or auth store of this build");
  const credential = auth.data.credentials.find((c) => c.user_id === demo && c.auth_provider_type === "homeassistant");
  if (!credential) throw new Error("the demo user has no Home Assistant credential");
  const version = auth.data.refresh_tokens.find((t) => t.version)?.version ?? null;
  const known = new Set(auth.data.refresh_tokens.map((t) => t.id));
  // Filtered again: a carry file left by a failed reset may be old, and one
  // older than 70 min (keep runs every reset) is not used at all.
  let carried = [];
  try {
    if (Date.now() - lstatSync(carry).mtimeMs < 70 * 60_000) carried = readPlain(carry) ?? [];
  } catch (err) {
    if (err.code !== "ENOENT") throw err;
  }
  const kept = keepable(carried.map((t) => ({ ...t, user_id: demo, token_type: "normal" })), demo)
    .filter((t) => !known.has(t.id));
  for (const t of kept) {
    auth.data.refresh_tokens.push({ ...t, user_id: demo, client_name: null, client_icon: null, token_type: "normal",
      access_token_expiration: 1800, last_used_ip: null, expire_at: Date.parse(t.last_used_at) / 1000 + 90 * 86400,
      credential_id: credential.id, version });
  }
  if (kept.length) writeAtomic(path, auth, lstatSync(path).mode & 0o777);
  else rmSync(carry, { force: true });
  console.log(kept.length);
}

const commands = { entry, files, gate, check, keep, restore };
if (!commands[cmd]) {
  console.error("usage: node chat.mjs entry|files|gate|check|keep|restore ...");
  process.exit(2);
}
Promise.resolve(commands[cmd](args)).then(
  () => process.exit(0),
  (err) => {
    console.error(`chat.mjs ${cmd}: ${err.message}`);
    process.exit(err instanceof Broken ? 3 : 1);
  },
);
