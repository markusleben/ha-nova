import type { IncomingMessage, ServerResponse } from "node:http";

import { checkOwner, type HaAuthUser } from "../../security/owner-check.js";
import type { CsrfStore } from "../../security/csrf.js";
import type { DeviceRecord, DeviceRegistry } from "../../security/device-registry.js";
import type { PairingV1Manager } from "../../security/pairing-v1.js";
import type { RouteHandler } from "../router.js";

// The NOVA owner console, served over Supervisor ingress. Server-rendered HTML
// with NO JavaScript and no external resources; every mutating action is a POST
// form guarded by a single-use CSRF token, Sec-Fetch/Origin checks, and a fresh
// owner re-check. Post/Redirect/Get avoids re-submission. The visible name is
// only "NOVA"; the code is shown solely while a pairing is active and is never
// logged.

const CSP = [
  "default-src 'none'",
  "style-src 'unsafe-inline'",
  "img-src 'self' data:",
  "base-uri 'none'",
  "form-action 'self'",
  "frame-ancestors 'self'",
].join("; ");

export type NovaAction = "generate_code" | "cancel_code" | "revoke_device" | "revoke_legacy";
const ACTIONS = new Set<NovaAction>(["generate_code", "cancel_code", "revoke_device", "revoke_legacy"]);

export interface ConnectionStatus {
  haConnected: boolean;
}
export interface UpdateStatus {
  version: string;
  versionLatest: string | null;
  updateAvailable: boolean;
  error: boolean;
}

export interface NovaPageDeps {
  fetchAuthUsers: () => Promise<HaAuthUser[]>;
  csrf: CsrfStore;
  pairing: PairingV1Manager;
  registry: DeviceRegistry;
  connection: () => ConnectionStatus;
  update: () => Promise<UpdateStatus>;
  relayVersion: string;
  now: () => number;
}

function setHeaders(response: ServerResponse): void {
  response.setHeader("content-security-policy", CSP);
  response.setHeader("cache-control", "no-store");
  response.setHeader("x-content-type-options", "nosniff");
  response.setHeader("x-frame-options", "SAMEORIGIN");
  response.setHeader("referrer-policy", "no-referrer");
}

function fail(response: ServerResponse, status: number, message: string): void {
  setHeaders(response);
  response.statusCode = status;
  response.setHeader("content-type", "text/html; charset=utf-8");
  response.end(`<!doctype html><meta charset="utf-8"><title>NOVA</title><p>${escapeHtml(message)}</p>`);
}

export function createNovaPageHandler(deps: NovaPageDeps): RouteHandler {
  return async ({ request, response }) => {
    const owner = await checkOwner(request, { fetchAuthUsers: deps.fetchAuthUsers });
    if (!owner.ok) {
      fail(response, owner.status, owner.status === 503 ? "Could not verify owner access. Try again shortly." : "Owner access required.");
      return;
    }
    const [update] = await Promise.all([safeUpdate(deps)]);
    const html = renderPage({
      ownerName: owner.name,
      csrf: deps.csrf,
      userId: owner.userId,
      now: deps.now(),
      pairing: deps.pairing.getStatus(),
      devices: deps.registry.list(),
      hasLegacy: deps.registry.hasLegacy(),
      connection: deps.connection(),
      update,
      relayVersion: deps.relayVersion,
      ingressPath: singleHeader(request, "x-ingress-path") ?? "/home",
    });
    setHeaders(response);
    response.statusCode = 200;
    response.setHeader("content-type", "text/html; charset=utf-8");
    response.end(html);
  };
}

export function createNovaActionHandler(deps: NovaPageDeps): RouteHandler {
  return async ({ request, response, body }) => {
    const owner = await checkOwner(request, { fetchAuthUsers: deps.fetchAuthUsers });
    if (!owner.ok) {
      fail(response, owner.status, "Owner access required.");
      return;
    }
    // Reject cross-site form submissions. Sec-Fetch-Site, when the browser sends
    // it, must be same-origin/same-site/none; an explicit cross-site is refused.
    const fetchSite = singleHeader(request, "sec-fetch-site");
    if (fetchSite !== null && fetchSite !== "same-origin" && fetchSite !== "same-site" && fetchSite !== "none") {
      fail(response, 403, "Cross-site request refused.");
      return;
    }
    const form = (body ?? {}) as Record<string, unknown>;
    const action = typeof form.action === "string" ? form.action : "";
    const token = typeof form.csrf === "string" ? form.csrf : "";
    if (!ACTIONS.has(action as NovaAction)) {
      fail(response, 400, "Unknown action.");
      return;
    }
    if (!deps.csrf.consume(owner.userId, action, token, deps.now())) {
      fail(response, 403, "This form expired. Reload the page and try again.");
      return;
    }

    try {
      applyAction(deps, action as NovaAction, form);
    } catch {
      // Fail closed but do not leak internals; the page reflects the new state.
    }

    // Post/Redirect/Get back to the page. The redirect target MUST keep the
    // ingress base path's trailing slash: Home Assistant serves the console at
    // "<ingress-path>/" (forwarding GET /) but 404s the slash-less base path.
    const ingressPath = singleHeader(request, "x-ingress-path") ?? "";
    setHeaders(response);
    response.statusCode = 303;
    response.setHeader("location", `${ingressPath.replace(/\/$/, "")}/`);
    response.end();
  };
}

