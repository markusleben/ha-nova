// The maintainer's heater pattern (MAR-262, MAR-263; invented names), run by
// calls.mjs right after the M rows on a bed with file access: the YAML thermostat's template
// heater around the demo fan, with the fan (it has no device) in an area on a floor
// that each hold a script, a valve automation on a state trigger, a panel
// blueprint instance on state_changed events (inputs set, left empty, and a
// relay named through a trigger variable), and the shutter watchdog with its
// repeat/choose script (cli/testdata/mar209). A light call asks nothing; a
// shutter, the garage, and an unlock still ask. Then each thing that does
// react to the heater makes the light ask, and the plan's detail names it: a
// running script in the area, a panel whose relay comes from a template
// (Home Assistant renders it with device_entities, HA NOVA can't), and a state
// trigger that names the heater by its registry entry id. Everything it adds
// is removed and the fan goes back to its area.
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { loaded } from "./mrow.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const BLUEPRINTS = join(HERE, ".state", "config", "blueprints", "automation", "testbed_quiet");
const FIXTURE = join(HERE, "../../../cli/testdata/mar209");
const THERMOSTAT = "climate.bath_thermostat", FAN = "fan.ceiling_fan", HEATER = "switch.bath_heater_safe";
const WATCHDOG = "shutter_watchdog_cover_groups", WATCHDOG_SCRIPT = "shutter_watchdog_check_group";
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const cause = (plan) => (plan.reasons ?? []).find((r) => r.code === "HELPER_CALLS_UNREAD");
const codes = (plan) => (plan.reasons ?? []).map((r) => r.code + " " + (r.entity_id ?? "")).sort().join(", ");
const describe = (plan) => "tier " + (plan.tier ?? plan.error?.code) + ", confirm " + plan.plan?.confirm + ", cause " + JSON.stringify(cause(plan) ?? null) + ", reasons " + codes(plan);
const fixture = (name) => JSON.parse(readFileSync(join(FIXTURE, name), "utf8"));

// A panel blueprint as the maintainer's: one state_changed trigger per entity
// input (an empty one defaults to []) and one on its relay through a trigger
// variable: a plain input (static), or a template over the panel's device.
const RELAY = {
  static: "!input relay",
  dynamic: "\"{{ device_entities('quiet_panel') | select('search', 'relay_1') | list | last | default('switch.quiet_panel_relay_1') }}\"",
};
const panel = (relay) => [
  "blueprint:",
  "  name: Testbed quiet panel (" + relay + ")",
  "  domain: automation",
  "  input:",
  "    chip01: {default: [], selector: {entity: {}}}",
  "    chip02: {default: [], selector: {entity: {}}}",
  "    room_climate: {default: [], selector: {entity: {}}}",
  "    relay: {default: \"\", selector: {text: {}}}",
  "trigger_variables:",
  "  relay_entity: " + RELAY[relay],
  "mode: queued",
  "triggers:",
  "  - {trigger: event, event_type: state_changed, event_data: {entity_id: !input chip01}}",
  "  - {trigger: event, event_type: state_changed, event_data: {entity_id: !input chip02}}",
  "  - {trigger: event, event_type: state_changed, event_data: {entity_id: !input room_climate}}",
  "  - {trigger: event, event_type: state_changed, event_data: {entity_id: \"{{ relay_entity }}\"}}",
  "actions:",
  "  - {action: logbook.log, data: {name: Testbed panel, message: \"{{ trigger.event.data.entity_id }}\"}}",
  "",
].join("\n");
const panelAt = (relay) => ({ alias: "Quiet kitchen panel", use_blueprint: { path: "testbed_quiet/" + relay + ".yaml",
  input: { chip01: "sensor.outside_temperature", chip02: [], room_climate: [], relay: "switch.quiet_panel_relay_1" } } });

