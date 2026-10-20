#!/usr/bin/env node
// Hosted demo house control, driven by up.sh (setup) and reset.sh / guard.sh
// on the demo host. Talks only to the local seeding path through Caddy
// (http://127.0.0.1:18123) with the owner's long-lived token. No dependencies.
//
//   node demo.mjs setup     <ha_url> <owner_llat>  http config check, owner local-only,
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
const DEMO_PASSWORD = "demo"; // public on purpose: on the landing page (hanova.app/demo/) and in docs/reference/hosted-demo.md
const RELAY_USER = "nova-relay"; // the public relay's HA identity; password random, never shown
const PROXY = "10.89.240.2/32"; // Caddy (edge.yml) = trusted_proxies in config/.storage/http
const ONBOARDING_EXTRAS = ["met", "google_translate", "radio_browser", "shopping_list"];
const BANNER_MARK = "**Public demo — everyone shares one account.**";
const DASHBOARD = "demo-house"; // storage dashboard; the system-wide default panel
const SMOKE_ENTITIES = ["light.bed_light", "lock.entrance_door", "cover.testbed_garage", "switch.smart_plug",
  "sensor.testbed_temperature", "button.router_restart", "light.template_desk_lamp", "input_boolean.guest_mode",
  "automation.a_guest_mode_unlocks_entrance_door", "script.leave_home", "scene.movie_night", "sensor.demo_next_reset"];
// https://<demo domain>: configuration.yaml's external_url (guard.sh, update-ha.sh).
const EXTERNAL_URL = process.env.DEMO_EXTERNAL_URL;
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

