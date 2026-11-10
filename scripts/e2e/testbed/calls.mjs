// Service-call listeners in step h (MAR-127, scripts/e2e/testbed/calm.mjs): a
// listener on call_service filtered to covers is set off only by a cover call.
// Then an oracle on this real Home Assistant: for each plan, every rclane
// automation that really ran after it was applied must be among the plan's
// consumers. Runs come from Home Assistant's traces (each run, not only the
// latest), started after the bed's last change before the apply (its own
// clock; the bed is otherwise quiet), read until a settle without new runs;
// and each case's expected automations must run, so a check can't pass by
// running nothing.
//
// UI thermostat helpers (MAR-131): the engine reads the heater from a generic
// thermostat's options flow and aborts it. With a demo fan nothing listens
// to as heater, a light call stays ordinary and planning changes nothing in
// the thermostat's entry; setting it to heat turns the fan on through
// homeassistant.turn_on, a consumer. A second thermostat whose heater the
// relay automation follows makes every listener count again.
import { execFileSync } from "node:child_process";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const names = (plan) => (plan.consumers ?? []).map((c) => c.automation);
const codes = (plan) => (plan.reasons ?? []).map((r) => r.code);

// Listener automations on call_service.
const ORACLE = {
  climate_set_temperature: { domain: "climate", service: "set_temperature" },
  light_turn_on: { domain: "light", service: "turn_on" },
  light_turn_off: { domain: "light", service: "turn_off" },
  switch_turn_on: { domain: "switch", service: "turn_on" },
  switch_turn_off: { domain: "switch", service: "turn_off" },
  cover_stop_cover: { domain: "cover", service: "stop_cover" },
  homeassistant_turn_on: { domain: "homeassistant", service: "turn_on" },
};
const RELAY = "switch.decorative_lights"; // a demo switch on its own device, switched by a switch_as_x light
const FAN = "fan.ceiling_fan"; // a demo fan nothing in the bed listens to: the first thermostat's heater

// A generic thermostat set up in the UI, as the helper skill does; its entry id.
async function thermostat(ha, name, heater) {
  const flow = await ha("POST", "/api/config/config_entries/flow", { handler: "generic_thermostat" });
  let step = await ha("POST", "/api/config/config_entries/flow/" + flow.flow_id,
    { name, heater, target_sensor: "sensor.testbed_temperature", ac_mode: false, cold_tolerance: 0.3, hot_tolerance: 0.3 });
  if (step.type === "form") step = await ha("POST", "/api/config/config_entries/flow/" + step.flow_id, {}); // presets: none
  return step.result?.entry_id;
}
// What a save or a reload would change: the stored entry, its modified_at, the thermostat's state.
async function thermostatNow(ha, entry, climate) {
  const stored = JSON.parse(execFileSync("docker", ["exec", "ha-nova-testbed-ha", "cat", "/config/.storage/core.config_entries"], { encoding: "utf8" }));
  return JSON.stringify({ stored: stored.data.entries.find((e) => e.entry_id === entry),
    live: (await ha("GET", "/api/config/config_entries/entry?domain=generic_thermostat")).find((e) => e.entry_id === entry),
    state: (await ha("GET", "/api/states/" + climate)).last_updated });
}

