import type { CsrfStore } from "../../security/csrf.js";
import type { DeviceRecord } from "../../security/device-registry.js";

// Presentation layer of the NOVA owner console: page model, HTML/CSS rendering,
// the armed confirm section, and formatting helpers. The HTTP/action/CSRF
// handling lives in nova-page.ts; this module never touches the request.

export type NovaAction = "generate_code" | "cancel_code" | "revoke_device" | "revoke_legacy" | "reset_registry";

export interface ConnectionStatus {
  haConnected: boolean;
}
export interface UpdateStatus {
  version: string;
  versionLatest: string | null;
  updateAvailable: boolean;
  error: boolean;
}

// The armed confirm step, taken from the GET query. Arming is pure navigation
// (no state, no mutation); the server-side enforcement is that the destructive
// action's single-use CSRF token is issued exclusively by the confirm render
// below — any other navigation simply never mints one.
export type ConfirmTarget =
  | { action: "revoke_device"; deviceId: string }
  | { action: "revoke_legacy" }
  | { action: "reset_registry" };

export function parseConfirm(query: URLSearchParams): ConfirmTarget | null {
  switch (query.get("confirm")) {
    case "revoke_device": {
      const deviceId = query.get("device") ?? "";
      return deviceId === "" ? null : { action: "revoke_device", deviceId };
    }
    case "revoke_legacy":
      return { action: "revoke_legacy" };
    case "reset_registry":
      return { action: "reset_registry" };
    default:
      return null;
  }
}

export interface PageModel {
  ownerName: string;
  csrf: CsrfStore;
  userId: string;
  now: number;
  pairing: { phase: string; code?: string; expiresAtMs?: number };
  devices: DeviceRecord[];
  hasLegacy: boolean;
  registryCorrupt: boolean;
  connection: ConnectionStatus;
  update: UpdateStatus;
  relayVersion: string;
  ingressPath: string;
  errorCode: string | null;
  confirm: ConfirmTarget | null;
}