// Runs right after onboarding (up.sh). The proxy settings come from the
// seeded store (config/.storage/http), not YAML: fail loud if HA did not
// take them as they are.
async function setup() {
  await withHa(async (ha) => {
    const http = await ha.call({ type: "http/config" });
    if (httpProblem(http)) throw new Error(`http config is not the seeded one: ${JSON.stringify(http)}`);

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
  "Anything you enter is visible to other visitors until the next reset.",
  "**Never enter real names, addresses, passwords, API keys or tokens, and don't connect real accounts or devices.**",
  "Everything you change is wiped every hour at :00 UTC.",
  "Next reset in {{ ((as_timestamp(states('sensor.demo_next_reset'), as_timestamp(now())) - as_timestamp(now())) / 60) | round(0, 'ceil') | int }} min.",
  `Running Home Assistant ${version}.`,
  "[About this demo](/start) · [Report abuse](/abuse) · [Privacy](https://hanova.app/privacy.html)",
].join(" ");
const TILE_DOMAINS = ["light", "switch", "cover", "lock", "climate", "fan", "media_player", "vacuum", "humidifier",
  "water_heater", "sensor", "binary_sensor", "button", "input_boolean", "input_number", "input_select",
  "input_text", "input_button", "timer", "counter", "scene", "script", "automation", "camera"];
// Off the dashboard, still in the house for agents: demo platform entities
// that do nothing on a tap, on purpose (HA's demo integration shows feature
// edge cases: a lock that jams, vacuums without controls, players that only
// browse or search), and demo.yaml's automations, so no tap switches one off.
const OFF_DASHBOARD = ["lock.poorly_installed_door", "vacuum.demo_vacuum_3_third_floor",
  "vacuum.demo_vacuum_4_fourth_floor", "media_player.browse", "media_player.search"];
// demo.yaml's automations by id (their entity ids follow their names).
const DEMO_AUTOMATIONS = ["demo_reset_warning", "demo_simulated_devices", "demo_relock_entrance_door"];
const demoAutomationRows = (rows, ids = DEMO_AUTOMATIONS) =>
  ids.map((id) => rows.find((e) => e.platform === "automation" && e.unique_id === id)).filter(Boolean);
const demoAutomations = async (ha, ids) => demoAutomationRows(await ha.call({ type: "config/entity_registry/list" }), ids);

// The default dashboard: a storage dashboard (HA 2026.10 sends a default
// "lovelace" panel without a dashboard entry to the new Home panel) with one
// sections view by area and one by domain, every view carrying the banner,
// set as the system-wide default ("core".default_panel, what Settings >
// Dashboards > "Set as default" writes). `ha-nova try` asks where the bed
// light is used: keep it on this dashboard (cli/command_try.go).
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
  const hidden = [...OFF_DASHBOARD, ...demoAutomationRows(entities).map((e) => e.entity_id)];
  const live = new Set(states.map((s) => s.entity_id).filter((id) => !hidden.includes(id)));
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

// The demo integration names this update device "Demo add-on"; HA calls them
// apps. Cosmetic: skipped if a future demo integration drops the device.
async function nameAppDevice(ha) {
  const devices = await ha.call({ type: "config/device_registry/list" });
  const app = devices.find((d) => d.identifiers.some(([domain, id]) => domain === "demo" && id === "update_addon"));
  if (app && app.name_by_user !== "Demo app") {
    await ha.call({ type: "config/device_registry/update", device_id: app.id, name_by_user: "Demo app" });
  }
}

// Onboarding adds a "Map" dashboard. The house has no egress, so its map
// tiles (/api/map_tiles/, proxied from OpenStreetMap) never load: an empty
// map and 503s. Not worth a third party in visitors' browsers; the panel goes.
async function dropMapDashboard(ha) {
  const map = (await ha.call({ type: "lovelace/dashboards/list" })).find((d) => d.url_path === "map");
  if (map) await ha.call({ type: "lovelace/dashboards/delete", dashboard_id: map.id });
}

// Calls the service until the entity shows the state (a command can be lost
// while nothing listens yet), up to `seconds`.
async function roundTrip(ha, domain, name, entityId, want, seconds = 30) {
  const deadline = Date.now() + seconds * 1000;
  for (;;) {
    try {
      await ha.call(service(domain, name, { entity_id: entityId }));
    } catch (err) {
      if (Date.now() > deadline) throw err;
    }
    for (let i = 0; i < 4; i++) {
      await new Promise((r) => setTimeout(r, 500));
      if ((await ha.call({ type: "get_states" })).find((x) => x.entity_id === entityId)?.state === want) return;
    }
    if (Date.now() > deadline) throw new Error(`${entityId} did not become ${want} within ${seconds} s`);
  }
}

// demo.yaml's MQTT trigger could not attach at boot (seed.mjs adds the
// broker entry later), and a reload skips unchanged automations: off and on.
// HA's MQTT client then still holds new subscriptions back for up to a
// minute after a reset: wait for a real answer (75 + 15 s, inside reset.sh's
// 120) before the house is exposed. No answer only warns: the house works
// without it, and smoke (update-ha.sh) gates new HA releases on it.
async function armSimulatedDevices(ha) {
  const [row] = await demoAutomations(ha, ["demo_simulated_devices"]);
  if (!row) throw new Error("automation demo_simulated_devices (config/packages/demo.yaml) is missing");
  await ha.call(service("automation", "turn_off", { entity_id: row.entity_id }));
  await ha.call(service("automation", "turn_on", { entity_id: row.entity_id }));
  try {
    await roundTrip(ha, "lock", "unlock", "lock.entrance_door", "unlocked", 75);
    await roundTrip(ha, "lock", "lock", "lock.entrance_door", "locked", 15);
  } catch (err) {
    console.error(`demo.mjs dashboard: warning: simulated devices do not answer yet: ${err.message}`);
  }
}

// After the house is seeded (reset.sh): seed.mjs waited for the demo entities.
async function dashboard() {
  await withHa(async (ha) => {
    await saveDashboard(ha);
    await nameAppDevice(ha);
    await dropMapDashboard(ha);
    await armSimulatedDevices(ha);
  });
}

function httpProblem(http) {
  const s = http.stable;
  return Boolean(http.pending) || http.active_config_type !== "stable"
    || s.use_x_forwarded_for !== true || s.ip_ban_enabled !== false
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

    // Add to Claude: HA names its OAuth metadata only for this origin, and
    // Settings > Network can change it (config/core/update has no YAML lock).
    if (!/^https?:\/\/[a-z0-9.:-]+$/.test(EXTERNAL_URL ?? "")) throw new Error("DEMO_EXTERNAL_URL is not set");
    const core = await ha.call({ type: "get_config" });
    if (core.external_url !== EXTERNAL_URL || core.internal_url) {
      await ha.call({ type: "config/core/update", external_url: EXTERNAL_URL, internal_url: null });
    }
    // The public password.
    await ha.call({
      type: "config/auth_provider/homeassistant/admin_change_password",
      user_id: ids[DEMO_USER], password: DEMO_PASSWORD,
    });
    // Notices other visitors would see: failed logins (visitor IPs, truncated by Caddy)
    // and anything carrying a link (phishing the next visitor).
    for (const n of await ha.call({ type: "persistent_notification/get" })) {
      if (n.notification_id === "http-login" || /\/\/|\]\s*[(\[]|\]:|href|www\./i.test(`${n.title ?? ""} ${n.message ?? ""}`)) {
        await ha.call(service("persistent_notification", "dismiss", { notification_id: n.notification_id }));
      }
    }
    // demo.yaml's automations, if a visitor turned them off.
    const states = await ha.call({ type: "get_states" });
    for (const row of await demoAutomations(ha)) {
      if (states.find((x) => x.entity_id === row.entity_id)?.state === "off") {
        await ha.call(service("automation", "turn_on", { entity_id: row.entity_id }));
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
    await roundTrip(ha, "light", "turn_on", "light.bed_light", "on");
    await roundTrip(ha, "light", "turn_off", "light.bed_light", "off");
    // An MQTT device reports back (demo.yaml).
    await roundTrip(ha, "lock", "unlock", "lock.entrance_door", "unlocked");
    await roundTrip(ha, "lock", "lock", "lock.entrance_door", "locked");
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
