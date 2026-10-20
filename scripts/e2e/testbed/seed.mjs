#!/usr/bin/env node
// Test bed seeding through Home Assistant's own APIs (Node's built-in
// WebSocket + fetch; no dependencies). Every step is idempotent: existing
// floors/areas/labels are reused by name, registry updates are absolute.
//
//   node seed.mjs mint    <ha_url> <access_token>   -> prints a long-lived token
//   node seed.mjs house   <ha_url> <llat>           -> MQTT entry + registry seed
//   node seed.mjs summary <ha_url> <llat>           -> prints the summary JSON

const [cmd, haUrl, token] = process.argv.slice(2);
if (!cmd || !haUrl || !token) {
  console.error("usage: node seed.mjs mint|house|summary <ha_url> <token>");
  process.exit(2);
}
if (!/^http:\/\/(127\.0\.0\.1|localhost):18123\/?$/.test(haUrl)) {
  console.error(`refusing ${haUrl}: the test bed only talks to http://127.0.0.1:18123`);
  process.exit(2);
}
const base = haUrl.replace(/\/$/, "");

// ---- transport -------------------------------------------------------------

async function connect() {
  const ws = new WebSocket(`${base.replace(/^http/, "ws")}/api/websocket`);
  const pending = new Map();
  let nextId = 1;
  let authed;
  const ready = new Promise((resolve, reject) => (authed = { resolve, reject }));
  ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    if (msg.type === "auth_required") ws.send(JSON.stringify({ type: "auth", access_token: token }));
    else if (msg.type === "auth_ok") authed.resolve();
    else if (msg.type === "auth_invalid") authed.reject(new Error(`auth failed: ${msg.message}`));
    else if (msg.type === "result" && pending.has(msg.id)) {
      const { resolve, reject, payload } = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.success) resolve(msg.result);
      else reject(new Error(`${payload.type}: ${JSON.stringify(msg.error)}`));
    }
  };
  ws.onerror = (e) => authed.reject(new Error(`websocket error: ${e.message ?? e.type}`));
  await ready;
  return {
    call(payload) {
      const id = nextId++;
      return new Promise((resolve, reject) => {
        pending.set(id, { resolve, reject, payload });
        ws.send(JSON.stringify({ id, ...payload }));
      });
    },
    close: () => ws.close(),
  };
}

async function rest(method, path, body) {
  const res = await fetch(`${base}${path}`, {
    method,
    headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path} -> HTTP ${res.status}: ${text}`);
  return text ? JSON.parse(text) : null;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitFor(what, check, timeoutMs = 90_000) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const value = await check();
    if (value) return value;
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${what}`);
    await sleep(1000);
  }
}

// ---- the house ---------------------------------------------------------------

