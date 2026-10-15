#!/usr/bin/env node
// Hosted demo house control, driven by up.sh (setup) and reset.sh / guard.sh
// on the demo host. Talks only to the local seeding path through Caddy
// (http://127.0.0.1:18123) with the owner's long-lived token. No dependencies.
//
//   node demo.mjs setup     <ha_url> <owner_llat>  http trial promote, owner local-only,
//                                                  admin user demo/demo, drop onboarding entries
//   node demo.mjs token     <ha_url> <owner_llat>  prints a long-lived token of the relay's own
//                                                  admin user (not demo, not the owner)
//   node demo.mjs dashboard <ha_url> <owner_llat>  default dashboard with the reset banner
//   node demo.mjs guard     <ha_url> <owner_llat>  minute check; exit 1 = full reset needed
//   node demo.mjs smoke     <ha_url> <owner_llat>  candidate gate (update-ha.sh); prints the HA version

import { randomBytes } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";

const [cmd, haUrl, token] = process.argv.slice(2);
if (!cmd || !haUrl || !token) {
  console.error("usage: node demo.mjs setup|token|dashboard|guard <ha_url> <owner_llat>");
  process.exit(2);
}
if (!/^http:\/\/127\.0\.0\.1:18123\/?$/.test(haUrl)) {
  console.error(`refusing ${haUrl}: the demo tooling only talks to http://127.0.0.1:18123`);
  process.exit(2);
}
const base = haUrl.replace(/\/$/, "");
const DEMO_USER = "demo";
const DEMO_PASSWORD = "demo"; // public on purpose: on the landing page (/start) and in docs/reference/hosted-demo.md
const RELAY_USER = "nova-relay"; // the public relay's HA identity; password random, never shown
const PROXY = "10.89.240.2/32"; // Caddy (edge.yml) = http.trusted_proxies
const ONBOARDING_EXTRAS = ["met", "google_translate", "radio_browser", "shopping_list"];
const BANNER_MARK = "**Public demo.**";
const DASHBOARD = "demo-house"; // storage dashboard; the system-wide default panel
const SMOKE_ENTITIES = ["light.bed_light", "lock.entrance_door", "cover.testbed_garage", "switch.smart_plug",
  "sensor.testbed_temperature", "button.router_restart", "light.template_desk_lamp", "input_boolean.guest_mode",
  "automation.a_guest_mode_unlocks_entrance_door", "script.leave_home", "scene.movie_night", "sensor.demo_next_reset"];
const HANDSHAKE_MS = 15_000;
const CALL_MS = 30_000; // per command: a stalled HA must not hang the guard
// User ids of this build (the test bed's .state is wiped every reset). The
// guard checks ids, so a visitor cannot swap in a look-alike user by name.
const IDS_FILE = new URL("../../e2e/testbed/.state/demo-users.json", import.meta.url);
const readIds = () => {
  try {
    return JSON.parse(readFileSync(IDS_FILE, "utf8"));
  } catch {
    return {};
  }
};
const rememberId = (username, id) => writeFileSync(IDS_FILE, JSON.stringify({ ...readIds(), [username]: id }), { mode: 0o600 });

async function connect(accessToken = token) {
  const ws = new WebSocket(`${base.replace(/^http/, "ws")}/api/websocket`);
  const pending = new Map();
  let nextId = 1;
  let authed;
  const ready = new Promise((resolve, reject) => (authed = { resolve, reject }));
  const deadline = setTimeout(() => authed.reject(new Error(`no websocket handshake within ${HANDSHAKE_MS} ms`)), HANDSHAKE_MS);
  ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    if (msg.type === "auth_required") ws.send(JSON.stringify({ type: "auth", access_token: accessToken }));
    else if (msg.type === "auth_ok") authed.resolve();
    else if (msg.type === "auth_invalid") authed.reject(new Error(`auth failed: ${msg.message}`));
    else if (msg.type === "result" && pending.has(msg.id)) {
      const { resolve, reject, type, timer } = pending.get(msg.id);
      clearTimeout(timer);
      pending.delete(msg.id);
      if (msg.success) resolve(msg.result);
      else reject(new Error(`${type}: ${JSON.stringify(msg.error)}`));
    }
  };
  ws.onerror = (e) => authed.reject(new Error(`websocket error: ${e.message ?? e.type}`));
  try {
    await ready;
  } finally {
    clearTimeout(deadline);
  }
  return {
    call(payload) {
      const id = nextId++;
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          pending.delete(id);
          reject(new Error(`${payload.type}: no answer within ${CALL_MS} ms`));
        }, CALL_MS);
        pending.set(id, { resolve, reject, type: payload.type, timer });
        ws.send(JSON.stringify({ id, ...payload }));
      });
    },
    close: () => ws.close(),
  };
}

