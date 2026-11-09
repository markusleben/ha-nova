// Service-call listeners in step h (MAR-127, scripts/e2e/testbed/calm.mjs): a
// listener on call_service filtered to covers is set off only by a cover call.
// Then an oracle on this real Home Assistant: for each plan, every listener
// its calls actually set off must be among the plan's consumers. A fired
// listener counts only when the context its run started from was made after
// the bed's last change before the apply (a context id is a ULID: its first
// ten characters are Home Assistant's clock; the bed is otherwise quiet), and
// each case's expected listeners must fire, so a check can't pass by firing
// nothing.
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const names = (plan) => (plan.consumers ?? []).map((c) => c.automation);
const codes = (plan) => (plan.reasons ?? []).map((r) => r.code);
const born = (ulid) => [...(ulid ?? "").slice(0, 10)].reduce((t, c) => t * 32 + "0123456789ABCDEFGHJKMNPQRSTVWXYZ".indexOf(c), 0);

// Listener automations on call_service, each toggling its own helper.
const ORACLE = {
  climate_set_temperature: { domain: "climate", service: "set_temperature" },
  light_turn_on: { domain: "light", service: "turn_on" },
  light_turn_off: { domain: "light", service: "turn_off" },
  switch_turn_on: { domain: "switch", service: "turn_on" },
  switch_turn_off: { domain: "switch", service: "turn_off" },
  cover_stop_cover: { domain: "cover", service: "stop_cover" },
};

