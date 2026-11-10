// Service-call listeners in step h (MAR-127, scripts/e2e/testbed/calm.mjs): a
// listener on call_service filtered to covers is set off only by a cover call.
// Then an oracle on this real Home Assistant: for each plan, every rclane
// automation that really ran after it was applied must be among the plan's
// consumers. Runs come from Home Assistant's traces (each run, not only the
// latest), started after the bed's last change before the apply (its own
// clock; the bed is otherwise quiet), read until a settle without new runs;
// and each case's expected automations must run, so a check can't pass by
// running nothing.
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
};
const RELAY = "switch.decorative_lights"; // a demo switch on its own device, switched by a switch_as_x light

export async function callChecks({ ha, ws, cli, check, watchdogID, loadAutomations }) {
  const watchdog = (await ha("GET", "/api/states")).find((s) => s.attributes?.id === watchdogID)?.entity_id;
  // Before any light listener exists: here a light call sets nothing off.
  const light = cli("plan", "call", "light.turn_off", "light.bed_light");
  check("an ordinary light call stays ordinary in this home", light.tier === "ordinary" && light.plan?.confirm === "none" && !names(light).includes(watchdog),
    "tier " + (light.tier ?? light.error?.code) + ", confirm " + light.plan?.confirm + ", consumers " + (names(light).join(",") || "-"));
  const cover = cli("plan", "call", "cover.close_cover", "cover.kitchen_window");
  check("a cover call still asks: the cover watchdog is its consumer", cover.tier === "typed" && names(cover).includes(watchdog) && codes(cover).includes("UNRESOLVED_TARGET"),
    "tier " + (cover.tier ?? cover.error?.code) + ", consumers " + (names(cover).join(",") || "-") + ", reasons " + (codes(cover).join(",") || "-"));

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
  const entries = [];
  for (const source of ["switch.template_porch_switch", RELAY]) {
    const flow = await ha("POST", "/api/config/config_entries/flow", { handler: "switch_as_x", show_advanced_options: false });
    entries.push((await ha("POST", "/api/config/config_entries/flow/" + flow.flow_id, { entity_id: source, target_domain: "light" })).result?.entry_id);
  }
  await sleep(1000);
  const states = async () => Object.fromEntries((await ha("GET", "/api/states")).map((s) => [s.entity_id, s]));
  const all = await states();
  const automationOf = Object.fromEntries(Object.values(all).filter((s) => s.entity_id.startsWith("automation.") && s.attributes?.id?.startsWith("rclane_")).map((s) => [s.attributes.id, s.entity_id]));
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
  ];
  for (const c of cases) {
    if (c.setup) await ha("POST", "/api/services/" + c.setup[0] + "/" + c.setup[1], c.setup[2]);
    await sleep(2000); // the setup's own listeners run outside the window
    const before = await states();
    const start = Math.max(...Object.values(before).map((s) => Date.parse(s.last_updated)));
    const plan = cli("plan", "call", ...c.call);
    const applied = plan.plan?.id ? cli("apply", "--plan", plan.plan.id, ...(plan.plan.code ? ["--confirm", plan.plan.code] : [])) : {};
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
  for (const entry of entries.filter(Boolean)) await ha("DELETE", "/api/config/config_entries/entry/" + entry);
}
