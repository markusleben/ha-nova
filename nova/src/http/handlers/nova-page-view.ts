import type { ChatAppsStatus } from "../../runtime/chat-apps.js";
import { claudeConnectorLink } from "../../runtime/chat-connector.js";
import type { CsrfStore } from "../../security/csrf.js";
import type { DeviceRecord } from "../../security/device-registry.js";
import { chatPhoneLine, chatPhoneWaiting, renderChatPhone, renderChatPhoneError, type ChatPhoneAction, type ChatPhoneModel } from "./nova-page-chat-phone.js";
import { renderChatActivity, renderChatUsers, renderToolsChanged, type ChatPhoneUser, type ChatUsersModel } from "./nova-page-chat-users.js";
import { liveScript } from "./nova-page-live.js";
import { PAGE_CSS } from "./nova-page-style.js";

// Presentation layer of the NOVA console: page model, HTML/CSS rendering,
// the armed confirm section, and formatting helpers. The HTTP/action/CSRF
// handling lives in nova-page.ts; this module never touches the request.

export type NovaAction = "generate_code" | "cancel_code" | "revoke_device" | "reset_registry" | "resume_phone_asks" | ChatAction | ChatPhoneAction;
export type ChatAction = "chat_apps_on" | "chat_apps_off" | "chat_read_only" | "chat_codes" | "chat_users";

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
  | { action: "reset_registry" };

export function parseConfirm(query: URLSearchParams): ConfirmTarget | null {
  switch (query.get("confirm")) {
    case "revoke_device": {
      const deviceId = query.get("device") ?? "";
      return deviceId === "" ? null : { action: "revoke_device", deviceId };
    }
    case "reset_registry":
      return { action: "reset_registry" };
    default:
      return null;
  }
}

export interface PageModel {
  ownerName: string; // who is signed in
  isOwner: boolean; // false: an administrator, who sees no chat apps and no registry reset
  chatFeature: boolean; // this App has chat apps (an administrator's page names who changes them)
  houseOwner: string; // the owner's name, "" unknown
  csrf: CsrfStore;
  userId: string;
  now: number;
  pairing: { phase: string; code?: string; expiresAtMs?: number };
  devices: DeviceRecord[];
  registryCorrupt: boolean;
  connection: ConnectionStatus;
  update: UpdateStatus;
  relayVersion: string;
  ingressPath: string;
  errorCode: string | null;
  confirm: ConfirmTarget | null;
  chatApps: ChatAppsStatus | null; // null: the feature is off in this App
  chatConnectorUrl: string | null; // null: neither Link remote access nor a public HTTPS address
  chatPhone: ChatPhoneModel | null; // null: chat apps are off
  chatPhoneUser: ChatPhoneUser | null; // whose phone chatPhone is: a listed user's (?chat_user=), null the owner's
  chatPhoneUserGone: boolean; // ?chat_user= named someone who can't have a phone now: the owner's is shown
  chatUsers: ChatUsersModel | null; // null: chat apps are off
  chatRestarting: boolean;
  gateSection: string; // nova-approve.ts renderGateSection; "" without a gate
  live: string | null; // nova-page-live.ts liveState; null without a gate
}

