// 0.31 RC row for the guided NOVA App update (#1162, MAR-174): on a home where
// an automation listens for service calls, the App step of ha-nova update
// (internal-offer-app-update, what ha-nova update runs after its own
// version) must rate update.install typed and ask through plan/apply, never
// show its yes question or refuse. It needs a terminal and a newer App in
// Home Assistant: run.mjs calls it from the App preview step, after the store
// offers the preview and before the lane installs it, on the Ubuntu VM (a
// terminal session with a file credential store). The ask is cancelled on the
// NOVA page (or its code skipped): nothing is installed, the lane updates the
// App itself right after. Spec: docs/work/2026-10-26-vm-lane-spec.md.
import { approvalAsks } from "./app-asks.mjs";

const AUTOMATION = "vm_lane_guided_update_listener";
const ANSI = /\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07]*\x07|\r/g;
const YES = /Update the NOVA App now\?/;
const REFUSED = /can't start the NOVA App update from here/;
const TYPED = /This update needs your confirmation first/;
const CODE = /Code \(Enter skips\)/;
const NOTHING = /Nothing was installed/;
export const GUIDED_ROW = "guided App update: a home that reacts to update.install gets the typed ask, not a refusal";

// Never fires: only an update.install it would react to, which the row cancels.
export const LISTENER = {
  alias: "vm-lane guided update listener",
  description: "Created and removed by the HA NOVA vm-lane (throwaway VM only)",
  triggers: [{ trigger: "event", event_type: "call_service", event_data: { domain: "update", service: "install" } }],
  actions: [{ action: "lock.unlock", target: { entity_id: "lock.front_door" } }],
  mode: "single",
};

// The session's text and what the lane did, judged. preview: the version Home Assistant offered.
export function judgeGuided({ out, cancelled, skipped, versionBefore, versionAfter, preview }) {
  const text = out.replace(ANSI, "");
  const bad = [];
  if (YES.test(text)) bad.push("it asked yes/no: a home that reacts to update.install got the preview question");
  if (REFUSED.test(text)) bad.push("it refused: \"can't start the NOVA App update from here\"");
  if (!TYPED.test(text)) bad.push("no typed ask");
  else if (!cancelled && !skipped) bad.push("the typed ask reached neither the NOVA page nor a code");
  if (!NOTHING.test(text)) bad.push("no \"Nothing was installed\"");
  if (versionAfter !== versionBefore) bad.push("the App changed: " + versionBefore + " -> " + versionAfter);
  const last = text.split("\n").map((l) => l.trim()).filter(Boolean).slice(-1)[0] ?? "";
  return { ok: bad.length === 0, detail: bad.length ? bad.join("; ") + (last ? " | " + last.slice(0, 120) : "") : "offered v" + preview + (text.includes("(now v" + preview + ")") ? "" : " (not named)") + ", asked typed " + (cancelled ? "on the NOVA page, cancelled there (" + cancelled.slice(0, 60) + ")" : "with a code, skipped") + "; App still " + versionAfter };
}