export async function callChecks({ ha, ws, cli, apply, check, watchdogID, loadAutomations, planWrite }) {
  const watchdog = (await ha("GET", "/api/states")).find((s) => s.attributes?.id === watchdogID)?.entity_id;
  // Before any light listener exists: here a light call sets nothing off.
  const light = cli("plan", "call", "light.turn_off", "light.bed_light");
  check("an ordinary light call stays ordinary in this home", light.tier === "ordinary" && light.plan?.confirm === "none" && !names(light).includes(watchdog),
    "tier " + (light.tier ?? light.error?.code) + ", confirm " + light.plan?.confirm + ", consumers " + (names(light).join(",") || "-"));
  const cover = cli("plan", "call", "cover.close_cover", "cover.kitchen_window");
  check("a cover call still asks: the cover watchdog is its consumer", cover.tier === "typed" && names(cover).includes(watchdog) && codes(cover).includes("UNRESOLVED_TARGET"),
    "tier " + (cover.tier ?? cover.error?.code) + ", consumers " + (names(cover).join(",") || "-") + ", reasons " + (codes(cover).join(",") || "-"));
  await placeChecks({ ha, ws, cli, check, loadAutomations, planWrite });

  const thermostats = [], entries = [];
  try {
    thermostats.push(await thermostat(ha, "RC thermostat fan", FAN));
    await sleep(2000);
    const climateOf = async (entry) => (await ws("config/entity_registry/list", {})).find((r) => r.config_entry_id === entry && r.entity_id.startsWith("climate."))?.entity_id;
    const fanThermostat = await climateOf(thermostats[0]);
    const before = await thermostatNow(ha, thermostats[0], fanThermostat);
    const bounded = cli("plan", "call", "light.turn_off", "light.bed_light");
    check("with a UI thermostat whose heater is a fan, a light call stays ordinary", bounded.tier === "ordinary" && !names(bounded).includes(watchdog),
      "thermostat " + (fanThermostat ?? "-") + ", tier " + (bounded.tier ?? bounded.error?.code) + ", consumers " + (names(bounded).join(",") || "-"));
    await sleep(3000); // a reload would be scheduled by now
    const after = await thermostatNow(ha, thermostats[0], fanThermostat);
    check("reading its heater changes nothing: entry as stored, no reload", before === after, before === after ? "" : before + " → " + after);

    const shapes = {};
    for (const [name, filter] of Object.entries(ORACLE)) {
      shapes["rclane_oracle_" + name] = { alias: "RC oracle " + name, mode: "queued", triggers: [{ trigger: "event", event_type: "call_service", event_data: filter }],
        actions: [{ event: "rclane_oracle", event_data: { name } }] };
    }
    shapes.rclane_oracle_chain = { alias: "RC oracle chain", mode: "queued", triggers: [{ trigger: "event", event_type: "call_service", event_data: { domain: "light", service: "turn_off" } }],
      actions: [{ action: "cover.stop_cover", target: { entity_id: "cover.kitchen_window" } }] };
    shapes.rclane_oracle_relay = { alias: "RC oracle relay", mode: "queued", triggers: [{ trigger: "state", entity_id: RELAY }],
      actions: [{ action: "cover.stop_cover", target: { entity_id: "cover.kitchen_window" } }] };
    await ha("POST", "/api/config/scene/config/rclane_oracle_scene", { id: "rclane_oracle_scene", name: "RC oracle scene", entities: { "climate.hvac": { state: "heat", temperature: 23 } } });
    // HA NOVA calls a group's members itself, so the group's own calls run inside a script.
    await ha("POST", "/api/config/script/config/rclane_oracle_group", { alias: "RC oracle group", sequence: [{ action: "light.toggle", target: { entity_id: "light.living_room_group" } }] });
    await loadAutomations(shapes);
    await sleep(1000);
    const states = async () => Object.fromEntries((await ha("GET", "/api/states")).map((s) => [s.entity_id, s]));
    const automationOf = Object.fromEntries(Object.values(await states()).filter((s) => s.entity_id.startsWith("automation.") && s.attributes?.id?.startsWith("rclane_")).map((s) => [s.attributes.id, s.entity_id]));
    // Before any switch_as_x exists (a device-less one leaves the device-less demo fan unattributable),
    // the fan thermostat's calls are bounded: a fan call sets off no listener's own calls (a light call
    // would: the chain), so the climate listener is not its consumer.
    const climateListener = automationOf.rclane_oracle_climate_set_temperature;
    const control = cli("plan", "call", "fan.turn_off", FAN);
    thermostats.push(await thermostat(ha, "RC thermostat relay", RELAY));
    await sleep(2000);
    const followed = cli("plan", "call", "fan.turn_off", FAN);
    check("a second thermostat whose heater an automation follows makes every listener count", !names(control).includes(climateListener) && names(followed).includes(climateListener),
      "climate listener a consumer before " + names(control).includes(climateListener) + ", after " + names(followed).includes(climateListener));
    await ha("DELETE", "/api/config/config_entries/entry/" + thermostats.at(-1));
    thermostats.pop();
    for (const source of ["switch.template_porch_switch", RELAY]) {
      const flow = await ha("POST", "/api/config/config_entries/flow", { handler: "switch_as_x", show_advanced_options: false });
      entries.push((await ha("POST", "/api/config/config_entries/flow/" + flow.flow_id, { entity_id: source, target_domain: "light" })).result?.entry_id);
    }
    await sleep(1000);
    const all = await states();
    const wrapper = (source) => Object.values(all).find((s) => s.entity_id.startsWith("light.") && s.entity_id.includes(source.split(".")[1]))?.entity_id;
    const scene = Object.values(all).find((s) => s.attributes?.id === "rclane_oracle_scene")?.entity_id;
    const ids = (...names) => names.map((n) => "rclane_oracle_" + n);

    const cases = [
      { name: "a scene sets its climate member by attribute services", setup: ["climate", "set_temperature", { entity_id: "climate.hvac", temperature: 19 }],
        call: ["scene.turn_on", scene], expect: [ids("climate_set_temperature")] },
      { name: "a light group's toggle in a script turns its members on or off", call: ["script.turn_on", "script.rclane_oracle_group"],
        expect: [ids("light_turn_on", "light_turn_off")] },
      { name: "a switch_as_x toggle calls its template switch, which calls a light", call: ["light.toggle", wrapper("switch.template_porch_switch")],
        expect: [ids("switch_turn_on", "switch_turn_off"), ids("light_turn_on", "light_turn_off")] },
      { name: "a switch_as_x light switches its relay, whose automation stops a cover", call: ["light.toggle", wrapper(RELAY)],
        expect: [ids("switch_turn_on", "switch_turn_off"), ids("relay"), ids("cover_stop_cover"), [watchdogID]] },
      { name: "a listener's own call sets off the next", setup: ["light", "turn_on", { entity_id: "light.bed_light" }], call: ["light.turn_off", "light.bed_light"],
        expect: [ids("light_turn_off"), ids("chain"), ids("cover_stop_cover"), [watchdogID]] },
      { name: "a UI thermostat turns its fan heater on through homeassistant.turn_on",
        setup: [["mqtt", "publish", { topic: "testbed/climate/temperature", payload: "18", retain: true }], ["fan", "turn_off", { entity_id: FAN }]],
        call: ["climate.set_temperature", fanThermostat, "--set", "temperature=25", "--set", "hvac_mode=heat"], expect: [ids("homeassistant_turn_on")] },
    ];
    for (const c of cases) {
      for (const s of c.setup && Array.isArray(c.setup[0]) ? c.setup : c.setup ? [c.setup] : []) await ha("POST", "/api/services/" + s[0] + "/" + s[1], s[2]);
      await sleep(2000); // the setup's own listeners run outside the window
      const before = await states();
      const start = Math.max(...Object.values(before).map((s) => Date.parse(s.last_updated)));
      const plan = cli("plan", "call", ...c.call);
      const applied = plan.plan?.id ? await apply(plan) : {};
      const runs = async () => (await ws("trace/list", { domain: "automation" })).filter((t) => !t.not_triggered && Date.parse(t.timestamp.start) > start && automationOf[t.item_id]);
      let seen = [];
      for (let i = 0, quiet = 0; i < 30 && quiet < 4; i++, await sleep(500)) { // until 2 s without a new run
        const now = await runs();
        quiet = now.length === seen.length ? quiet + 1 : 0;
        seen = now;
      }
      const fired = [...new Set(seen.map((t) => t.item_id))];
      const missing = c.expect.filter((any) => !any.some((id) => fired.includes(id))).map((any) => any.join("|"));
      const unlisted = fired.filter((id) => !names(plan).includes(automationOf[id]));
      check("oracle: " + c.name + ": every automation it set off was a consumer", applied.status !== undefined && missing.length === 0 && unlisted.length === 0,
        "tier " + (plan.tier ?? plan.error?.code) + ", applied " + (applied.status ?? "-") + ", ran " + (fired.join(",") || "-") + (missing.length ? ", never ran " + missing.join(",") : "") + (unlisted.length ? ", not listed " + unlisted.join(",") : ""));
    }
    const unbounded = cli("plan", "call", "logbook.log", "--set", "name=rclane", "--set", "message=oracle");
    const listeners = Object.keys(shapes).filter((id) => id !== "rclane_oracle_relay").map((id) => automationOf[id]).concat(watchdog);
    check("a call whose calls can't be bounded counts every service-call listener", listeners.every((a) => a && names(unbounded).includes(a)),
      "tier " + (unbounded.tier ?? unbounded.error?.code) + ", listener consumers " + listeners.filter((a) => names(unbounded).includes(a)).length + "/" + listeners.length);
  } finally { // the bed as up.sh left it: no helpers of this run, the sensor at 21.5, the fan off
    for (const entry of [...entries, ...thermostats].filter(Boolean)) await ha("DELETE", "/api/config/config_entries/entry/" + entry).catch(() => {});
    await ha("POST", "/api/services/mqtt/publish", { topic: "testbed/climate/temperature", payload: "21.5", retain: true }).catch(() => {});
    await ha("POST", "/api/services/fan/turn_off", { entity_id: FAN }).catch(() => {});
  }
}