const FLOORS = [
  { name: "Erdgeschoss", level: 0 },
  { name: "Obergeschoss", level: 1 },
];
const AREAS = [
  { name: "Küche", floor: "Erdgeschoss" },
  { name: "Wohnzimmer", floor: "Erdgeschoss" },
  { name: "Flur", floor: "Erdgeschoss" },
  { name: "Garage", floor: "Erdgeschoss" },
  { name: "Schlafzimmer", floor: "Obergeschoss" },
];
const LABELS = [
  { name: "Security", color: "red", icon: "mdi:shield-lock" },
  { name: "Lighting", color: "amber", icon: "mdi:lightbulb" },
];
// `ha-nova try` names the bed light, kitchen lights, and entrance door
// (cli/command_try.go, demoTryPrompts): keep them when changing the house
// (and the bed light on the hosted dashboard, scripts/demo/hosted/demo.mjs).
// Device areas, addressed by one entity on the device.
const DEVICE_AREAS = {
  "light.kitchen_lights": "Küche",
  "switch.smart_plug": "Küche",
  "light.living_room_rgbww_lights": "Wohnzimmer",
  "light.ceiling_lights": "Wohnzimmer",
  "light.bed_light": "Schlafzimmer",
  "lock.entrance_door": "Flur",
  "cover.garage_door": "Garage",
  "cover.testbed_garage": "Garage",
  "sensor.testbed_temperature": "Wohnzimmer",
};
// Entity registry edits (absolute values -> idempotent).
const ENTITY_EDITS = [
  // Entity-level area override: the Smart Plug device sits in Küche, its switch
  // in Wohnzimmer (HA allows this only for entities with a name of their own).
  { entity_id: "switch.smart_plug", area: "Wohnzimmer" },
  { entity_id: "light.ceiling_lights", labels: ["Lighting"] },
  { entity_id: "light.bed_light", aliases: ["Leselampe"], labels: ["Lighting"] },
  { entity_id: "light.kitchen_lights", labels: ["Lighting"] },
  { entity_id: "light.living_room_rgbww_lights", labels: ["Lighting"] },
  { entity_id: "lock.entrance_door", aliases: ["Haustür"], labels: ["Security"] },
  { entity_id: "cover.garage_door", labels: ["Security"] },
  { entity_id: "cover.testbed_garage", labels: ["Security"] },
  { entity_id: "cover.driveway_gate", labels: ["Security"] },
  // device_class override: a class-less cover becomes a (non-access) shutter.
  { entity_id: "cover.kitchen_window", device_class: "shutter" },
  // A non-user-authored restart button (the MQTT one is USER_AUTHORED_BUTTON).
  { entity_id: "button.push", name: "Modem restart", device_class: "restart", new_entity_id: "button.modem_restart" },
  // Disabled in the registry.
  { entity_id: "switch.ac", disabled_by: "user" },
];
const REQUIRED_ENTITIES = [
  ...Object.keys(DEVICE_AREAS),
  "light.office_rgbw_lights", "light.entrance_color_white_lights",
  "cover.kitchen_window", "button.push", "switch.ac", "button.plug_factory_reset",
  "button.router_restart", "binary_sensor.movement_backyard", "cover.driveway_gate",
];

async function seedHouse() {
  await ensureMqttEntry();
  const ha = await connect();
  try {
    const entities = await waitFor("demo + MQTT entities", async () => {
      const list = await ha.call({ type: "config/entity_registry/list" });
      const ids = new Set(list.map((e) => e.entity_id));
      const doneRename = ids.has("button.modem_restart");
      const missing = REQUIRED_ENTITIES.filter((id) => !ids.has(id) && !(doneRename && id === "button.push"));
      if (missing.length) process.stderr.write(`  waiting for: ${missing.join(", ")}\n`);
      return missing.length ? null : list;
    });
    const byEntity = Object.fromEntries(entities.map((e) => [e.entity_id, e]));

    const floors = await ensureByName(ha, "floor", FLOORS, (f) => ({ name: f.name, level: f.level }));
    const areas = await ensureByName(ha, "area", AREAS, (a) => ({ name: a.name, floor_id: floors[a.floor] }));
    const labels = await ensureByName(ha, "label", LABELS, (l) => ({ name: l.name, color: l.color, icon: l.icon }));
    // Onboarding adds English default areas (Living Room, Kitchen, Bedroom);
    // drop them so "Küche" is the only kitchen and area ids stay unambiguous.
    for (const area of await ha.call({ type: "config/area_registry/list" })) {
      if (!Object.values(areas).includes(area.area_id)) {
        await ha.call({ type: "config/area_registry/delete", area_id: area.area_id });
      }
    }

    for (const [entityId, area] of Object.entries(DEVICE_AREAS)) {
      const deviceId = byEntity[entityId]?.device_id;
      if (!deviceId) throw new Error(`${entityId} has no device; cannot place it in ${area}`);
      await ha.call({ type: "config/device_registry/update", device_id: deviceId, area_id: areas[area] });
    }
    for (const edit of ENTITY_EDITS) {
      const current = byEntity[edit.entity_id] ?? byEntity[edit.new_entity_id];
      if (!current) throw new Error(`entity ${edit.entity_id} missing`);
      const update = { type: "config/entity_registry/update", entity_id: current.entity_id };
      if (edit.area) update.area_id = areas[edit.area];
      if (edit.aliases) update.aliases = edit.aliases;
      if (edit.labels) update.labels = edit.labels.map((l) => labels[l]);
      if (edit.device_class) update.device_class = edit.device_class;
      if (edit.name) update.name = edit.name;
      if (edit.disabled_by) update.disabled_by = edit.disabled_by;
      if (edit.new_entity_id && current.entity_id !== edit.new_entity_id) update.new_entity_id = edit.new_entity_id;
      await ha.call(update);
    }
    console.log(JSON.stringify({ floors, areas, labels }));
  } finally {
    ha.close();
  }
}

