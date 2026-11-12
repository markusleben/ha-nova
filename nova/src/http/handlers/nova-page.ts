import type { IncomingMessage, ServerResponse } from "node:http";

import { ChatAppsError, type ChatApps, type ChatAppsStatus } from "../../runtime/chat-apps.js";
import { checkOwner, isOwner, type HaAuthUser, type OwnerCheckResult } from "../../security/owner-check.js";
import type { CsrfStore } from "../../security/csrf.js";
import type { DeviceRegistry } from "../../security/device-registry.js";
import { BRIDGE_INSTALL_ID, GATE_INSTALL_ID } from "../../security/pairing-metadata.js";
import type { PairingV1Manager } from "../../security/pairing-v1.js";
import type { GatePage } from "../gate-asks.js";
import type { RouteHandler } from "../router.js";
import { renderGateSection } from "./nova-approve.js";
import { LIVE_CSP, liveState } from "./nova-page-live.js";
import type { RelayLogger } from "../server.js";
import { applyChatPhoneAction, CHAT_PHONE_ACTIONS, chatPhoneErrorCode, readChatPhone, type ChatPhoneAction, type ChatPhoneModel } from "./nova-page-chat-phone.js";
import { applyChatUsers, chatPhoneUser, readChatUsers, type ChatUsersModel } from "./nova-page-chat-users.js";
import {
  escapeHtml,
  parseConfirm,
  renderPage,
  type ConnectionStatus,
  type NovaAction,
  type UpdateStatus,
} from "./nova-page-view.js";

// The NOVA console, served over Supervisor ingress, for Home Assistant
// administrators; chat apps and the registry reset stay with the owner
// (docs/work/2026-11-12-nova-page-admins-spec.md). Server-rendered HTML
// with NO JavaScript and no external resources; every mutating action is a POST
// form guarded by a single-use CSRF token, Sec-Fetch/Origin checks, and a fresh
// owner re-check. Post/Redirect/Get avoids re-submission. The visible name is
// only "NOVA"; the code is shown solely while a pairing is active and is never
// logged. Rendering lives in nova-page-view.ts; this module owns the HTTP,
// action, and CSRF handling.

export type { ConnectionStatus, NovaAction, UpdateStatus } from "./nova-page-view.js";

const CSP = [
  "default-src 'none'",
  "style-src 'unsafe-inline'",
  "img-src 'self' data:",
  "base-uri 'none'",
  "form-action 'self'",
  "frame-ancestors 'self'",
  LIVE_CSP,
].join("; ");

const ACTIONS = new Set<NovaAction>(["generate_code", "cancel_code", "revoke_device", "reset_registry"]);
const CHAT_ACTIONS = new Set<NovaAction>(["chat_apps_on", "chat_apps_off", "chat_read_only", "chat_codes", "chat_users"]);
// What an administrator who isn't the owner may do; every other action needs the owner.
const ADMIN_ACTIONS = new Set<string>(["generate_code", "cancel_code", "revoke_device", "resume_phone_asks"]);

export interface NovaPageDeps {
  fetchAuthUsers: () => Promise<HaAuthUser[]>;
  csrf: CsrfStore;
  pairing: PairingV1Manager;
  registry: DeviceRegistry;
  // Corrupt-registry recovery. Defaulted for standalone/tests where a corrupt
  // registry cannot occur (single-listener path has no registry).
  registryCorrupt?: () => boolean;
  resetRegistry?: () => void;
  connection: () => ConnectionStatus;
  update: () => Promise<UpdateStatus>;
  relayVersion: string;
  now: () => number;
  logger?: RelayLogger;
  chatApps?: ChatApps; // only with HA_NOVA_CHAT_APPS=1
  connectorUrl?: () => Promise<string | null>; // null: Link remote access isn't ready
  gatePage?: GatePage; // the gate's asks: "Waiting for your OK" and paused devices
}

function setHeaders(response: ServerResponse): void {
  response.setHeader("content-security-policy", CSP);
  response.setHeader("cache-control", "no-store");
  response.setHeader("x-content-type-options", "nosniff");
  response.setHeader("x-frame-options", "SAMEORIGIN");
  response.setHeader("referrer-policy", "no-referrer");
}

function fail(response: ServerResponse, status: number, message: string, extra = ""): void {
  setHeaders(response);
  response.statusCode = status;
  response.setHeader("content-type", "text/html; charset=utf-8");
  response.end(`<!doctype html><meta charset="utf-8"><title>NOVA</title><p>${escapeHtml(message)}</p>${extra}`);
}

