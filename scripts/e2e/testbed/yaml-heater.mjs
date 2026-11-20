// YAML thermostats (MAR-245, docs/work/2026-11-20-yaml-helper-read-spec.md)
// on a bed with file access (TESTBED_FILES=1, where up.sh adds a YAML
// thermostat whose heater is a YAML template switch around the demo fan).
// The M rows already run beside it: a light call asks nothing, in the CLI
// and the gated Relay. Here, after calm's oracle automations (which keep the
// light typed for their own reasons), the YAML is read with no cause named
// and stays byte-identical. Then the proof that the engine reads only what
// Home Assistant runs: an older copy moved over the heater's file, and a
// hard link to it, each move its ctime past Home Assistant's start, so the
// plan names the thermostat (yaml_changed); a restart (Home Assistant loads
// the file again) clears it. Runs last in calm.mjs: it restarts Home
// Assistant twice.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const THERMOSTAT = "climate.bath_thermostat";
const FILE = "/config/yaml_heater_switch.yaml";
const TOKEN = readFileSync(join(dirname(fileURLToPath(import.meta.url)), ".state", "relay-token"), "utf8").trim();
const health = async () => (await (await fetch("http://127.0.0.1:18791/health", { headers: { authorization: `Bearer ${TOKEN}` } })).json().catch(() => ({}))).data ?? {};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const inHA = (script) => execFileSync("docker", ["exec", "ha-nova-testbed-ha", "sh", "-c", script], { encoding: "utf8" });
const cause = (plan) => (plan.reasons ?? []).find((r) => r.code === "HELPER_CALLS_UNREAD");
const consumers = (plan) => (plan.consumers ?? []).map((c) => c.automation);

export async function yamlHeaterChecks({ ha, cli, check }) {
  if ((await health()).file_access !== "readwrite") {
    console.log("WARN  YAML thermostat rows need file access (TESTBED_FILES=1)");
    return;
  }
  const light = () => cli("plan", "call", "light.turn_off", "light.bed_light");
  const sums = () => inHA("sha256sum /config/yaml_heater_climate.yaml " + FILE);
  const describe = (plan) => `tier ${plan.tier ?? plan.error?.code}, cause ${JSON.stringify(cause(plan) ?? null)}, consumers ${consumers(plan).join(",") || "-"}`;
  const before = sums();
  const bounded = light();
  check("a YAML thermostat whose heater is a YAML template switch is read from /config: no cause named (MAR-245)", !bounded.error && !cause(bounded), describe(bounded));
  check("planning leaves the YAML files byte-identical", sums() === before);
  for (const [name, change] of [
    ["an older copy moved over the heater's file", `cp -p ${FILE} ${FILE}.bak && touch -d 2020-01-01 ${FILE}.bak && mv ${FILE}.bak ${FILE}`],
    ["a hard link to the heater's file", `ln ${FILE} ${FILE}.link && rm ${FILE}.link`],
  ]) {
    inHA(change);
    const unproven = light();
    check(`${name} makes the light's plan ask and name the thermostat`, unproven.tier === "typed" && cause(unproven)?.entity_id === THERMOSTAT && cause(unproven)?.detail === "yaml_changed", describe(unproven));
    await restart(ha);
    const again = light();
    check(`after a restart (${name}), the YAML is read again: no cause named`, !again.error && !cause(again), describe(again));
  }
}

// restart: Home Assistant writes a new run lock and loads the YAML again.
async function restart(ha) {
  const lock = () => inHA("cat /config/.ha_run.lock 2>/dev/null || true");
  const old = lock();
  await ha("POST", "/api/services/homeassistant/restart", {}).catch(() => {});
  for (let i = 0; i < 90; i++) {
    await sleep(2000);
    const states = await ha("GET", "/api/states/" + THERMOSTAT).catch(() => null);
    if (lock() !== old && states && (await health()).ha_ws_connected) return;
  }
  throw new Error("Home Assistant did not come back within 3 minutes");
}