// Registry kinds share one shape: <kind>_id + name, list/create by type.
async function ensureByName(ha, kind, wanted, toCreate) {
  const existing = await ha.call({ type: `config/${kind}_registry/list` });
  const ids = {};
  for (const item of wanted) {
    const found = existing.find((e) => e.name === item.name);
    const row = found ?? (await ha.call({ type: `config/${kind}_registry/create`, ...toCreate(item) }));
    ids[item.name] = row[`${kind}_id`];
  }
  return ids;
}

// The broker connection is UI-only: drive the config flow like the UI does.
async function ensureMqttEntry() {
  const entries = await rest("GET", "/api/config/config_entries/entry?domain=mqtt");
  if (entries.some((e) => e.state === "loaded")) return;
  let step = await rest("POST", "/api/config/config_entries/flow", { handler: "mqtt", show_advanced_options: false });
  if (step.type === "menu") {
    step = await rest("POST", `/api/config/config_entries/flow/${step.flow_id}`, { next_step_id: "broker" });
  }
  step = await rest("POST", `/api/config/config_entries/flow/${step.flow_id}`, {
    broker: "mosquitto", port: 1883, protocol: "5",
    other_settings: { set_client_cert: false, set_ca_cert: "off", transport: "tcp" },
  });
  if (step.type !== "create_entry") throw new Error(`MQTT flow did not create an entry: ${JSON.stringify(step)}`);
  await waitFor("MQTT entry loaded", async () =>
    (await rest("GET", "/api/config/config_entries/entry?domain=mqtt")).some((e) => e.state === "loaded"));
}

// ---- summary -----------------------------------------------------------------

async function summary() {
  const ha = await connect();
  try {
    const [config, states, devices, areas, floors, labels] = await Promise.all([
      rest("GET", "/api/config"),
      rest("GET", "/api/states"),
      ha.call({ type: "config/device_registry/list" }),
      ha.call({ type: "config/area_registry/list" }),
      ha.call({ type: "config/floor_registry/list" }),
      ha.call({ type: "config/label_registry/list" }),
    ]);
    const count = (...domains) => states.filter((s) => domains.includes(s.entity_id.split(".")[0])).length;
    console.log(JSON.stringify({
      ha_version: config.version,
      entities: states.length,
      devices: devices.length,
      floors: floors.map((f) => f.floor_id),
      areas: areas.map((a) => a.area_id),
      labels: labels.map((l) => l.label_id),
      automations: count("automation"),
      scripts: count("script"),
      scenes: count("scene"),
      helpers: count("input_boolean", "input_number", "input_select", "input_text", "input_button", "timer", "counter", "schedule"),
      mqtt_entities: states.filter((s) => /^(lock\.entrance_door|cover\.(testbed_garage|driveway_gate)|switch\.smart_plug|sensor\.testbed_temperature|button\.(plug_factory_reset|router_restart))$/.test(s.entity_id)).length,
    }, null, 2));
  } finally {
    ha.close();
  }
}

// ---- LLAT --------------------------------------------------------------------

async function mint() {
  const ha = await connect();
  try {
    console.log(await ha.call({ type: "auth/long_lived_access_token", client_name: "HA NOVA testbed", lifespan: 30 }));
  } finally {
    ha.close();
  }
}

const commands = { mint, house: seedHouse, summary };
if (!commands[cmd]) {
  console.error(`unknown command ${cmd}`);
  process.exit(2);
}
commands[cmd]().then(
  () => process.exit(0),
  (err) => {
    console.error(`seed.mjs ${cmd}: ${err.message}`);
    process.exit(1);
  },
);
