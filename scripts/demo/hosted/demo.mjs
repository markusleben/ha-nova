#!/usr/bin/env node
// Hosted demo house control, driven by up.sh (setup) and reset.sh / guard.sh
// on the demo host. Talks only to the local seeding path through Caddy
// (http://127.0.0.1:18123) with the owner's long-lived token. No dependencies.
//
//   node demo.mjs setup     <ha_url> <owner_llat>  http trial promote, owner local-only,
//                                                  admin user demo/demo, drop onboarding entries
//   node demo.mjs token     <ha_url> <owner_llat>  prints a long-lived token OF THE DEMO USER (relay)
//   node demo.mjs dashboard <ha_url> <owner_llat>  default dashboard with the reset banner
//   node demo.mjs guard     <ha_url> <owner_llat>  minute check; exit 1 = full reset needed

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
const DEMO_PASSWORD = "demo"; // public on purpose: printed on the demo's login page and README
const PROXY = "10.89.240.2/32"; // Caddy (edge.yml) = http.trusted_proxies
const ONBOARDING_EXTRAS = ["met", "google_translate", "radio_browser", "shopping_list"];

async function connect(accessToken = token) {
  const ws = new WebSocket(`${base.replace(/^http/, "ws")}/api/websocket`);
  const pending = new Map();
  let nextId = 1;
  let authed;
  const ready = new Promise((resolve, reject) => (authed = { resolve, reject }));
  ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    if (msg.type === "auth_required") ws.send(JSON.stringify({ type: "auth", access_token: accessToken }));
    else if (msg.type === "auth_ok") authed.resolve();
    else if (msg.type === "auth_invalid") authed.reject(new Error(`auth failed: ${msg.message}`));
    else if (msg.type === "result" && pending.has(msg.id)) {
      const { resolve, reject, type } = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.success) resolve(msg.result);
      else reject(new Error(`${type}: ${JSON.stringify(msg.error)}`));
    }
  };
  ws.onerror = (e) => authed.reject(new Error(`websocket error: ${e.message ?? e.type}`));
  await ready;
  return {
    call(payload) {
      const id = nextId++;
      return new Promise((resolve, reject) => {
        pending.set(id, { resolve, reject, type: payload.type });
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

const findDemo = (users) => users.find((u) => u.username === DEMO_USER);
const isAdmin = (u) => u.group_ids.includes("system-admin");

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
    if (!http.stable.use_x_forwarded_for || JSON.stringify(http.stable.trusted_proxies) !== JSON.stringify([PROXY])) {
      throw new Error(`http config not promoted: ${JSON.stringify(http.stable)}`);
    }

    const users = await ha.call({ type: "config/auth/list" });
    const owner = users.find((u) => u.is_owner);
    if (!owner) throw new Error("no owner user; run onboarding first");
    // The owner is host tooling only: never a login from the internet.
    await ha.call({ type: "config/auth/update", user_id: owner.id, local_only: true });

    let demo = findDemo(users);
    if (!demo) {
      ({ user: demo } = await ha.call({
        type: "config/auth/create", name: "Demo", group_ids: ["system-admin"], local_only: false,
      }));
      await ha.call({
        type: "config/auth_provider/homeassistant/create", user_id: demo.id, username: DEMO_USER, password: DEMO_PASSWORD,
      });
    }

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

// A long-lived token of the demo user, for the public relay: the owner's token
// never leaves host tooling. Login flow as the demo user, then mint over WS.
async function demoToken() {
  const clientId = `${base}/`;
  const post = async (path, body, form) => {
    const res = await fetch(`${base}${path}`, form
      ? { method: "POST", body: new URLSearchParams(body) }
      : { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
    if (!res.ok) throw new Error(`POST ${path} -> HTTP ${res.status}`);
    return res.json();
  };
  const flow = await post("/auth/login_flow", { client_id: clientId, handler: ["homeassistant", null], redirect_uri: clientId });
  const done = await post(`/auth/login_flow/${flow.flow_id}`, { client_id: clientId, username: DEMO_USER, password: DEMO_PASSWORD });
  if (done.type !== "create_entry") throw new Error(`demo login failed: ${JSON.stringify(done.errors ?? done.type)}`);
  const { access_token: access } = await post("/auth/token", { client_id: clientId, grant_type: "authorization_code", code: done.result }, true);
  const ha = await connect(access);
  try {
    console.log(await ha.call({ type: "auth/long_lived_access_token", client_name: "HA NOVA demo relay", lifespan: 1 }));
  } finally {
    ha.close();
  }
}

const BANNER = [
  "**Public demo.** Everything you change is wiped every hour at :00 UTC.",
  "Next reset in {{ ((as_timestamp(states('sensor.demo_next_reset'), as_timestamp(now())) - as_timestamp(now())) / 60) | round(0, 'ceil') | int }} min.",
  "Don't enter personal data. Report abuse: [/abuse](/abuse)",
].join(" ");
const header = { card: { type: "markdown", text_only: true, content: BANNER } };
const TILE_DOMAINS = ["light", "switch", "cover", "lock", "climate", "fan", "media_player", "vacuum", "humidifier",
  "water_heater", "sensor", "binary_sensor", "button", "input_boolean", "input_number", "input_select",
  "input_text", "input_button", "timer", "counter", "scene", "script", "automation", "camera"];

// The default dashboard: one sections view per area set plus one per domain,
// every view carrying the reset banner as its header.
async function dashboard() {
  await withHa(async (ha) => {
    const [states, entities, devices, areas] = await Promise.all([
      ha.call({ type: "get_states" }),
      ha.call({ type: "config/entity_registry/list" }),
      ha.call({ type: "config/device_registry/list" }),
      ha.call({ type: "config/area_registry/list" }),
    ]);
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
      config: {
        title: "NOVA Demo House",
        views: [
          { title: "House", path: "house", icon: "mdi:home", type: "sections", max_columns: 4, header, sections: byArea },
          { title: "Everything", path: "everything", icon: "mdi:view-list", type: "sections", max_columns: 4, header, sections: byDomain },
        ],
      },
    });
  });
}

// Every minute (guard.sh). Repairs what is cheap to repair (the public
// password, failed-login notices that show visitors' IPs) and exits 1 on
// anything that needs the full reset. Connecting at all proves Caddy -> HA.
async function guard() {
  await withHa(async (ha) => {
    const users = await ha.call({ type: "config/auth/list" });
    const owner = users.find((u) => u.is_owner);
    const demo = findDemo(users);
    const problems = [];
    if (!owner?.is_active || !owner.local_only) problems.push("owner changed");
    if (!demo) problems.push("demo user missing");
    else if (!demo.is_active || !isAdmin(demo) || demo.local_only) problems.push("demo user changed");
    const http = await ha.call({ type: "http/config" });
    if (http.pending || JSON.stringify(http.stable.trusted_proxies) !== JSON.stringify([PROXY])) problems.push("http config changed");
    if (problems.length) {
      console.error(`guard: ${problems.join(", ")}`);
      process.exitCode = 1;
      return;
    }
    await ha.call({ type: "config/auth_provider/homeassistant/admin_change_password", user_id: demo.id, password: DEMO_PASSWORD });
    await ha.call({ type: "call_service", domain: "persistent_notification", service: "dismiss", service_data: { notification_id: "http-login" } });
  });
}

const commands = { setup, token: demoToken, dashboard, guard };
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