export async function callChecks({ ha, ws, cli, check, watchdogID, loadAutomations }) {
  const watchdog = (await ha("GET", "/api/states")).find((s) => s.attributes?.id === watchdogID)?.entity_id;
  // Before any light listener exists: here a light call sets nothing off.
  const light = cli("plan", "call", "light.turn_off", "light.bed_light");
  check("an ordinary light call stays ordinary in this home", light.tier === "ordinary" && light.plan?.confirm === "none" && !names(light).includes(watchdog),
    "tier " + (light.tier ?? light.error?.code) + ", confirm " + light.plan?.confirm + ", consumers " + (names(light).join(",") || "-"));
  const cover = cli("plan", "call", "cover.close_cover", "cover.kitchen_window");
  check("a cover call still asks: the cover watchdog is its consumer", cover.tier === "typed" && names(cover).includes(watchdog) && codes(cover).includes("UNRESOLVED_TARGET"),
    "tier " + (cover.tier ?? cover.error?.code) + ", consumers " + (names(cover).join(",") || "-") + ", reasons " + (codes(cover).join(",") || "-"));

  for (const name of Object.keys(ORACLE)) await ws("input_boolean/create", { name: "rclane oracle " + name });
  const shapes = {};
  for (const [name, filter] of Object.entries(ORACLE)) {
    shapes["rclane_oracle_" + name] = { alias: "RC oracle " + name, mode: "queued", triggers: [{ trigger: "event", event_type: "call_service", event_data: filter }],
      actions: [{ action: "input_boolean.toggle", target: { entity_id: "input_boolean.rclane_oracle_" + name } }] };
  }
  shapes.rclane_oracle_chain = { alias: "RC oracle chain", mode: "queued", triggers: [{ trigger: "event", event_type: "call_service", event_data: { domain: "light", service: "turn_off" } }],
    actions: [{ action: "cover.stop_cover", target: { entity_id: "cover.kitchen_window" } }] };
  await ha("POST", "/api/config/scene/config/rclane_oracle_scene", { id: "rclane_oracle_scene", name: "RC oracle scene", entities: { "climate.hvac": { state: "heat", temperature: 23 } } });
  // HA NOVA calls a group's members itself, so the group's own calls run inside a script.
  await ha("POST", "/api/config/script/config/rclane_oracle_group", { alias: "RC oracle group", sequence: [{ action: "light.toggle", target: { entity_id: "light.living_room_group" } }] });
  await loadAutomations(shapes);
  const flow = await ha("POST", "/api/config/config_entries/flow", { handler: "switch_as_x", show_advanced_options: false });
  const made = await ha("POST", "/api/config/config_entries/flow/" + flow.flow_id, { entity_id: "switch.template_porch_switch", target_domain: "light" });
  const states = async () => Object.fromEntries((await ha("GET", "/api/states")).map((s) => [s.entity_id, s]));
  let all = await states();
  const oracleOf = Object.fromEntries(Object.keys(ORACLE).map((name) => [Object.values(all).find((s) => s.attributes?.id === "rclane_oracle_" + name)?.entity_id, name]));
  const wrapper = Object.values(all).find((s) => s.entity_id.startsWith("light.") && s.entity_id.includes("porch_switch"))?.entity_id;
  const scene = Object.values(all).find((s) => s.attributes?.id === "rclane_oracle_scene")?.entity_id;

  const cases = [
    { name: "a scene sets its climate member by attribute services", setup: ["climate", "set_temperature", { entity_id: "climate.hvac", temperature: 19 }],
      call: ["scene.turn_on", scene], expect: [["climate_set_temperature"]] },
    { name: "a light group's toggle in a script turns its members on or off", call: ["script.turn_on", "script.rclane_oracle_group"],
      expect: [["light_turn_on", "light_turn_off"]] },
    { name: "a switch_as_x toggle calls its template switch, which calls a light", call: ["light.toggle", wrapper],
      expect: [["switch_turn_on", "switch_turn_off"], ["light_turn_on", "light_turn_off"]] },
    { name: "a listener's own call sets off the next", setup: ["light", "turn_on", { entity_id: "light.bed_light" }], call: ["light.turn_off", "light.bed_light"],
      expect: [["light_turn_off"], ["cover_stop_cover"]] },
  ];
  for (const c of cases) {
    if (c.setup) await ha("POST", "/api/services/" + c.setup[0] + "/" + c.setup[1], c.setup[2]);
    await sleep(2000); // the setup's own listeners run outside the window
    const before = await states();
    const plan = cli("plan", "call", ...c.call);
    const start = Math.max(...Object.values(before).map((s) => Date.parse(s.last_updated))) + 1;
    const applied = plan.plan?.id ? cli("apply", "--plan", plan.plan.id, ...(plan.plan.code ? ["--confirm", plan.plan.code] : [])) : {};
    let fired = [];
    for (let i = 0; i < 16; i++, await sleep(500)) {
      all = await states();
      fired = Object.keys(oracleOf).filter((id) => all[id]?.last_updated !== before[id]?.last_updated && born(all[id]?.context?.parent_id) >= start);
      if (c.expect.every((any) => any.some((n) => fired.includes(Object.keys(oracleOf).find((id) => oracleOf[id] === n))))) break;
    }
    const missing = c.expect.filter((any) => !any.some((n) => fired.includes(Object.keys(oracleOf).find((id) => oracleOf[id] === n)))).map((any) => any.join("|"));
    const unlisted = fired.map((id) => Object.values(all).find((s) => s.attributes?.id === "rclane_oracle_" + oracleOf[id])?.entity_id).filter((a) => !names(plan).includes(a));
    check("oracle: " + c.name + ": every listener it set off was a consumer", applied.status !== undefined && missing.length === 0 && unlisted.length === 0,
      "tier " + (plan.tier ?? plan.error?.code) + ", applied " + (applied.status ?? "-") + ", fired " + (fired.map((id) => oracleOf[id]).join(",") || "-") + (missing.length ? ", never fired " + missing.join(",") : "") + (unlisted.length ? ", not listed " + unlisted.join(",") : ""));
  }
  const unbounded = cli("plan", "call", "logbook.log", "--set", "name=rclane", "--set", "message=oracle");
  const oracles = Object.values(all).filter((s) => s.attributes?.id?.startsWith("rclane_oracle_") && s.entity_id.startsWith("automation.")).map((s) => s.entity_id);
  check("a call whose calls can't be bounded counts every service-call listener", oracles.length > 0 && oracles.every((a) => names(unbounded).includes(a)),
    "tier " + (unbounded.tier ?? unbounded.error?.code) + ", oracle consumers " + oracles.filter((a) => names(unbounded).includes(a)).length + "/" + oracles.length);
  if (made.result?.entry_id) await ha("DELETE", "/api/config/config_entries/entry/" + made.result.entry_id);
}
