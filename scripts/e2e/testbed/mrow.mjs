// The M row of the 0.32 friction table (MAR-209, D6; release gate): a copy
// of the watchdog in Markus's home, on every call_service without a filter,
// whose script opens, closes, and positions covers it picks by template. A
// light call is rated by every cover that template could pick. With the
// bed's garage and gate covers it still asks, with an access cover as the
// reason; with every cover a shutter (the template pool cover disabled, the
// others classed shutter in the registry) it asks nothing, in the CLI and in
// the gated App's check. Everything it changes is put back. The templates
// render "none" here, so a run the bed's own calls set off moves nothing.
import { relay } from "./phone.mjs";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const names = (plan) => (plan.consumers ?? []).map((c) => c.automation);
const codes = (plan) => (plan.reasons ?? []).map((r) => r.code);
const SAFE = new Set(["awning", "blind", "curtain", "damper", "shade", "shutter", "window"]); // cli/tier_rules.go nonAccessCoverClasses
const WATCHDOG = "rclane_m_watchdog", SCRIPT = "rclane_m_check_group";
const picked = { entity_id: "{{ 'none' }}" };

// measure (ask-count.mjs): runs at each of the two homes, "garage" and "shutter".
export async function mRowChecks({ ha, ws, cli, check, loadAutomations, gated, measure }) {
  const classed = [], disabled = [];
  const covers = async () => (await ha("GET", "/api/states")).filter((s) => s.entity_id.startsWith("cover."));
  try {
    await ha("POST", "/api/config/script/config/" + SCRIPT, { alias: "RC M check group", mode: "queued", sequence: [
      { action: "cover.open_cover", target: picked }, { action: "cover.close_cover", target: picked },
      { action: "cover.set_cover_position", target: picked, data: { position: "{{ 50 }}" } }] });
    await loadAutomations({ [WATCHDOG]: { alias: "RC M watchdog", mode: "queued", triggers: [{ trigger: "event", event_type: "call_service" }],
      conditions: [{ condition: "template", value_template: "{{ trigger.event.data.domain not in ['script', 'cover'] }}" }],
      actions: [{ action: "script." + SCRIPT }] } });
    const watchdog = (await ha("GET", "/api/states")).find((s) => s.attributes?.id === WATCHDOG)?.entity_id;
    const light = () => cli("plan", "call", "light.turn_off", "light.bed_light");
    const detail = (plan) => "tier " + (plan.tier ?? plan.error?.code) + ", confirm " + plan.plan?.confirm + ", reasons " + (codes(plan).join(",") || "-") + ", consumers " + (names(plan).join(",") || "-");
    const gate = async () => relay("/core", { method: "POST", path: "/api/services/light/turn_off", body: { entity_id: "light.bed_light" } });

    const risky = light();
    check("M: beside the watchdog copy, a light call still asks while it could pick a garage cover", risky.tier === "typed" && names(risky).includes(watchdog) && codes(risky).includes("ACCESS_COVER"), detail(risky));
    if (gated) {
      const held = await gate();
      check("M: the gated Relay holds that light call", held.status === 428 && held.error?.code === "GATE_APPROVAL_REQUIRED", "HTTP " + held.status + " " + (held.error?.code ?? ""));
    }
    await measure?.("garage");

    const entries = await ws("config/entity_registry/list", {});
    for (const state of await covers()) {
      const row = entries.find((r) => r.entity_id === state.entity_id);
      if (!row) throw new Error(state.entity_id + " has no registry row: the bed can't stage a home without access covers");
      if (row.platform === "template") {
        disabled.push(state.entity_id); // before the change: a lost answer is still put back
        await ws("config/entity_registry/update", { entity_id: state.entity_id, disabled_by: "user" }); // removes it from the states at once
      } else if (!SAFE.has(state.attributes?.device_class)) {
        classed.push([state.entity_id, (await ws("config/entity_registry/get", { entity_id: state.entity_id })).device_class ?? null]);
        await ws("config/entity_registry/update", { entity_id: state.entity_id, device_class: "shutter" });
      }
    }
    for (let tries = 0; tries < 20; tries++, await sleep(500)) {
      const now = await covers();
      if (now.every((s) => SAFE.has(s.attributes?.device_class) && !disabled.includes(s.entity_id))) break;
    }
    const calm = light();
    check("M (release gate): with every cover a shutter, a light call beside the watchdog copy asks nothing", calm.tier === "ordinary" && calm.plan?.confirm === "none" && names(calm).includes(watchdog) && !codes(calm).includes("UNRESOLVED_TARGET"), detail(calm));
    if (gated) {
      const passed = await gate();
      check("M (release gate): the gated Relay passes that light call with no ask", passed.status === 200, "HTTP " + passed.status + " " + (passed.error?.code ?? ""));
    }
    await measure?.("shutter");
  } finally { // every step on its own; then the bed must read as before
    const failed = [];
    const attempt = async (what, step) => { try { await step(); } catch (err) { failed.push(what + ": " + err.message); } };
    await attempt("delete the watchdog copy", () => ha("DELETE", "/api/config/automation/config/" + WATCHDOG));
    await attempt("delete its script", () => ha("DELETE", "/api/config/script/config/" + SCRIPT));
    for (const [entity_id, device_class] of classed) await attempt("class of " + entity_id, () => ws("config/entity_registry/update", { entity_id, device_class }));
    for (const entity_id of disabled) await attempt("enable " + entity_id, () => ws("config/entity_registry/update", { entity_id, disabled_by: null }));
    if (disabled.length) await attempt("template reload", () => ha("POST", "/api/services/template/reload", {}));
    await attempt("read back", async () => {
      let ok = false, detail = "";
      for (let tries = 0; tries < 20 && !ok; tries++, ok || await sleep(500)) {
        const states = await ha("GET", "/api/states");
        const missing = disabled.filter((id) => !states.some((s) => s.entity_id === id));
        const wrong = [];
        for (const [entity_id, device_class] of classed) if (((await ws("config/entity_registry/get", { entity_id })).device_class ?? null) !== device_class) wrong.push(entity_id);
        const left = states.filter((s) => [WATCHDOG, SCRIPT].includes(s.attributes?.id) || s.entity_id === "script." + SCRIPT).map((s) => s.entity_id);
        ok = !missing.length && !wrong.length && !left.length;
        detail = "missing " + (missing.join(",") || "-") + ", class not restored " + (wrong.join(",") || "-") + ", left " + (left.join(",") || "-");
      }
      if (!ok) throw new Error(detail);
    });
    check("M: the bed reads as before (covers, classes, no watchdog copy)", !failed.length, failed.join("; "));
  }
}
