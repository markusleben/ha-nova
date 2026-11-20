// The M row of the friction table (MAR-209; release gate): the watchdog in
// Markus's home and its script, the sanitized copy in cli/testdata/mar209 (on
// call_service filtered to cover; the script waits, notifies, logs, and moves
// covers it picks by template). It runs twice: with no logbook stream, and
// with one held open as Home Assistant's logbook page holds it (one more
// call_service listener no automation explains). Both times a light call asks
// nothing and the watchdog isn't its consumer (decision A,
// docs/work/2026-11-18-0.33-spec.md §1.2), a cover call asks once (the
// watchdog matches, and its templated target could pick the garage), and the
// garage asks once; the gated App's check passes the light call too (the
// Go tests run every case through it as well). The watchdog's
// helpers (input_boolean.shutter_watchdog_enabled) don't exist here, so a run
// the bed's calls set off stops at its first condition. Everything it adds
// is removed.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { read, relay } from "./phone.mjs";

const HA = "http://127.0.0.1:18123";
const FIXTURE = join(dirname(fileURLToPath(import.meta.url)), "../../../cli/testdata/mar209");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const names = (plan) => (plan.consumers ?? []).map((c) => c.automation);
const codes = (plan) => (plan.reasons ?? []).map((r) => r.code);
const notes = (plan) => plan.coverage?.notes ?? [];
const shown = (plan) => (plan.reasons ?? []).map((r) => r.code === "SCAN_FAILED" ? r.code + "(" + r.entity_id + " " + r.detail + ")" : r.code);
const WATCHDOG = "shutter_watchdog_cover_groups", SCRIPT = "shutter_watchdog_check_group";
const fixture = (name) => JSON.parse(readFileSync(join(FIXTURE, name), "utf8"));

// loaded: a script or scene the bed just wrote is there only after Home
// Assistant's own reload. Waits for each entity (script.<id> / its config id),
// or names the bed's reload as the failure, never HA NOVA's read.
export async function loaded(ha, ...ids) {
  for (let tries = 0; tries < 30; tries++, await sleep(500)) {
    const states = await ha("GET", "/api/states");
    if (ids.every((id) => states.some((s) => s.entity_id === id || s.attributes?.id === id.split(".")[1]))) return;
  }
  throw new Error(ids.join(", ") + " never loaded in Home Assistant (the bed's reload, not HA NOVA)");
}

// The logbook page's stream: an unfiltered logbook/event_stream subscription.
async function openLogbook() {
  const socket = new WebSocket(HA.replace(/^http/, "ws") + "/api/websocket");
  await new Promise((resolve, reject) => {
    socket.onerror = () => reject(new Error("Home Assistant's WebSocket failed"));
    socket.onmessage = (event) => {
      const m = JSON.parse(event.data);
      if (m.type === "auth_required") socket.send(JSON.stringify({ type: "auth", access_token: read("ha-llat") }));
      else if (m.type === "auth_ok") socket.send(JSON.stringify({ id: 1, type: "logbook/event_stream", start_time: new Date().toISOString() }));
      else if (m.id === 1 && m.type === "result") m.success ? resolve() : reject(new Error("logbook/event_stream: " + JSON.stringify(m.error)));
    };
  });
  return socket;
}

// measure (ask-count.mjs): runs once per listener state, "no logbook" and "logbook open".
export async function mRowChecks({ ha, cli, check, loadAutomations, gated, measure }) {
  let logbook;
  try {
    await ha("POST", "/api/config/script/config/" + SCRIPT, fixture("script.json"));
    await loaded(ha, "script." + SCRIPT);
    await loadAutomations({ [WATCHDOG]: fixture("automation.json") });
    const watchdog = (await ha("GET", "/api/states")).find((s) => s.attributes?.id === WATCHDOG)?.entity_id;
    const listeners = async () => (await ha("GET", "/api/events")).find((e) => e.event === "call_service")?.listener_count ?? 0;
    const detail = (plan) => "tier " + (plan.tier ?? plan.error?.code) + ", confirm " + plan.plan?.confirm + ", reasons " + (shown(plan).join(",") || "-") + ", notes " + (notes(plan).join(",") || "-") + ", consumers " + (names(plan).join(",") || "-");

    for (const stage of ["no logbook", "logbook open"]) {
      if (stage === "logbook open") {
        const before = await listeners();
        logbook = await openLogbook();
        let after = before;
        for (let tries = 0; tries < 20 && after <= before; tries++, await sleep(250)) after = await listeners();
        check("M (" + stage + "): the stream adds a call_service listener no automation explains", after > before, "call_service listeners " + before + " → " + after);
      }
      const light = cli("plan", "call", "light.turn_off", "light.bed_light");
      check("M (" + stage + ", release gate): a light call beside the watchdog asks nothing", light.tier === "ordinary" && light.plan?.confirm === "none" && !names(light).includes(watchdog)
        && notes(light).includes("UNEXPLAINED_LISTENER") === (stage === "logbook open"), detail(light));
      const cover = cli("plan", "call", "cover.close_cover", "cover.kitchen_window");
      check("M (" + stage + "): a shutter call asks once, the watchdog could pick the garage", cover.tier === "typed" && names(cover).includes(watchdog), detail(cover));
      const garage = cli("plan", "call", "cover.open_cover", "cover.garage_door");
      check("M (" + stage + "): the garage asks once", garage.tier === "typed" && codes(garage).includes("ACCESS_COVER"), detail(garage));
      if (gated) {
        const passed = await relay("/core", { method: "POST", path: "/api/services/light/turn_off", body: { entity_id: "light.bed_light" } });
        check("M (" + stage + ", release gate): the gated Relay passes that light call with no ask", passed.status === 200, "HTTP " + passed.status + " " + (passed.error?.code ?? ""));
      }
      await measure?.(stage);
    }
  } finally { // every step on its own; then the bed must read as before
    const failed = [];
    const attempt = async (what, step) => { try { await step(); } catch (err) { failed.push(what + ": " + err.message); } };
    logbook?.close();
    await attempt("delete the watchdog", () => ha("DELETE", "/api/config/automation/config/" + WATCHDOG));
    await attempt("delete its script", () => ha("DELETE", "/api/config/script/config/" + SCRIPT));
    await attempt("read back", async () => {
      let left = [];
      for (let tries = 0; tries < 20; tries++, await sleep(500)) {
        left = (await ha("GET", "/api/states")).filter((s) => [WATCHDOG, SCRIPT].includes(s.attributes?.id) || s.entity_id === "script." + SCRIPT).map((s) => s.entity_id);
        if (!left.length) return;
      }
      throw new Error("left " + left.join(","));
    });
    check("M: the bed reads as before (no watchdog, no script)", !failed.length, failed.join("; "));
  }
}