async function withHa(fn) {
  const ha = await connect();
  try {
    return await fn(ha);
  } finally {
    ha.close();
  }
}

const byUsername = (users, name) => users.find((u) => u.username === name);
const isAdmin = (u) => u.group_ids.includes("system-admin");
const service = (domain, name, data) => ({ type: "call_service", domain, service: name, service_data: data });

async function ensureAdmin(ha, users, username, name, password) {
  let user = byUsername(users, username);
  if (user) {
    await ha.call({ type: "config/auth_provider/homeassistant/admin_change_password", user_id: user.id, password });
    rememberId(username, user.id);
    return user;
  }
  ({ user } = await ha.call({ type: "config/auth/create", name, group_ids: ["system-admin"], local_only: false }));
  await ha.call({ type: "config/auth_provider/homeassistant/create", user_id: user.id, username, password });
  rememberId(username, user.id);
  return user;
}

// HA 2026.10+ imports the YAML http block as a PENDING trial and reverts to
// the stored default (no proxy: every request 400) five minutes after boot
// unless an admin promotes it. Runs right after onboarding (up.sh).
async function setup() {
  await withHa(async (ha) => {
    let http = await ha.call({ type: "http/config" });
    if (http.pending && !http.pending.error) {
      await ha.call({ type: "http/config/promote" });
      http = await ha.call({ type: "http/config" });
    }
    if (httpProblem(http)) throw new Error(`http config not promoted: ${JSON.stringify(http.stable)}`);

    const users = await ha.call({ type: "config/auth/list" });
    const owner = users.find((u) => u.is_owner);
    if (!owner) throw new Error("no owner user; run onboarding first");
    // The owner is host tooling only: never a login from the internet.
    await ha.call({ type: "config/auth/update", user_id: owner.id, local_only: true });
    await ensureAdmin(ha, users, DEMO_USER, "Demo", DEMO_PASSWORD);

    // Onboarding adds these; they only call out (blocked anyway) or store data.
    const entries = await ha.call({ type: "config_entries/get" });
    for (const entry of entries.filter((e) => ONBOARDING_EXTRAS.includes(e.domain))) {
      const res = await fetch(`${base}/api/config/config_entries/entry/${entry.entry_id}`, {
        method: "DELETE", headers: { authorization: `Bearer ${token}` },
      });
      if (!res.ok) throw new Error(`delete ${entry.domain} entry -> HTTP ${res.status}`);
    }
  });
}