export async function quietPatternChecks({ ha, ws, cli, check, loadAutomations }) {
  const registry = await ws("config/entity_registry/list", {});
  const fan = registry.find((r) => r.entity_id === FAN), heaterID = registry.find((r) => r.entity_id === HEATER)?.id;
  const floor = (await ws("config/floor_registry/list", {})).find((f) => f.name === "Obergeschoss")?.floor_id;
  const automations = ["quiet_valve_guard", "quiet_kitchen_panel", "quiet_heater_follow", WATCHDOG];
  const scripts = ["quiet_morning_greeting", "quiet_boiler_restart", WATCHDOG_SCRIPT];
  if (!(await ha("GET", "/api/states")).some((s) => s.entity_id === THERMOSTAT)) {
    console.log("WARN  the MAR-262 heater pattern needs the YAML thermostat (TESTBED_FILES=1)");
    return;
  }
  const light = () => cli("plan", "call", "light.turn_off", "light.bed_light");
  let area;
  try {
    for (const [what, ok] of [["the fan's registry row", fan], ["the heater's registry row", heaterID], ["the Obergeschoss floor", floor]]) {
      if (!ok) throw new Error("the bed lacks " + what);
    }
    area = (await ws("config/area_registry/create", { name: "Quiet Bad", floor_id: floor })).area_id;
    await ws("config/entity_registry/update", { entity_id: FAN, area_id: area });
    mkdirSync(BLUEPRINTS, { recursive: true });
    for (const relay of Object.keys(RELAY)) writeFileSync(join(BLUEPRINTS, relay + ".yaml"), panel(relay));
    await ha("POST", "/api/config/script/config/quiet_morning_greeting", { alias: "Quiet morning greeting", mode: "single",
      sequence: [{ wait_template: "{{ is_state('" + FAN + "', 'on') }}", timeout: "00:05:00" }, { action: "fan.turn_off", target: { area_id: area } }] });
    await ha("POST", "/api/config/script/config/quiet_boiler_restart", { alias: "Quiet boiler restart", sequence: [{ delay: 1 }, { action: "fan.turn_off", target: { floor_id: floor } }] });
    await ha("POST", "/api/config/script/config/" + WATCHDOG_SCRIPT, fixture("script.json"));
    await loaded(ha, ...scripts.map((s) => "script." + s));
    await loadAutomations({
      quiet_valve_guard: { alias: "Quiet valve guard", mode: "parallel",
        triggers: [{ trigger: "state", entity_id: ["climate.ecobee", "climate.heatpump"], to: "auto" }],
        conditions: [{ condition: "template", value_template: "{{ trigger.to_state.attributes.get('hvac_modes') is not none }}" }],
        actions: [{ action: "climate.set_temperature", continue_on_error: true, target: { entity_id: "{{ trigger.entity_id }}" }, data: { temperature: 5 } }] },
      quiet_kitchen_panel: panelAt("static"),
      [WATCHDOG]: fixture("automation.json"),
    });

    const calm = light();
    check("MAR-262 pattern: scripts at rest in the heater's area and floor, a valve automation, a panel on literal and empty inputs: the light asks nothing",
      calm.tier === "ordinary" && calm.plan?.confirm === "none" && !cause(calm), describe(calm));
    for (const [what, args] of [["a shutter", ["cover.close_cover", "cover.kitchen_window"]], ["the garage", ["cover.open_cover", "cover.garage_door"]], ["an unlock", ["lock.unlock", "lock.entrance_door"]]]) {
      const plan = cli("plan", "call", ...args);
      check("MAR-262 pattern: " + what + " still asks", plan.tier === "typed", describe(plan));
    }

    await ha("POST", "/api/services/script/turn_on", { entity_id: "script.quiet_morning_greeting" });
    for (let tries = 0; tries < 20 && (await ha("GET", "/api/states/script.quiet_morning_greeting")).state !== "on"; tries++) await sleep(250);
    const running = light();
    check("MAR-262 pattern: a script running in the heater's area makes the light ask and is named",
      running.tier === "typed" && (cause(running)?.detail ?? "").includes("running script script.quiet_morning_greeting"), describe(running));
    await ha("POST", "/api/services/script/turn_off", { entity_id: "script.quiet_morning_greeting" });

    await loadAutomations({ quiet_kitchen_panel: panelAt("dynamic") });
    const dynamic = light();
    check("MAR-262 pattern: a panel relay from a device template makes the light ask and names the panel", dynamic.tier === "typed"
      && /^helper_listened: climate\.bath_thermostat: couldn't rule out a reaction from automation\.\S+ \(templated state_changed trigger\)$/.test(cause(dynamic)?.detail ?? ""), describe(dynamic));
    await loadAutomations({ quiet_kitchen_panel: panelAt("static") });

    await loadAutomations({ quiet_heater_follow: { alias: "Quiet heater follow", triggers: [{ trigger: "state", entity_id: heaterID }],
      actions: [{ action: "logbook.log", data: { name: "Testbed", message: "heater changed" } }] } });
    const byID = light();
    check("MAR-263: a state trigger naming the heater by its registry entry id makes the light ask and is named", byID.tier === "typed"
      && /^heater_not_plain: switch\.bath_heater_safe: trigger of automation\./.test(cause(byID)?.detail ?? ""), describe(byID));
  } finally { // every step on its own; then the bed must read as before
    const failed = [];
    const attempt = async (what, step) => { try { await step(); } catch (err) { failed.push(what + ": " + err.message); } };
    const gone = (err) => { if (!/HTTP 404|HTTP 400/.test(err.message)) throw err; };
    for (const id of automations) await attempt("delete automation " + id, () => ha("DELETE", "/api/config/automation/config/" + id).catch(gone));
    for (const id of scripts) await attempt("delete script " + id, () => ha("DELETE", "/api/config/script/config/" + id).catch(gone));
    await attempt("remove the blueprints", async () => rmSync(BLUEPRINTS, { recursive: true, force: true }));
    if (area) await attempt("put the fan back", () => ws("config/entity_registry/update", { entity_id: FAN, area_id: fan.area_id ?? null }));
    if (area) await attempt("delete the area", () => ws("config/area_registry/delete", { area_id: area }));
    await attempt("read back", async () => {
      let left = [];
      for (let tries = 0; tries < 20; tries++, await sleep(500)) {
        left = (await ha("GET", "/api/states")).filter((s) => automations.includes(s.attributes?.id) || scripts.some((id) => s.entity_id === "script." + id)).map((s) => s.entity_id);
        if (!left.length) return;
      }
      throw new Error("left " + left.join(","));
    });
    check("MAR-262 pattern: the bed reads as before", !failed.length, failed.join("; "));
  }
}