// Triggers that target an area, a floor, or a label (MAR-142): Home Assistant
// indexes such a trigger there, never under the light it covers. A light call
// that sets one off asks like a trigger naming the light, and so does an edit
// that adds that call to an automation (the edited one turns the bed light
// off by now). Everything it adds is removed again; nothing is applied.
async function placeChecks({ ha, ws, cli, check, loadAutomations, planWrite }) {
  const LABELED = "light.entrance_color_white_lights"; // no area, no label in the bed
  let label;
  const door = (target) => ({ alias: "RC places " + Object.keys(target)[0], mode: "single", triggers: [{ trigger: "light.turned_on", target }],
    actions: [{ action: "lock.unlock", target: { entity_id: "lock.entrance_door" } }] });
  const PLACES = {
    rclane_places_area: [door({ area_id: "kuche" }), "light.kitchen_lights"],
    rclane_places_floor: [door({ floor_id: "obergeschoss" }), "light.bed_light"],
  };
  try {
    label = await ws("config/label_registry/create", { name: "RC places probe" });
    await ws("config/entity_registry/update", { entity_id: LABELED, labels: [label.label_id] });
    PLACES.rclane_places_label = [door({ label_id: label.label_id }), LABELED];
    await loadAutomations(Object.fromEntries(Object.entries(PLACES).map(([id, [config]]) => [id, config])));
    const placed = Object.fromEntries((await ha("GET", "/api/states")).filter((s) => PLACES[s.attributes?.id]).map((s) => [s.attributes.id, s.entity_id]));
    const asks = (plan, consumer) => plan.tier === "typed" && plan.plan?.confirm === "code" && names(plan).includes(consumer);
    const detail = (plan) => "tier " + (plan.tier ?? plan.error?.code) + ", confirm " + plan.plan?.confirm + ", consumers " + (names(plan).join(",") || "-");
    for (const [id, [, lamp]] of Object.entries(PLACES)) {
      const plan = cli("plan", "call", "light.turn_on", lamp);
      check("a light call whose " + id.slice(14) + " trigger unlocks a door asks with a code", asks(plan, placed[id]), detail(plan));
    }
    const edit = planWrite([{ action: "light.turn_off", target: { entity_id: "light.bed_light" } }, { action: "light.turn_on", target: { entity_id: "light.kitchen_lights" } }]);
    check("an edit that adds the kitchen light beside the area-triggered unlock asks with a code", asks(edit, placed.rclane_places_area), detail(edit));
  } finally {
    for (const id of Object.keys(PLACES)) await ha("DELETE", "/api/config/automation/config/" + id).catch(() => {});
    if (label) {
      await ws("config/entity_registry/update", { entity_id: LABELED, labels: [] });
      await ws("config/label_registry/delete", { label_id: label.label_id });
    }
  }
}