function applyAction(deps: NovaPageDeps, action: NovaAction, form: Record<string, unknown>): void {
  switch (action) {
    case "generate_code":
      deps.pairing.generateCode();
      return;
    case "cancel_code":
      deps.pairing.cancel();
      return;
    case "revoke_device": {
      const deviceId = typeof form.device_id === "string" ? form.device_id : "";
      if (deviceId) {
        deps.registry.revoke(deviceId);
      }
      return;
    }
    case "revoke_legacy":
      // Cut off the migrated shared token so a pre-pairing credential cannot
      // outlive the devices; paired devices keep working.
      deps.registry.revokeLegacy();
      return;
  }
}

async function safeUpdate(deps: NovaPageDeps): Promise<UpdateStatus> {
  try {
    return await deps.update();
  } catch {
    return { version: deps.relayVersion, versionLatest: null, updateAvailable: false, error: true };
  }
}

interface PageModel {
  ownerName: string;
  csrf: CsrfStore;
  userId: string;
  now: number;
  pairing: { phase: string; code?: string; expiresAtMs?: number };
  devices: DeviceRecord[];
  hasLegacy: boolean;
  connection: ConnectionStatus;
  update: UpdateStatus;
  relayVersion: string;
  ingressPath: string;
}

function renderPage(m: PageModel): string {
  const formOpen = `<form method="post" action="${escapeAttr(joinIngress(m.ingressPath, "action"))}">`;
  // One token per action per render, not per form: the page can show up to
  // MAX_ACTIVE revoke forms, but a single-use token per device would evict the
  // earliest ones (CSRF cap) and break those buttons. Forms of one action
  // submit one at a time (PRG re-renders with a fresh token), so they share it.
  const csrfTokens = new Map<NovaAction, string>();
  const csrfField = (action: NovaAction): string => {
    let token = csrfTokens.get(action);
    if (token === undefined) {
      token = m.csrf.issue(m.userId, action, m.now);
      csrfTokens.set(action, token);
    }
    return `<input type="hidden" name="csrf" value="${escapeAttr(token)}"><input type="hidden" name="action" value="${action}">`;
  };

  const pairingSection =
    m.pairing.phase === "active" && m.pairing.code
      ? `<p class="muted">On your computer, run <code>ha-nova setup</code> and enter this code when asked. It works once and expires in 10 minutes. Click the code to select it for copying.</p>
         <p class="code">${escapeHtml(formatCode(m.pairing.code))}</p>
         <p class="muted waiting">Waiting for the device… this page updates on its own once it connects.</p>
         ${formOpen}${csrfField("cancel_code")}<button class="secondary" type="submit">Cancel</button></form>`
      : `<p class="muted">Generate a one-time code, then enter it on your computer when NOVA asks. Each device gets its own secure connection — no tokens to copy.</p>
         ${formOpen}${csrfField("generate_code")}<button type="submit">Connect a device</button></form>${
          m.pairing.phase === "consumed" ? `<p class="ok">✓ A device was just connected.</p>` : ""
        }`;

  const activeDevices = m.devices.filter((d) => d.state === "active");
  const deviceRows =
    activeDevices.length === 0
      ? `<p class="muted">No devices are connected yet. Use “Connect a device” above to add your first one.</p>`
      : `<p class="muted">Computers allowed to control Home Assistant through NOVA. Revoke one to cut its access immediately.</p><ul>${activeDevices
          .map(
            (d) =>
              `<li><span class="device"><strong>${escapeHtml(d.name)}</strong><span class="muted">${escapeHtml(d.platform)} · ${escapeHtml(d.client)}</span></span>${formOpen}${csrfField("revoke_device")}<input type="hidden" name="device_id" value="${escapeAttr(d.deviceId)}"><button class="secondary danger" type="submit">Revoke</button></form></li>`
          )
          .join("")}</ul>`;

  // Shown only while a migrated shared token still exists, so the owner can
  // cut off pre-pairing access that would otherwise outlive every device.
  const legacySection = m.hasLegacy
    ? `<section><h2>Legacy access</h2><p class="muted">A shared token from before device pairing can still control Home Assistant. Once every computer is paired, revoke it so only paired devices keep access.</p>${formOpen}${csrfField("revoke_legacy")}<button class="secondary danger" type="submit">Revoke legacy access</button></form></section>`
    : "";

  const updateLine = m.update.error
    ? `<span class="muted">Update status unavailable.</span>`
    : m.update.updateAvailable
      ? `Update available: <strong>${escapeHtml(m.update.versionLatest ?? "")}</strong> (installed ${escapeHtml(m.update.version)}). Update from the Apps page.`
      : `<span class="muted">Up to date (${escapeHtml(m.update.version)}).</span>`;

  const star = `<svg class="logo" viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M12 2l2.2 7.8L22 12l-7.8 2.2L12 22l-2.2-7.8L2 12l7.8-2.2z"/></svg>`;

  // Colours are Home Assistant's own default theme tokens; prefers-color-scheme
  // follows the dark/light preference (HA does not inject theme CSS into ingress
  // iframes, and this page runs without JavaScript by design).
  // While a code is active we auto-refresh so the owner sees the device appear
  // without reloading — pure HTML (this page runs no JavaScript by design). The
  // refresh stops the moment pairing is consumed or cancelled.
  const autoRefresh = m.pairing.phase === "active" ? `<meta http-equiv="refresh" content="5">` : "";

  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">${autoRefresh}
<title>NOVA</title>
<style>
  :root {
    --bg: #f5f5f7; --card: #ffffff; --text: #212121; --muted: #727272;
    --border: rgba(0,0,0,.10); --primary: #03a9f4; --on-primary: #ffffff;
    --danger: #db4437; --shadow: 0 2px 6px rgba(0,0,0,.08);
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #111111; --card: #1c1c1c; --text: #e1e1e1; --muted: #9b9b9b;
      --border: rgba(225,225,225,.12); --primary: #03a9f4; --on-primary: #ffffff;
      --danger: #e57373; --shadow: none;
    }
  }
  * { box-sizing: border-box; }
  body { font-family: Roboto, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    background: var(--bg); color: var(--text); margin: 0; padding: 1.5rem 1rem;
    -webkit-font-smoothing: antialiased; line-height: 1.5; }
  main { max-width: 34rem; margin: 0 auto; }
  h1 { display: flex; align-items: center; gap: .55rem; font-size: 1.5rem; font-weight: 500; margin: 0 0 1.25rem; }
  .logo { width: 1.7rem; height: 1.7rem; color: var(--primary); }
  section { background: var(--card); border: 1px solid var(--border); border-radius: 12px;
    box-shadow: var(--shadow); padding: 1rem 1.25rem; margin: 0 0 1rem; }
  section h2 { font-size: .8rem; text-transform: uppercase; letter-spacing: .04em;
    color: var(--muted); margin: 0 0 .5rem; font-weight: 600; }
  p { margin: .35rem 0; }
  .muted { color: var(--muted); }
  .ok { color: var(--primary); }
  .waiting { font-style: italic; animation: pulse 1.6s ease-in-out infinite; }
  @keyframes pulse { 0%, 100% { opacity: .55; } 50% { opacity: 1; } }
  /* One click selects the whole code for copying — the page runs no
     JavaScript (CSP), so this replaces a clipboard button. */
  .code { font-size: 2.25rem; letter-spacing: .3rem; font-weight: 700; color: var(--primary); margin: .5rem 0; user-select: all; -webkit-user-select: all; cursor: pointer; }
  form { display: inline; margin: 0; }
  button { font: inherit; font-weight: 500; padding: .5rem 1.1rem; border-radius: 10px;
    border: none; background: var(--primary); color: var(--on-primary); cursor: pointer; }
  button.secondary { background: transparent; color: var(--text); border: 1px solid var(--border); }
  button.danger { color: var(--danger); }
  ul { list-style: none; padding: 0; margin: 0; }
  li { display: flex; align-items: center; justify-content: space-between; gap: 1rem;
    padding: .6rem 0; border-top: 1px solid var(--border); }
  li:first-child { border-top: none; }
  .device { display: flex; flex-direction: column; }
  footer { color: var(--muted); font-size: .8rem; margin-top: 1.5rem; text-align: center; }
  code { background: var(--border); padding: .05rem .35rem; border-radius: 5px; font-size: .9em; }
  .intro { color: var(--muted); margin: -.75rem 0 1.5rem; }
</style></head><body>
<main>
<h1>${star}NOVA</h1>
<p class="intro">Let your AI assistant work with Home Assistant — safely. Connect each computer once with a one-time code; there are no tokens to copy or paste.</p>
<section><h2>Home Assistant</h2><p>${m.connection.haConnected ? "Connected." : `<span class="muted">Not connected — check the App logs.</span>`}</p></section>
<section><h2>Update</h2><p>${updateLine}</p></section>
<section><h2>Pairing</h2>${pairingSection}</section>
<section><h2>Devices</h2>${deviceRows}</section>
${legacySection}<footer>Signed in as ${escapeHtml(m.ownerName)} · Relay ${escapeHtml(m.relayVersion)}</footer>
</main>
</body></html>`;
}

function formatCode(code: string): string {
  return code.length === 6 ? `${code.slice(0, 3)} ${code.slice(3)}` : code;
}

function joinIngress(ingressPath: string, leaf: string): string {
  return `${ingressPath.replace(/\/$/, "")}/${leaf}`;
}

function singleHeader(request: IncomingMessage, name: string): string | null {
  const value = request.headers[name];
  return typeof value === "string" ? value : null;
}

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] ?? c);
}
function escapeAttr(value: string): string {
  return escapeHtml(value);
}