export function renderPage(m: PageModel): string {
  const formOpen = `<form method="post" action="${escapeAttr(joinIngress(m.ingressPath, "action"))}">`;
  const base = `${m.ingressPath.replace(/\/$/, "")}/`;
  // One token per action per render, not per form. Destructive actions
  // (revoke_device, revoke_legacy, reset_registry) mint their token ONLY inside
  // the confirm section, and the revoke token's bound action carries the device
  // id — that is what makes the two-step flow enforceable server-side.
  const csrfTokens = new Map<string, string>();
  const csrfField = (action: NovaAction, boundAction?: string): string => {
    const bind = boundAction ?? action;
    let token = csrfTokens.get(bind);
    if (token === undefined) {
      token = m.csrf.issue(m.userId, bind, m.now);
      csrfTokens.set(bind, token);
    }
    return `<input type="hidden" name="csrf" value="${escapeAttr(token)}"><input type="hidden" name="action" value="${action}">`;
  };
  const armLink = (params: string, label: string): string =>
    `<a class="linkbtn danger" href="${escapeAttr(`${base}?${params}`)}">${escapeHtml(label)}</a>`;

  // A corrupt registry disables device auth and pairing (a code would fail at
  // finish). Surface it plainly with a recovery path instead of leaving the
  // owner staring at an empty page or editing /data by hand. The reset itself
  // is armed via its confirm screen (strongest gate: typed RESET).
  const recoverySection = m.registryCorrupt
    ? `<section><h2>Recovery needed</h2><p class="muted">The device registry is damaged, so device pairing and existing device access are disabled. Reset it to start fresh — the damaged file is kept aside, and every computer will need to pair again.</p>${armLink("confirm=reset_registry", "Reset device registry…")}</section>`
    : "";

  // Shown after an owner action failed (redirected here with ?err=<code>) so a
  // failure is visible instead of a silent reload.
  const errorSection =
    m.errorCode === "confirm"
      ? `<section class="error"><h2>Reset not confirmed</h2><p>The registry was NOT reset: the confirmation text did not match. Type RESET exactly to confirm.</p></section>`
      : m.errorCode === "1"
        ? `<section class="error"><h2>That did not work</h2><p>The action could not be completed. If you were connecting a device, the relay's secure port may be unavailable — check the App is running, then try again.</p></section>`
        : "";

  const confirmSection = renderConfirmSection(m, formOpen, csrfField, base);

  const pairingSection = m.registryCorrupt
    ? `<p class="muted">Unavailable until the device registry is reset (see Recovery above).</p>`
    : m.pairing.phase === "active" && m.pairing.code
      ? `<p class="muted">On your computer, run <code>ha-nova setup</code> and enter this code when asked. It works once and expires in 10 minutes. Click the code to select it for copying.</p>
         <p class="code">${escapeHtml(formatCode(m.pairing.code))}</p>
         <p class="muted waiting">Waiting for the device… this page updates on its own once it connects.</p>
         ${formOpen}${csrfField("cancel_code")}<button class="secondary" type="submit">Cancel</button></form>`
      : `<p class="muted">Generate a one-time code, then enter it on your computer when NOVA asks. Each device gets its own secure connection.</p>
         ${formOpen}${csrfField("generate_code")}<button type="submit">Connect a device</button></form>${
          m.pairing.phase === "consumed" ? `<p class="ok">✓ A device was just connected.</p>` : ""
        }`;

  const activeDevices = m.devices.filter((d) => d.state === "active");
  const deviceRows = m.registryCorrupt
    ? `<p class="muted">Device access is disabled until the registry is reset.</p>`
    : activeDevices.length === 0
      ? `<p class="muted">No devices are connected yet. Use “Connect a device” above to add your first one.</p>`
      : `<p class="muted">Computers allowed to control Home Assistant through NOVA. Revoking asks for confirmation first.</p><ul>${activeDevices
          .map(
            (d) =>
              `<li><span class="device"><strong>${escapeHtml(d.name)}</strong>${cloudBadge(d)}<span class="muted">${escapeHtml(d.platform)} · ${escapeHtml(d.client)}</span><span class="muted meta">added ${escapeHtml(formatWhen(d.createdAtMs))} · last used ${escapeHtml(formatLastUsed(d.lastUsedAtMs))}</span></span>${armLink(`confirm=revoke_device&device=${encodeURIComponent(d.deviceId)}`, "Revoke…")}</li>`
          )
          .join("")}</ul>`;

  // Shown only while a migrated shared token still exists, so the owner can
  // cut off pre-pairing access that would otherwise outlive every device.
  const legacySection = m.hasLegacy
    ? `<section><h2>Legacy access</h2><p class="muted">A shared token from before device pairing can still control Home Assistant. Once every computer is paired, revoke it so only paired devices keep access.</p>${armLink("confirm=revoke_legacy", "Revoke legacy access…")}</section>`
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
  .error { border-color: var(--danger); }
  .error h2, .error p { color: var(--danger); }
  .waiting { font-style: italic; animation: pulse 1.6s ease-in-out infinite; }
  @keyframes pulse { 0%, 100% { opacity: .55; } 50% { opacity: 1; } }
  /* One click selects the whole code for copying — the page runs no
     JavaScript (CSP), so this replaces a clipboard button. */
  .code { font-size: 2.25rem; letter-spacing: .3rem; font-weight: 700; color: var(--primary); margin: .5rem 0; user-select: all; -webkit-user-select: all; cursor: pointer; }
  form { display: inline; margin: 0; }
  button { font: inherit; font-weight: 500; padding: .5rem 1.1rem; border-radius: 10px;
    border: none; background: var(--primary); color: var(--on-primary); cursor: pointer; }
  button.secondary { background: transparent; color: var(--text); border: 1px solid var(--border); }
  button.danger-solid { background: var(--danger); color: var(--on-primary); }
  .linkbtn { display: inline-block; font-weight: 500; padding: .5rem 1.1rem; border-radius: 10px;
    border: 1px solid var(--border); color: var(--text); text-decoration: none; }
  .linkbtn.danger { color: var(--danger); }
  .badge { display: inline-block; font-size: .7rem; text-transform: uppercase; letter-spacing: .04em;
    border: 1px solid var(--primary); color: var(--primary); border-radius: 6px;
    padding: 0 .35rem; margin-left: .45rem; vertical-align: middle; }
  .meta { font-size: .8rem; }
  .confirm { border-color: var(--danger); }
  .confirm input[type="text"] { font: inherit; padding: .45rem .7rem; border-radius: 10px;
    border: 1px solid var(--border); background: var(--bg); color: var(--text); width: 7rem; }
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
<p class="intro">Let your AI assistant work with Home Assistant — safely. Connect each computer once with a one-time code; every device gets its own connection.</p>
${errorSection}${confirmSection}<section><h2>Home Assistant</h2><p>${m.connection.haConnected ? "Connected." : `<span class="muted">Not connected — check the App logs.</span>`}</p></section>
${recoverySection}<section><h2>Update</h2><p>${updateLine}</p></section>
<section><h2>Pairing</h2>${pairingSection}</section>
<section><h2>Devices</h2>${deviceRows}</section>
${legacySection}<footer>Signed in as ${escapeHtml(m.ownerName)} · Relay ${escapeHtml(m.relayVersion)}</footer>
</main>
</body></html>`;
}

// Renders the armed confirm step for a destructive action. This render is the
// ONLY place the action's single-use CSRF token is minted; confirming a device
// revoke shows exactly which device, since when, last used, and whether it is
// Cloud-bound — an informed decision instead of a blind click.
function renderConfirmSection(
  m: PageModel,
  formOpen: string,
  csrfField: (action: NovaAction, boundAction?: string) => string,
  base: string,
): string {
  if (m.confirm === null || (m.registryCorrupt && m.confirm.action !== "reset_registry")) {
    return "";
  }
  const cancel = `<a class="linkbtn" href="${escapeAttr(base)}">Cancel</a>`;
  switch (m.confirm.action) {
    case "revoke_device": {
      const target = m.confirm;
      const device = m.devices.find(
        (d) => d.deviceId === target.deviceId && d.state === "active",
      );
      if (device === undefined) {
        return "";
      }
      const cloudLine =
        device.cloudUserId === undefined
          ? "local network only"
          : `Home Assistant Cloud (user ${escapeHtml(device.cloudUserId)})`;
      return `<section class="confirm"><h2>Confirm revoke</h2>
<p><strong>${escapeHtml(device.name)}</strong>${cloudBadge(device)}<span class="muted"> — ${escapeHtml(device.platform)} · ${escapeHtml(device.client)}</span></p>
<p class="muted meta">added ${escapeHtml(formatWhen(device.createdAtMs))} · last used ${escapeHtml(formatLastUsed(device.lastUsedAtMs))} · reaches NOVA via ${cloudLine}</p>
<p>Revoking cuts this computer's access immediately. It can pair again later with a new code.</p>
${formOpen}${csrfField("revoke_device", `revoke_device:${device.deviceId}`)}<input type="hidden" name="device_id" value="${escapeAttr(device.deviceId)}"><button class="danger-solid" type="submit">Revoke this device</button></form> ${cancel}</section>`;
    }
    case "revoke_legacy": {
      if (!m.hasLegacy) {
        return "";
      }
      return `<section class="confirm"><h2>Confirm revoke of legacy access</h2>
<p>The shared token from before device pairing stops working immediately. Paired devices keep their access.</p>
${formOpen}${csrfField("revoke_legacy")}<button class="danger-solid" type="submit">Revoke legacy access</button></form> ${cancel}</section>`;
    }
    case "reset_registry": {
      if (!m.registryCorrupt) {
        return "";
      }
      return `<section class="confirm"><h2>Confirm registry reset</h2>
<p>Every paired computer loses access and must pair again with a new code. The damaged file is kept aside. Type <code>RESET</code> to confirm.</p>
${formOpen}${csrfField("reset_registry")}<input type="text" name="confirm_text" autocomplete="off" placeholder="RESET" required> <button class="danger-solid" type="submit">Reset device registry</button></form> ${cancel}</section>`;
    }
  }
}

function cloudBadge(device: DeviceRecord): string {
  return device.cloudUserId === undefined ? "" : `<span class="badge">cloud</span>`;
}

// Server-local wall-clock, minute precision — the App container shares the
// household's timezone, and an absolute stamp is what makes two same-named
// pairings distinguishable. Hand-formatted so the output does not depend on
// the Node build's ICU data.
function formatWhen(ms: number): string {
  const d = new Date(ms);
  const pad = (n: number): string => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function formatLastUsed(ms: number | undefined): string {
  return ms === undefined ? "never" : formatWhen(ms);
}

function formatCode(code: string): string {
  return code.length === 6 ? `${code.slice(0, 3)} ${code.slice(3)}` : code;
}

function joinIngress(ingressPath: string, leaf: string): string {
  return `${ingressPath.replace(/\/$/, "")}/${leaf}`;
}

export function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] ?? c);
}
function escapeAttr(value: string): string {
  return escapeHtml(value);
}