// ctx: { S, preview, app, tag, repo, guestRelay, ubuntu(script, o), tty(cmd, o), nova, ws, sup, ha(path, init), check, row, sleep }
export async function guidedAppUpdate(ctx) {
  const { S } = ctx;
  const name = GUIDED_ROW;
  const [registry] = await ctx.ws([{ type: "config/entity_registry/list" }]);
  const entity = (registry ?? []).filter((e) => e.platform === "hassio" && e.unique_id === ctx.app + "_version_latest" && e.entity_id.startsWith("update.")).map((e) => e.entity_id);
  if (entity.length !== 1) return ctx.check(S, name, false, entity.length + " registry-proven NOVA App update entities on the VM");
  const state = async () => (await (await ctx.ha("/api/states/" + entity[0])).json());
  await ctx.ha("/api/services/homeassistant/update_entity", { method: "POST", body: JSON.stringify({ entity_id: entity[0] }) }); // as "Check for updates": the lane's own, before any listener
  let st = {};
  for (let i = 0; i < 24 && !(st.state === "on" && st.attributes?.latest_version === ctx.preview); i++) { await ctx.sleep(5000); st = await state(); }
  if (!ctx.check(S, "Home Assistant offers the preview as a NOVA App update", st.state === "on" && st.attributes?.latest_version === ctx.preview, entity[0] + ": " + st.attributes?.installed_version + " -> " + st.attributes?.latest_version + " (" + st.state + ")")) return;
  const versionBefore = (await ctx.sup("/addons/" + ctx.app + "/info")).version;
  const H = (await ctx.ubuntu("mktemp -d /tmp/vm-lane-XXXXXX")).out.trim();
  if (!/^\/tmp\/vm-lane-\w+$/.test(H)) return ctx.check(S, name, false, "no throwaway HOME on the Ubuntu VM: " + H);
  const ENV = "env -i HOME=" + H + "/home USER=$(id -un) LOGNAME=$(id -un) LANG=C.UTF-8 TERM=dumb SHELL=/bin/bash XDG_CONFIG_HOME=" + H + "/home/.config XDG_DATA_HOME=" + H + "/home/.local/share XDG_CACHE_HOME=" + H + "/home/.cache XDG_STATE_HOME=" + H + "/home/.local/state PATH=" + H + "/home/.local/bin:/usr/local/bin:/usr/bin:/bin";
  const sh = (cmd, o) => ctx.ubuntu("set -o pipefail; " + cmd, o);
  let made = false;
  try {
    await sh("mkdir -p " + H + "/home");
    const install = await sh("curl -fsSL https://raw.githubusercontent.com/" + ctx.repo + "/" + ctx.tag + "/install.sh | " + ENV + " HA_NOVA_VERSION=" + ctx.tag + " bash", { timeoutMs: 6 * 60_000 });
    const page = await ctx.nova();
    const pair = install.code === 0 ? await sh(ENV + " ha-nova pair --relay-url " + ctx.guestRelay + " --code " + (await page.code()) + " --credential-store=file </dev/null", { timeoutMs: 3 * 60_000 }) : install;
    if (!ctx.check(S, "guided App update: " + ctx.tag + " installed and paired in a throwaway HOME", pair.code === 0, pair.out.trim().split("\n").slice(-1)[0])) return;
    const saved = await ctx.ha("/api/config/automation/config/" + AUTOMATION, { method: "POST", body: JSON.stringify(LISTENER) });
    made = saved.ok;
    let listening = false;
    for (let i = 0; i < 10 && made && !listening; i++) { await ctx.sleep(2000); listening = (await (await ctx.ha("/api/states")).json()).some((s) => s.attributes?.id === AUTOMATION && s.state === "on"); }
    if (!ctx.check(S, "guided App update: an automation listens for update.install calls", listening, "HTTP " + saved.status + "; automation " + (listening ? "on" : "not on"))) return;
    const open = approvalAsks(await page.approval()).map((a) => a.ref);
    let typed = false, ended = false, skipped = false, cancelled = "";
    const session = ctx.tty(ENV + " ha-nova internal-offer-app-update", { timeoutMs: 6 * 60_000, onData: (out, child) => {
      const text = out.replace(ANSI, "");
      if (YES.test(text) && !ended) { ended = true; child.stdin.write("\x03"); } // the defect: never say yes
      typed ||= TYPED.test(text);
      if (CODE.test(text) && !skipped) { skipped = true; child.stdin.write("\r"); } // an ungated App's code: skipped
    } }).finally(() => { ended = true; });
    for (let i = 0; i < 90 && !ended && !cancelled; i++) { // a gated App's ask: cancelled on the NOVA page, as a person would
      await ctx.sleep(2000);
      if (!typed) continue;
      const ask = approvalAsks(await page.approval()).find((a) => !open.includes(a.ref));
      if (ask) { await page.cancelAsk(ask.ref); cancelled = ask.text || ask.ref; }
    }
    const r = await session;
    const versionAfter = (await ctx.sup("/addons/" + ctx.app + "/info")).version;
    const { ok, detail } = judgeGuided({ out: r.out, cancelled, skipped, versionBefore, versionAfter, preview: ctx.preview });
    ctx.check(S, name, ok, detail);
  } finally {
    if (made) await ctx.ha("/api/config/automation/config/" + AUTOMATION, { method: "DELETE" });
    await sh(ENV + " ha-nova uninstall --yes --purge </dev/null; rm -rf " + H, { cleanup: true, timeoutMs: 3 * 60_000 });
    const [left] = await ctx.ws([{ type: "config/entity_registry/list" }]);
    const gone = (await ctx.ha("/api/config/automation/config/" + AUTOMATION)).status === 404 && !(left ?? []).some((e) => e.unique_id === AUTOMATION);
    ctx.check(S, "guided App update: listener automation removed", gone, gone ? "config and registry entry gone; the throwaway HOME purged (its device revoked)" : "STILL THERE: delete automation \"" + LISTENER.alias + "\" on the HAOS VM");
  }
}