// Someone who isn't an administrator (Home Assistant shows NOVA to admins
// only, so this is rare): name whom to ask (fail() escapes the name).
function refuse(response: ServerResponse, owner: Extract<OwnerCheckResult, { ok: false }>, approve = ""): void {
  const them = owner.ownerName ? `the owner, ${owner.ownerName}, or another administrator` : "an administrator of this Home Assistant";
  fail(response, owner.status, owner.status === 503 ? "Could not verify your access. Try again shortly."
    : owner.unknownUser ? "Couldn't tell who you are. Open NOVA from the Home Assistant sidebar." : `Only Home Assistant administrators can open NOVA. Ask ${them} to open it and click Connect a device. That shows a six-digit code for your computer.`, approve);
}

// The owner's name for an administrator's page and refusals ("" unknown).
function ownerOf(users: HaAuthUser[]): string {
  const name = users.find(isOwner)?.name;
  return typeof name === "string" ? name.trim() : "";
}

export function createNovaPageHandler(deps: NovaPageDeps): RouteHandler {
  return async ({ request, response }) => {
    const owner = await checkOwner(request, { fetchAuthUsers: deps.fetchAuthUsers, logger: deps.logger }, { admins: true });
    if (!owner.ok) return refuse(response, owner);
    const ingressPath = singleHeader(request, "x-ingress-path") ?? "/home";
    const now = deps.now();
    // A paused device's "ask on the phone again": a POST whose token is bound to that device.
    const resumeForm = (deviceId: string): string =>
      `<form method="post" action="${escapeHtml(`${ingressPath.replace(/\/$/, "")}/action`)}"><input type="hidden" name="csrf" value="${escapeHtml(deps.csrf.issue(owner.userId, `resume_phone_asks:${deviceId}`, now))}"><input type="hidden" name="action" value="resume_phone_asks"><input type="hidden" name="device_id" value="${escapeHtml(deviceId)}">`;
    // The live digest of the asks the gate section reads now, so one that
    // arrives while the page renders differs from it and the poll reloads.
    const live = deps.gatePage === undefined ? null : liveState(deps.gatePage);
    const [update, gateSection] = await Promise.all([
      safeUpdate(deps),
      deps.gatePage === undefined ? "" : renderGateSection(deps.gatePage, deps.registry.list(), ingressPath, now, resumeForm),
    ]);
    const query = new URLSearchParams((request.url ?? "").split("?")[1] ?? "");
    // The bridge's internal device is shown under Chat apps, never as a
    // computer that could be revoked by mistake. Unreadable chat-app files
    // hide that section (logged); the rest of the console keeps working.
    let bridgeDevice: string | null = null;
    let chatApps: ChatAppsStatus | null = null;
    try {
      bridgeDevice = deps.chatApps?.bridgeDeviceId() ?? null;
      chatApps = owner.isOwner ? deps.chatApps?.status(owner.userId) ?? null : null; // an administrator's page reads no chat-app files
    } catch (error) {
      deps.logger?.error("Chat-app files are unreadable", { error: error instanceof Error ? error.message : String(error) });
    }
    // The household list, and the owner's phone or that of the listed user whose row the owner opened.
    const chatOn = chatApps?.enabled === true && chatApps.available;
    const asked = query.get("chat_user");
    const chatWho = chatOn ? chatPhoneUser(asked, chatApps!, owner.userId, owner.users) : null;
    // Someone removed or lowered meanwhile: never their pick list or job as the owner's.
    const chatWhoGone = chatOn && asked !== null && asked !== owner.userId && chatWho === null;
    const [chatPhone, chatUsers]: [ChatPhoneModel | null, ChatUsersModel | null] = chatOn
      ? await Promise.all([readChatPhone(deps.chatApps!.phone, chatWho?.id ?? owner.userId, chatWhoGone ? new URLSearchParams() : query), readChatUsers(deps.chatApps!, owner.userId, owner.users, query)])
      : [null, null];
    const chatConnectorUrl = chatApps?.enabled // unknown: the page words it without the address
      ? await deps.connectorUrl?.().catch((error: unknown) => (deps.logger?.warn("connector address unavailable", { error: String(error) }), null)) ?? null
      : null;
    const html = renderPage({
      ownerName: owner.name,
      isOwner: owner.isOwner,
      chatFeature: deps.chatApps !== undefined,
      houseOwner: owner.isOwner ? owner.name : ownerOf(owner.users),
      csrf: deps.csrf,
      userId: owner.userId,
      now,
      pairing: deps.pairing.getStatus(),
      devices: deps.registry.list().filter((d) => d.deviceId !== bridgeDevice && d.clientInstallId !== GATE_INSTALL_ID),
      registryCorrupt: deps.registryCorrupt?.() ?? false,
      connection: deps.connection(),
      update,
      relayVersion: deps.relayVersion,
      ingressPath,
      // The action handler redirects here with ?err=<code> when an owner action
      // threw ("1") or the typed reset confirmation did not match ("confirm").
      errorCode: query.get("err"),
      confirm: owner.isOwner || query.get("confirm") !== "reset_registry" ? parseConfirm(query) : null,
      chatApps,
      chatConnectorUrl,
      chatPhone,
      chatPhoneUser: chatWho,
      chatPhoneUserGone: chatWhoGone,
      chatUsers,
      chatRestarting: query.get("chat") === "restarting",
      gateSection,
      live,
    });
    setHeaders(response);
    response.statusCode = 200;
    response.setHeader("content-type", "text/html; charset=utf-8");
    response.end(html);
  };
}

