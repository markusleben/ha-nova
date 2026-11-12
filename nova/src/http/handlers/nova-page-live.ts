import { createHash } from "node:crypto";

import type { GatePage } from "../gate-asks.js";
import { checkOwner, type HaAuthUser } from "../../security/owner-check.js";
import type { RouteHandler } from "../router.js";
import type { RelayLogger } from "../server.js";

// Live updates for the NOVA page and the approval view, without a reload:
// the one script on these pages, inline and allowed only by its hash. It
// polls GET live (an administrator's ingress session, like the pages) for a
// digest of the open asks and paused phone asks, and reloads the page when
// it changes, unless a form on the page has unsaved changes or the pointer
// or focus is on a form (so no button moves under a click), and holds a
// fresh page's buttons for 0.7 s. It also counts [data-left] seconds down
// and fills [data-copy] buttons (selecting the text where the clipboard
// can't be written, as on plain http). It mints no
// token and decides nothing: every button stays a CSRF-guarded POST, and
// both pages work without it.

export const LIVE_SCRIPT = `(()=>{const b=document.body,t0=Date.now();let dirty=false;addEventListener("input",()=>{dirty=true});addEventListener("change",()=>{dirty=true});for(const e of document.querySelectorAll("[data-copy]")){e.hidden=false;e.addEventListener("click",()=>{const f=document.getElementById(e.dataset.copy),pick=()=>getSelection().selectAllChildren(f),c=navigator.clipboard;c?c.writeText(f.textContent).then(()=>{e.textContent=e.dataset.done},pick):pick()})}const bs=[...document.querySelectorAll("form button")];for(const x of bs)x.disabled=true;setTimeout(()=>{for(const x of bs)x.disabled=false},700);const tick=()=>{for(const e of document.querySelectorAll("[data-left]")){const s=Math.max(0,Math.round(+e.dataset.left-(Date.now()-t0)/1000));e.textContent=Math.floor(s/60)+":"+String(s%60).padStart(2,"0")}};tick();setInterval(tick,1000);if(b.dataset.live)setInterval(()=>{if(document.hidden)return;fetch(b.dataset.live,{cache:"no-store"}).then(r=>r.ok?r.json():null).then(j=>{if(j&&j.state!==b.dataset.state&&!dirty&&!document.querySelector("form:hover,form:focus-within"))b.dataset.reload?location.replace(b.dataset.reload):location.reload()},()=>{})},4000)})();`;

export const LIVE_CSP = `script-src 'sha256-${createHash("sha256").update(LIVE_SCRIPT).digest("base64")}'; connect-src 'self'`;

export const liveScript = (): string => `<script>${LIVE_SCRIPT}</script>`;

// What the live poll compares: the asks waiting and the paused phone asks.
export function liveState(page: GatePage): string {
  const asks = page.waiting().map((ask) => ask.ref).sort();
  const paused = page.pausedDevices().map((p) => `${p.deviceId}:${p.until}`).sort();
  return createHash("sha256").update(JSON.stringify([asks, paused])).digest("hex").slice(0, 16);
}

export function createLiveHandler(deps: { fetchAuthUsers: () => Promise<HaAuthUser[]>; page: GatePage; logger?: RelayLogger }): RouteHandler {
  return async ({ request, response }) => {
    const owner = await checkOwner(request, { fetchAuthUsers: deps.fetchAuthUsers, logger: deps.logger }, { admins: true });
    response.setHeader("content-security-policy", "default-src 'none'");
    response.setHeader("cache-control", "no-store");
    response.setHeader("x-content-type-options", "nosniff");
    response.setHeader("content-type", "application/json");
    response.statusCode = owner.ok ? 200 : owner.status;
    response.end(owner.ok ? JSON.stringify({ state: liveState(deps.page) }) : "{}");
  };
}