export function renderPage(m: PageModel): string {
  const formOpen = `<form method="post" action="${escapeAttr(joinIngress(m.ingressPath, "action"))}">`;
  const base = `${m.ingressPath.replace(/\/$/, "")}/`;
  // One token per action per render, not per form. Destructive actions
  // (revoke_device, reset_registry) mint their token ONLY inside
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
    ? `<section><h2>Recovery needed</h2><p class="muted">The device registry is damaged, so device pairing and existing device access are disabled. Reset it to start fresh — the damaged file is kept aside, and every computer will need to pair again.</p>${m.isOwner ? armLink("confirm=reset_registry", "Reset device registry…") : `<p>Only ${ownerWords(m)} can reset it.</p>`}</section>`
    : "";

  // Shown after an owner action failed (redirected here with ?err=<code>) so a
  // failure is visible instead of a silent reload.
  const errorSection =
    m.errorCode === "confirm"
      ? `<section class="error"><h2>Reset not confirmed</h2><p>The registry was NOT reset: the confirmation text did not match. Type RESET exactly to confirm.</p></section>`
      : m.errorCode === "1"
        ? `<section class="error"><h2>That did not work</h2><p>The action could not be completed. If you were connecting a device, the relay's secure port may be unavailable — check the App is running, then try again.</p></section>`
        : m.errorCode === "chat_config"
          ? `<section class="error"><h2>Chat apps are not on</h2><p>Home Assistant's configuration has an error, so it can't restart. Fix it in Home Assistant, then try again.</p></section>`
          : m.errorCode === "chat_old"
            ? `<section class="error"><h2>Chat apps are not on</h2><p>Chat apps need Home Assistant 2026.10 or newer. Update Home Assistant first, then try again.</p></section>`
          : m.errorCode === "chat_folder"
            ? `<section class="error"><h2>Chat apps: that did not work</h2><p>HA NOVA can't use the custom_components folder in Home Assistant's configuration. Check that it is a normal folder, then try again.</p></section>`
          : m.errorCode === "chat_users"
            ? `<section class="error"><h2>Chat apps: list not saved</h2><p>Nothing changed: someone you picked changed in Home Assistant meanwhile (removed, turned off, or another role). Check the list under Chat apps, then save again.</p></section>`
          : m.errorCode === "chat_users_link"
            ? `<section class="error"><h2>Chat apps: list not saved</h2><p>Nothing changed: HA NOVA couldn't clear an old phone link of someone you added. Try again in a moment. If it keeps failing, the NOVA App log says why.</p></section>`
          : m.errorCode === "chat"
            ? `<section class="error"><h2>Chat apps: that did not work</h2><p>Try again in a moment. If it keeps failing, the NOVA App log says why.</p></section>`
            : renderChatPhoneError(m.errorCode);

  const confirmSection = renderConfirmSection(m, formOpen, csrfField, base);

  const connect = `${formOpen}${csrfField("generate_code")}<button type="submit">Connect a device</button></form>`;
  const cancelCode = `${formOpen}${csrfField("cancel_code")}<button class="secondary" type="submit">Cancel</button></form>`;
  const pairingSection = m.registryCorrupt
    ? `<p class="muted">Unavailable until the device registry is reset (see Recovery above).</p>`
    : m.pairing.phase === "active" && m.pairing.code
      ? `<div class="field"><p class="muted">On your computer, run <code>ha-nova setup</code> and enter this code when asked. It works once and expires in 10 minutes. Click the code to select it for copying.</p>
         <p class="code">${escapeHtml(formatCode(m.pairing.code))}</p>
         <p class="muted waiting">Waiting for the device… this page updates on its own once it connects.</p>
         <div class="actions">${cancelCode}</div></div>`
      : m.pairing.phase === "armed"
        ? `<p class="muted waiting">A computer is connecting through Home Assistant sign-in… this page updates on its own once it connects. If your computer asks for a code instead, click “Connect a device”.</p>
         <div class="actions">${connect}${cancelCode}</div>`
        : `${m.pairing.phase === "consumed" ? `<p class="ok">✓ A device was just connected.</p>` : ""}<div class="actions">${connect}<span class="muted meta">Most computers connect when you sign in on them. Make a code here only if your computer asks for one. It works once, for 10 minutes.</span></div>`;

  const activeDevices = m.devices.filter((d) => d.state === "active");
  const deviceRows = m.registryCorrupt
    ? `<p class="muted">Device access is disabled until the registry is reset.</p>`
    : activeDevices.length === 0
      ? `<p class="muted">No computers are connected yet.</p>`
      : `<ul>${activeDevices
          .map(
            (d) =>
              `<li><span class="device"><span><strong>${escapeHtml(d.name)}</strong>${cloudBadge(d)}</span><span class="muted meta">${escapeHtml(platformName(d.platform))} · ${escapeHtml(d.client)}</span><span class="muted meta">added ${escapeHtml(formatWhen(d.createdAtMs))} · last used ${escapeHtml(formatLastUsed(d.lastUsedAtMs))}</span></span>${armLink(`confirm=revoke_device&device=${encodeURIComponent(d.deviceId)}`, "Revoke…")}</li>`
          )
          .join("")}</ul>`;

  const updateLine = m.update.error
    ? `<span class="muted">Update status unavailable.</span>`
    : m.update.updateAvailable
      ? `<span>Update available: <strong>${escapeHtml(m.update.versionLatest ?? "")}</strong> <span class="muted meta">(installed ${escapeHtml(m.update.version)}). Update from the Apps page.</span></span>`
      : `<span class="pill good">Up to date · ${escapeHtml(m.update.version)}</span>`;

  const star = `<svg class="logo" viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M12 2l2.2 7.8L22 12l-7.8 2.2L12 22l-2.2-7.8L2 12l7.8-2.2z"/></svg>`;

  // While a code is active the page refreshes itself (plain HTML, also
  // without the live script) so the owner sees the device appear; the
  // refresh stops the moment pairing is consumed or cancelled.
  const autoRefresh = m.pairing.phase === "active" || m.pairing.phase === "armed" || chatPhoneWaiting(m.chatPhone) ? `<meta http-equiv="refresh" content="5">` : "";

  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">${autoRefresh}
<title>NOVA</title>
<style>${PAGE_CSS}</style></head><body${m.live === null ? "" : ` data-live="${escapeAttr(`${base}live`)}" data-state="${m.live}"`}>
<main>
<h1>${star}NOVA</h1>
<p class="intro">Let your AI app work with Home Assistant, safely.</p>
${errorSection}${confirmSection}${m.gateSection}<section><h2>Status</h2><ul class="facts"><li><span>Home Assistant</span>${m.connection.haConnected ? `<span class="pill good">Connected</span>` : `<span class="pill bad">Not connected — check the App logs</span>`}</li><li><span>NOVA App</span>${updateLine}</li></ul></section>
${recoverySection}<section id="computers"><h2>Computers</h2>${m.pairing.phase === "active" ? pairingSection + deviceRows : deviceRows + pairingSection}</section>
${m.isOwner ? `${renderChatApps(m, formOpen, csrfField)}${renderChatActivity(m.chatUsers?.activity ?? null, m.now)}` : !m.chatFeature ? "" : `<section><h2>Chat apps</h2><p class="muted">Chat apps and their settings: only ${ownerWords(m)} can change them.</p></section>`}<footer>Signed in as ${escapeHtml(m.ownerName)} · Relay ${escapeHtml(m.relayVersion)}</footer>
</main>
${liveScript()}</body></html>`;
}

// "the owner, Markus," for an administrator's page (names only through escapeHtml).
function ownerWords(m: PageModel): string {
  return m.houseOwner ? `the owner, ${escapeHtml(m.houseOwner)},` : "the owner of this Home Assistant";
}

// "Chat apps": Claude and ChatGPT through Home Assistant Link or a public HTTPS address (spec §11.6).
// Turning on is consent to the one Home Assistant restart named on the button.
function renderChatApps(m: PageModel, formOpen: string, csrfField: (action: NovaAction, boundAction?: string) => string): string {
  const c = m.chatApps;
  if (c === null) {
    return "";
  }
  const button = (action: ChatAction, label: string, value = "", style = ""): string =>
    `${formOpen}${csrfField(action)}${value ? `<input type="hidden" name="value" value="${value}">` : ""}<button${style ? ` class="${style}"` : ""} type="submit">${label}</button></form>`;
  const toggle = (label: string, on: boolean, action: ChatAction): string =>
    `<li><span>${label}: <strong>${on ? "on" : "off"}</strong></span>${button(action, on ? "Turn off" : "Turn on", on ? "off" : "on", "secondary")}</li>`;
  let body: string;
  if (!c.available && !c.enabled && !c.unfinished) {
    body = `<p class="muted">Using HA NOVA in Claude and ChatGPT needs a newer NOVA App.</p>`;
  } else if (!c.available) { // e.g. after going back to an older App: offer only the way off
    body = `<p>This NOVA App version can't run chat apps. Turn them off, so Home Assistant stops looking for them.</p>${button("chat_apps_off", "Turn off chat apps", "", "secondary")}`;
  } else if (c.enabled) {
    const base = `${m.ingressPath.replace(/\/$/, "")}/`;
    // The owner's phone, or that of a user on the list whose row the owner opened.
    const who = m.chatPhoneUser;
    const codes = who === null ? c.chatCodes : who.codes;
    const phone = `<h3 id="chat-phone">${who === null ? "Your phone" : `${escapeHtml(who.name)}'s phone`}</h3>
${m.chatPhoneUserGone ? `<p class="muted">That person can't have a phone for chat apps now: they aren't on the list at Use the home or above.</p>` : ""}<p class="muted">Confirmations: ${chatPhoneLine(m.chatPhone, who === null ? c.ownerPhone : c.phones[who.id] ?? null, codes, who?.name ?? null)}.</p>
${m.chatPhone === null ? "" : renderChatPhone(m.chatPhone, { id: who?.id ?? m.userId, name: who?.name ?? null }, codes, formOpen, csrfField, base)}`;
    const users = m.chatUsers === null ? "" : renderChatUsers({ model: m.chatUsers, status: c, ownerId: m.userId, ownerName: m.ownerName, url: m.chatConnectorUrl, form: `${formOpen}${csrfField("chat_users")}`, base });
    // Claude asks before each step until its tools are on “Always allow”: with a phone that confirms, all of them.
    const next = c.ownerPhone === null
      ? "In Claude, open Customize → Connectors → HA NOVA. Set every tool to “Always allow”, except “Run a confirmed change”: leave it on “Needs approval”. Then Claude still asks before each bigger change. The AI can see a chat code, so that question is your own check. Link a phone, and you can allow that one too."
      : "In Claude, open Customize → Connectors → HA NOVA and set every tool to “Always allow”. HA NOVA still shows you a preview before bigger changes, and your phone asks before unlocks, the alarm, and deletes.";
    const url = m.chatConnectorUrl;
    const settings = `<details><summary>Settings · can only read: ${c.readOnly ? "on" : "off"} · codes in the chat: ${c.chatCodes ? "on" : "off"}</summary>
<ul>${toggle("Chat apps can only read", c.readOnly, "chat_read_only")}${toggle("Codes in the chat", c.chatCodes, "chat_codes")}</ul>
<p class="muted meta">To sign a chat app out, remove its token in Home Assistant under Profile → Security. Turn chat apps off before you remove the NOVA App or go back to an older version.</p>
<div class="actions">${button("chat_apps_off", "Turn off chat apps", "", "secondary")}</div></details>`;
    body = `${m.chatRestarting ? `<p class="ok">Home Assistant is restarting (usually under a minute). Reload this page afterwards.</p>` : ""}${url === null
      ? `<p>On, but HA NOVA can't find an address Claude and ChatGPT can reach. Turn on remote access in Home Assistant Link, or set your own public HTTPS address (valid certificate) as the external URL under Settings → System → Network, then reload this page.</p>`
      : `<p><span class="pill good">On</span> Add HA NOVA to Claude or ChatGPT, then sign in with your own Home Assistant login.</p>
<div class="clients"><div class="client"><h3>Claude</h3><p class="muted meta">On a computer and in its phone app.</p>
<div class="actions"><a class="linkbtn primary" href="${escapeAttr(claudeConnectorLink(url))}" target="_blank" rel="noopener noreferrer">Add to Claude</a></div>
${c.ownerPhone === null ? `<p class="meta"><strong>Keep “Run a confirmed change” on “Needs approval” in Claude</strong> until you link a phone: the AI can see a chat code.</p>` : ""}
<details><summary>Next: stop Claude asking before every step</summary><p>At first, Claude asks before each step. That's Claude, not HA NOVA. ${next} Claude may ask again after an update.</p></details>
<details><summary>Claude Team or Enterprise plan</summary><p>The owner of your Claude plan adds it once for everyone: <a href="${escapeAttr(claudeConnectorLink(url, true))}" target="_blank" rel="noopener noreferrer">Add for my organization</a>. Then click Connect next to HA NOVA in Customize → Connectors.</p></details>
<details><summary>No form opens?</summary><p>In Claude, open Customize → Connectors, click +, then Add custom connector, and paste the address below.</p></details></div>
<div class="client"><h3>ChatGPT</h3><p class="muted meta">On the web, on a computer, as a plugin.</p>
<details><summary>How to add it</summary><ol><li>Open chatgpt.com/plugins → + → “Add custom MCP server”.</li><li>Name it HA NOVA and paste the address below under “Connection”.</li><li>Pick “OAuth”, tick “I understand and want to continue”, then “Create as a plugin”.</li><li>Sign in with your own Home Assistant login, then type @HA NOVA in a chat.</li></ol>
<p class="muted meta">If ChatGPT shows an error right after, choose Connect on the plugin again. Name already taken? Find your old HA NOVA under your own plugins. If it has this address, choose Install; if not, use another name.</p></details>${renderToolsChanged(m.chatUsers?.toolsChangedAt ?? null, m.now)}</div></div>
<p class="label">Address for chat apps <span class="muted meta">· adds ${escapeHtml(new URL(url).host)}</span></p>
<p class="field address" id="chat-address">${escapeHtml(url)}</p><button type="button" class="secondary" data-copy="chat-address" data-done="Copied" hidden>Copy address</button>`}
${phone}
${users}
${settings}`;
  } else {
    body = `${c.unfinished ? `<p class="error">Chat apps are not fully off: parts are still in Home Assistant.</p>${button("chat_apps_off", "Finish turning off", "", "secondary")}` : ""}
<p>Use HA NOVA in Claude, also in its phone app, and in ChatGPT on a computer. Once they're on, you pick who else in your home may use them.</p>
<details><summary>What you need</summary><p>Home Assistant Link or your own HTTPS address with a valid certificate. ChatGPT needs a paid plan that lets you add your own plugins (we tested Pro); on a phone, it can only answer.</p></details>
<p class="muted meta">Home Assistant restarts once if needed, always the first time (usually under a minute, longer on small devices).</p><div class="actions">${button("chat_apps_on", "Turn on chat apps")}</div>`;
  }
  return `<section><h2>Chat apps</h2>${body}</section>
`;
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
          : `Home Assistant Link (user ${escapeHtml(device.cloudUserId)})`;
      return `<section class="confirm"><h2>Confirm revoke</h2>
<p><strong>${escapeHtml(device.name)}</strong>${cloudBadge(device)}<span class="muted"> — ${escapeHtml(device.platform)} · ${escapeHtml(device.client)}</span></p>
<p class="muted meta">added ${escapeHtml(formatWhen(device.createdAtMs))} · last used ${escapeHtml(formatLastUsed(device.lastUsedAtMs))} · reaches NOVA via ${cloudLine}</p>
<p>Revoking cuts this computer's access immediately. It can pair again later with a new code.</p>
${formOpen}${csrfField("revoke_device", `revoke_device:${device.deviceId}`)}<input type="hidden" name="device_id" value="${escapeAttr(device.deviceId)}"><button class="danger-solid" type="submit">Revoke this device</button></form> ${cancel}</section>`;
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

const PLATFORM: Record<string, string> = { darwin: "macOS", windows: "Windows", linux: "Linux" };
function platformName(platform: string): string {
  return PLATFORM[platform] ?? platform;
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