export function createNovaActionHandler(deps: NovaPageDeps): RouteHandler {
  return async ({ request, response, body }) => {
    const owner = await checkOwner(request, { fetchAuthUsers: deps.fetchAuthUsers, logger: deps.logger }, { admins: true });
    if (!owner.ok) return refuse(response, owner);
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
    const phoneAction = CHAT_PHONE_ACTIONS.has(action);
    if (!ACTIONS.has(action as NovaAction) && !(deps.chatApps !== undefined && (CHAT_ACTIONS.has(action as NovaAction) || phoneAction)) && !(deps.gatePage !== undefined && action === "resume_phone_asks")) {
      fail(response, 400, "Unknown action.");
      return;
    }
    // Before the token is spent: an administrator's owner action changes nothing.
    if (!owner.isOwner && !ADMIN_ACTIONS.has(action)) {
      const name = ownerOf(owner.users);
      fail(response, 403, `Only ${name ? `the owner, ${name},` : "the owner of this Home Assistant"} can do this. Nothing was changed.`);
      return;
    }
    // Destructive actions are two-step: their CSRF token is issued ONLY by the
    // confirm screen, and the revoke token is bound to the exact device id — a
    // token armed for one device cannot revoke another, and no token for these
    // actions exists outside an open confirm screen.
    const deviceId = typeof form.device_id === "string" ? form.device_id : "";
    // A phone form names its user (the owner, or one on the list), and its token is bound to that user.
    const phoneUser = !phoneAction ? "" : typeof form.user === "string" ? form.user : owner.userId;
    const csrfAction =
      action === "revoke_device" || action === "resume_phone_asks" ? `${action}:${deviceId}` : phoneAction ? `${action}:${phoneUser}` : action;
    if (!deps.csrf.consume(owner.userId, csrfAction, token, deps.now())) {
      fail(response, 403, "This form expired. Reload the page and try again.");
      return;
    }

    let errCode: string | null = null;
    let restart = false;
    let job: string | null = null; // a chat-app phone change that asks a phone: the page shows it
    let removed: string[] = []; // the users a save of the household list removed
    // The strongest gate: resetting the registry additionally requires the
    // owner to type RESET verbatim on the confirm screen. The consumed token
    // stays consumed on a mismatch — re-arming means opening the screen again.
    if (action === "reset_registry" && form.confirm_text !== "RESET") {
      errCode = "confirm";
    } else {
      try {
        if (phoneAction) {
          if (phoneUser !== owner.userId && chatPhoneUser(phoneUser, deps.chatApps!.status(owner.userId), owner.userId, owner.users) === null) {
            throw new Error("that phone form's user can't have a chat-app phone now");
          }
          job = await applyChatPhoneAction(deps.chatApps!, phoneUser, action as ChatPhoneAction, form);
        } else if (action === "chat_users") {
          // owner.users: Home Assistant's users as read for this request (checkOwner, never cached).
          removed = await applyChatUsers(deps.chatApps!, owner.userId, owner.users, form);
        } else {
          restart = await applyAction(deps, action as NovaAction, form);
        }
        // Who connected or revoked a computer: any administrator may, so the log names them.
        if (ADMIN_ACTIONS.has(action)) {
          deps.logger?.info?.("NOVA page action", { action, user_id: owner.userId, ...(deviceId ? { device_id: deviceId } : {}) });
        }
      } catch (error) {
        // Do not leak internals, but tell the owner it failed via ?err=1 below
        // so a failed "Connect a device" is not a silent reload with no code —
        // and log the cause, which the page deliberately hides.
        deps.logger?.error("NOVA action failed", {
          action,
          error: error instanceof Error ? error.message : String(error),
        });
        errCode = phoneAction ? chatPhoneErrorCode(error) : CHAT_ACTIONS.has(action as NovaAction) ? chatErrorCode(error) : "1";
      }
    }

    // Post/Redirect/Get back to the page. The redirect target MUST keep the
    // ingress base path's trailing slash: Home Assistant serves the console at
    // "<ingress-path>/" (forwarding GET /) but 404s the slash-less base path.
    const ingressPath = singleHeader(request, "x-ingress-path") ?? "";
    const base = `${ingressPath.replace(/\/$/, "")}/`;
    setHeaders(response);
    response.statusCode = 303;
    // A RESET mismatch redirects back INTO the armed confirm screen (fresh
    // token) so the error's "type RESET" instruction has an input to point at.
    const who = phoneUser !== "" && phoneUser !== owner.userId ? `&chat_user=${encodeURIComponent(phoneUser)}#chat-phone` : "";
    const location =
      errCode === null
        ? restart ? `${base}?chat=restarting`
          : action === "chat_users" ? `${base}?chat_users=saved${removed.length === 0 ? "" : `&chat_removed=${removed.map(encodeURIComponent).join(",")}`}#chat-users`
          : job !== null ? `${base}?chat_job=${job}${who}` : action === "chat_phone_remove" ? `${base}?chat_phone=stopped${who}` : base
        : errCode === "confirm"
          ? `${base}?err=confirm&confirm=reset_registry`
          : `${base}?err=${errCode}${who}`;
    response.setHeader("location", location);
    response.end();
    if (restart) {
      deps.chatApps?.restartSoon(); // after the redirect: HA carries the ingress connection
    }
  };
}