// A long-lived token for the public relay, owned by its own admin user: the
// owner's token never leaves host tooling, and visitors (logged in as demo)
// never see this token in their profile. Fresh random password each reset.
async function relayToken() {
  const password = randomBytes(24).toString("hex");
  await withHa(async (ha) => {
    await ensureAdmin(ha, await ha.call({ type: "config/auth/list" }), RELAY_USER, "NOVA Relay (demo tooling)", password);
  });
  const clientId = `${base}/`;
  const post = async (path, body, form) => {
    const res = await fetch(`${base}${path}`, form
      ? { method: "POST", body: new URLSearchParams(body) }
      : { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
    if (!res.ok) throw new Error(`POST ${path} -> HTTP ${res.status}`);
    return res.json();
  };
  const flow = await post("/auth/login_flow", { client_id: clientId, handler: ["homeassistant", null], redirect_uri: clientId });
  const done = await post(`/auth/login_flow/${flow.flow_id}`, { client_id: clientId, username: RELAY_USER, password });
  if (done.type !== "create_entry") throw new Error(`relay user login failed: ${JSON.stringify(done.errors ?? done.type)}`);
  const { access_token: access } = await post("/auth/token", { client_id: clientId, grant_type: "authorization_code", code: done.result }, true);
  const ha = await connect(access);
  try {
    console.log(await ha.call({ type: "auth/long_lived_access_token", client_name: "HA NOVA demo relay", lifespan: 1 }));
  } finally {
    ha.close();
  }
}

const banner = (version) => [
  BANNER_MARK,
  "Everything you change is wiped every hour at :00 UTC.",
  "Next reset in {{ ((as_timestamp(states('sensor.demo_next_reset'), as_timestamp(now())) - as_timestamp(now())) / 60) | round(0, 'ceil') | int }} min.",
  `Don't enter personal data. Running Home Assistant ${version}.`,
  "[About this demo](/start) · [Report abuse](/abuse)",
].join(" ");
const TILE_DOMAINS = ["light", "switch", "cover", "lock", "climate", "fan", "media_player", "vacuum", "humidifier",
  "water_heater", "sensor", "binary_sensor", "button", "input_boolean", "input_number", "input_select",
  "input_text", "input_button", "timer", "counter", "scene", "script", "automation", "camera"];

// The default dashboard: a storage dashboard (HA 2026.10 sends a default
// "lovelace" panel without a dashboard entry to the new Home panel) with one
// sections view by area and one by domain, every view carrying the banner,
// set as the system-wide default ("core".default_panel, what Settings >
// Dashboards > "Set as default" writes).
async function saveDashboard(ha) {
  const [config, states, entities, devices, areas, dashboards] = await Promise.all([
    ha.call({ type: "get_config" }),
    ha.call({ type: "get_states" }),
    ha.call({ type: "config/entity_registry/list" }),
    ha.call({ type: "config/device_registry/list" }),
    ha.call({ type: "config/area_registry/list" }),
    ha.call({ type: "lovelace/dashboards/list" }),
  ]);
  if (!dashboards.some((d) => d.url_path === DASHBOARD)) {
    await ha.call({
      type: "lovelace/dashboards/create", url_path: DASHBOARD, title: "Demo House", icon: "mdi:home-city",
      show_in_sidebar: true, require_admin: false,
    });
  }
  const header = { card: { type: "markdown", text_only: true, content: banner(config.version) } };
  const live = new Set(states.map((s) => s.entity_id));
  const deviceArea = Object.fromEntries(devices.map((d) => [d.id, d.area_id]));
  const shown = entities.filter((e) => live.has(e.entity_id) && !e.disabled_by && !e.hidden_by && !e.entity_category
    && TILE_DOMAINS.includes(e.entity_id.split(".")[0]));
  const areaOf = (e) => e.area_id ?? deviceArea[e.device_id] ?? null;
  const section = (heading, ids) => ({
    type: "grid",
    cards: [{ type: "heading", heading }, ...ids.map((entity) => ({ type: "tile", entity }))],
  });
  const byArea = areas
    .map((a) => section(a.name, shown.filter((e) => areaOf(e) === a.area_id).map((e) => e.entity_id)))
    .filter((s) => s.cards.length > 1);
  const byDomain = TILE_DOMAINS
    .map((d) => section(d, [...live].filter((id) => id.startsWith(`${d}.`)).sort()))
    .filter((s) => s.cards.length > 1);
  await ha.call({
    type: "lovelace/config/save",
    url_path: DASHBOARD,
    config: {
      title: "NOVA Demo House",
      views: [
        { title: "House", path: "house", icon: "mdi:home", type: "sections", max_columns: 4, header, sections: byArea },
        { title: "Everything", path: "everything", icon: "mdi:view-list", type: "sections", max_columns: 4, header, sections: byDomain },
      ],
    },
  });
  await setDefaultPanel(ha);
}

async function setDefaultPanel(ha) {
  const { value: core } = await ha.call({ type: "frontend/get_system_data", key: "core" });
  if (core?.default_panel !== DASHBOARD) {
    await ha.call({ type: "frontend/set_system_data", key: "core", value: { ...core, default_panel: DASHBOARD } });
  }
}

async function dashboard() {
  await withHa(saveDashboard);
}

function httpProblem(http) {
  const s = http.stable;
  return Boolean(http.pending) || s.use_x_forwarded_for !== true || s.ip_ban_enabled !== false
    || JSON.stringify(s.trusted_proxies) !== JSON.stringify([PROXY]);
}

// Every minute (guard.sh). Repairs what is cheap to repair and exits 1 on
// anything that needs the full reset. Connecting at all proves Caddy -> HA.
async function guard() {
  await withHa(async (ha) => {
    const users = await ha.call({ type: "config/auth/list" });
    const owner = users.find((u) => u.is_owner);
    const problems = [];
    if (!owner?.is_active || !owner.local_only) problems.push("owner changed");
    const ids = readIds();
    for (const name of [DEMO_USER, RELAY_USER]) {
      const user = users.find((u) => u.id === ids[name]);
      if (!user || user.username !== name || !user.is_active || !isAdmin(user) || user.local_only) {
        problems.push(`user ${name} missing or changed`);
      }
    }
    if (httpProblem(await ha.call({ type: "http/config" }))) problems.push("http config changed");
    if (problems.length) {
      console.error(`guard: ${problems.join(", ")}`);
      process.exitCode = 1;
      return;
    }

    // The public password.
    await ha.call({
      type: "config/auth_provider/homeassistant/admin_change_password",
      user_id: ids[DEMO_USER], password: DEMO_PASSWORD,
    });
    // Notices other visitors would see: failed logins (they show visitor IPs)
    // and anything carrying a link (phishing the next visitor).
    for (const n of await ha.call({ type: "persistent_notification/get" })) {
      if (n.notification_id === "http-login" || /\/\/|\]\s*[(\[]|\]:|href|www\./i.test(`${n.title ?? ""} ${n.message ?? ""}`)) {
        await ha.call(service("persistent_notification", "dismiss", { notification_id: n.notification_id }));
      }
    }
    // The banner dashboard and the system-wide default panel.
    const lovelace = await ha.call({ type: "lovelace/config", url_path: DASHBOARD }).catch(() => null);
    if (!lovelace?.views?.length || !lovelace.views.every((v) => v.header?.card?.content?.includes(BANNER_MARK))) {
      await saveDashboard(ha);
    } else {
      await setDefaultPanel(ha);
    }
  });
}

// Gate for a new Home Assistant release (update-ha.sh), after a full reset
// with the candidate image: the house is complete, a service call round-trips,
// and the guard's invariants hold. Prints the running version.
async function smoke() {
  await withHa(async (ha) => {
    const [config, states, areas] = await Promise.all([
      ha.call({ type: "get_config" }),
      ha.call({ type: "get_states" }),
      ha.call({ type: "config/area_registry/list" }),
    ]);
    const ids = new Set(states.map((s) => s.entity_id));
    const missing = SMOKE_ENTITIES.filter((id) => !ids.has(id));
    if (missing.length) throw new Error(`missing entities: ${missing.join(", ")}`);
    if (states.length < 150 || areas.length !== 5) throw new Error(`house incomplete: ${states.length} entities, ${areas.length} areas`);
    if (Number.isNaN(Date.parse(states.find((s) => s.entity_id === "sensor.demo_next_reset").state))) {
      throw new Error("sensor.demo_next_reset is not a timestamp");
    }
    await ha.call(service("light", "turn_on", { entity_id: "light.bed_light" }));
    for (let i = 0; ; i++) {
      const s = (await ha.call({ type: "get_states" })).find((x) => x.entity_id === "light.bed_light");
      if (s.state === "on") break;
      if (i === 10) throw new Error("light.bed_light did not turn on");
      await new Promise((r) => setTimeout(r, 500));
    }
    await ha.call(service("light", "turn_off", { entity_id: "light.bed_light" }));
    console.log(config.version);
  });
  await guard();
  if (process.exitCode) throw new Error("guard invariants failed");
}

const commands = { setup, token: relayToken, dashboard, guard, smoke };
if (!commands[cmd]) {
  console.error(`unknown command ${cmd}`);
  process.exit(2);
}
commands[cmd]().then(
  () => process.exit(process.exitCode ?? 0),
  (err) => {
    console.error(`demo.mjs ${cmd}: ${err.message}`);
    process.exit(1);
  },
);