// Returns whether Home Assistant must restart (turning chat apps on).
async function applyAction(deps: NovaPageDeps, action: NovaAction, form: Record<string, unknown>): Promise<boolean> {
  const chat = deps.chatApps!;
  switch (action as Exclude<NovaAction, ChatPhoneAction | "chat_users">) {
    case "chat_apps_on":
      // The button carries the consent line "Home Assistant restarts once if needed".
      return (await chat.turnOn(true)).restart;
    case "chat_apps_off":
      await chat.turnOff();
      return false;
    case "chat_read_only":
      chat.setSwitches({ readOnly: onOff(form.value) });
      return false;
    case "chat_codes":
      chat.setSwitches({ chatCodes: onOff(form.value) });
      return false;
    case "generate_code":
      deps.pairing.generateCode();
      return false;
    case "resume_phone_asks": {
      deps.gatePage?.resume(typeof form.device_id === "string" ? form.device_id : ""); // false: the pause already ended
      return false;
    }
    case "cancel_code":
      deps.pairing.cancel();
      return false;
    case "revoke_device": {
      const deviceId = typeof form.device_id === "string" ? form.device_id : "";
      const device = deps.registry.list().find((d) => d.deviceId === deviceId);
      if (device !== undefined && (device.clientInstallId === BRIDGE_INSTALL_ID || device.clientInstallId === GATE_INSTALL_ID)) {
        throw new Error("the App's own connections are not computers: they can't be revoked here");
      }
      if (deviceId) {
        deps.registry.revoke(deviceId);
      }
      return false;
    }
    case "reset_registry":
      // Recover from a corrupt registry: archive the damaged file and start a
      // fresh, empty one so pairing works again.
      deps.resetRegistry?.();
      return false;
  }
}

function onOff(value: unknown): boolean {
  if (value !== "on" && value !== "off") {
    throw new Error("a switch needs on or off");
  }
  return value === "on";
}

// One plain line per refusal the owner can act on (nova-page-view.ts).
function chatErrorCode(error: unknown): string {
  const code = error instanceof ChatAppsError ? error.code : "";
  return code === "HA_CONFIG_INVALID" ? "chat_config" : code === "HA_TOO_OLD" ? "chat_old" : code.startsWith("CONFIG_") ? "chat_folder"
    : code === "USER_NOT_PICKABLE" || code === "LEVEL_ABOVE_ROLE" ? "chat_users" : code === "OLD_LINK_STAYS" ? "chat_users_link" : "chat";
}

async function safeUpdate(deps: NovaPageDeps): Promise<UpdateStatus> {
  try {
    return await deps.update();
  } catch (error) {
    deps.logger?.warn("update status unavailable", {
      error: error instanceof Error ? error.message : String(error),
    });
    return { version: deps.relayVersion, versionLatest: null, updateAvailable: false, error: true };
  }
}

function singleHeader(request: IncomingMessage, name: string): string | null {
  const value = request.headers[name];
  return typeof value === "string" ? value : null;
}

