import type { IncomingMessage, ServerResponse } from "node:http";

import type { GatePage, PageAsk } from "../gate-asks.js";
import { createCsrfStore, type CsrfStore } from "../../security/csrf.js";
import type { DeviceRecord } from "../../security/device-registry.js";
import { checkApprover, isApprover, type HaAuthUser } from "../../security/owner-check.js";
import type { RouteHandler } from "../router.js";
import type { RelayLogger } from "../server.js";
import { escapeHtml } from "./nova-page-view.js";

// The approval view (docs/work/2026-11-03-relay-typed-gate-spec.md §3, item 2):
// "Waiting for your OK", over Supervisor ingress only. Any active, non-system
// administrator except the Relay's own user approves or cancels, re-checked
// fresh on every request; each button's single-use token is bound to that
// person and that ask. No dashboard card can frame it (frame-ancestors 'none'),
// so the owner page links to it top-level. Asks appear by ref (the id's hash),
// which can approve or cancel but never wait for the grant.

const CSP = ["default-src 'none'", "style-src 'unsafe-inline'", "base-uri 'none'", "form-action 'self'", "frame-ancestors 'none'"].join("; ");
const SHOWN = 3; // two tokens each: two people's pages fit the CSRF store's 16
const DONE = new Map(Object.entries({
  approved: "Approved. The computer that asked sends it now.",
  cancelled: "Cancelled. Nothing was sent.",
  gone: "That request had already ended. Nothing changed.",
}));
type WsClient = { sendMessage(message: { type: string }): Promise<unknown> };

export interface ApprovePageDeps {
  fetchAuthUsers: () => Promise<HaAuthUser[]>;
  relayUserId: () => Promise<string>; // auth/current_user of the Relay's own connection
  csrf: CsrfStore;
  page: GatePage;
  now: () => number;
  logger?: RelayLogger;
}

function send(response: ServerResponse, status: number, body: string): void {
  response.setHeader("content-security-policy", CSP);
  response.setHeader("x-frame-options", "DENY");
  response.setHeader("cache-control", "no-store");
  response.setHeader("x-content-type-options", "nosniff");
  response.setHeader("referrer-policy", "no-referrer");
  response.statusCode = status;
  response.setHeader("content-type", "text/html; charset=utf-8");
  response.end(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>HA NOVA: waiting for your OK</title>
<style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:2rem auto;padding:0 1rem;line-height:1.45}section{border:1px solid #8884;border-radius:12px;padding:1rem;margin:1rem 0}.muted{opacity:.7}button{font:inherit;padding:.45rem .9rem;border-radius:10px;margin-right:.5rem}code{font-size:.9em}</style></head><body><main><h1>Waiting for your OK</h1>${body}<p><a href="/">Back to Home Assistant</a></p></main></body></html>`);
}

function base(request: IncomingMessage): string {
  const path = request.headers["x-ingress-path"];
  return `${(typeof path === "string" ? path : "").replace(/\/$/, "")}/`;
}

function renderAsk(ask: PageAsk, form: (action: string) => string, now: number): string {
  const minutes = Math.max(1, Math.ceil((ask.expiresAt - now) / 60_000));
  const items = ask.items.map((item) => `<li>${escapeHtml(item.text ?? "")}${item.detail ? ` <span class="muted">${escapeHtml(item.detail)}</span>` : ""}${(item.entity_ids ?? []).map((id) => ` <code>${escapeHtml(id)}</code>`).join("")}</li>`).join("");
  return `<section><p>From <strong>${escapeHtml(ask.device)}</strong> <span class="muted">· ends in ${minutes} min</span></p><ul>${items}</ul>${form("approve")}<button type="submit">Approve</button></form>${form("cancel")}<button type="submit">Cancel</button></form></section>`;
}

export function createApprovePageHandler(deps: ApprovePageDeps): RouteHandler {
  return async ({ request, response }) => {
    const who = await checkApprover(request, deps);
    if (!who.ok) return send(response, who.status, `<p>${escapeHtml(who.message)}</p>`);
    const done = DONE.get(new URLSearchParams((request.url ?? "").split("?")[1] ?? "").get("done") ?? "");
    // An ask the worker didn't word for every typed request isn't shown: nobody approves what they can't read.
    const all = deps.page.waiting().filter((ask) => ask.items.length > 0 && ask.items.every((item) => (item.text ?? "") !== "")).sort((a, b) => a.expiresAt - b.expiresAt);
    const asks = all.slice(0, SHOWN);
    const more = all.length > SHOWN ? `<p class="muted">${all.length - SHOWN} more after these.</p>` : "";
    const now = deps.now();
    const action = `${base(request)}approve`;
    const html = asks.map((ask) => renderAsk(ask, (kind) => `<form method="post" action="${escapeHtml(action)}"><input type="hidden" name="action" value="${kind}"><input type="hidden" name="ref" value="${ask.ref}"><input type="hidden" name="csrf" value="${escapeHtml(deps.csrf.issue(who.userId, `${kind}:${ask.ref}`, now))}">`, now)).join("");
    send(response, 200, `${done ? `<p><strong>${done}</strong></p>` : ""}${html || '<p class="muted">Nothing is waiting for your OK. When your computer asks, reload this page.</p>'}${more}<p class="muted">Signed in as ${escapeHtml(who.name)}. Approve only what you asked your AI app to do.</p>`);
  };
}

export function createApproveActionHandler(deps: ApprovePageDeps): RouteHandler {
  return async ({ request, response, body }) => {
    const who = await checkApprover(request, deps);
    if (!who.ok) return send(response, who.status, `<p>${escapeHtml(who.message)}</p>`);
    const site = request.headers["sec-fetch-site"];
    if (typeof site === "string" && site !== "same-origin" && site !== "same-site" && site !== "none") {
      return send(response, 403, "<p>Cross-site request refused.</p>");
    }
    const form = (body ?? {}) as Record<string, unknown>;
    const action = form.action === "approve" || form.action === "cancel" ? form.action : null;
    const ref = typeof form.ref === "string" ? form.ref : "";
    if (action === null || !deps.csrf.consume(who.userId, `${action}:${ref}`, typeof form.csrf === "string" ? form.csrf : "", deps.now())) {
      return send(response, 403, `<p>This form expired. <a href="${escapeHtml(`${base(request)}approve`)}">Open the requests again</a>.</p>`);
    }
    const ended = action === "approve" ? deps.page.approve(ref) : deps.page.cancel(ref);
    if (ended) {
      deps.logger?.info?.(action === "approve" ? "A typed request was approved on the NOVA page" : "A typed request was cancelled on the NOVA page", { user_id: who.userId });
    }
    response.statusCode = 303;
    response.setHeader("cache-control", "no-store");
    response.setHeader("location", `${base(request)}approve?done=${ended ? (action === "approve" ? "approved" : "cancelled") : "gone"}`);
    response.end();
  };
}

// The view's routes on the ingress listener (identity auth: Supervisor
// ingress only), with the Relay's own user read fresh over its connection.
export function approveRoutes(input: Omit<ApprovePageDeps, "relayUserId" | "csrf"> & { wsClient: WsClient }): ["GET" | "POST", string, "none" | "form", RouteHandler][] {
  const deps: ApprovePageDeps = {
    ...input,
    relayUserId: () => relayUserId(input.wsClient),
    csrf: createCsrfStore(), // its own: the view's tokens never evict the owner page's
  };
  const [view, act] = [createApprovePageHandler(deps), createApproveActionHandler(deps)];
  return ["", "/home"].flatMap((prefix): ["GET" | "POST", string, "none" | "form", RouteHandler][] => [["GET", prefix + "/approve", "none", view], ["POST", prefix + "/approve", "form", act]]);
}

async function relayUserId(ws: WsClient): Promise<string> {
  const id = ((await ws.sendMessage({ type: "auth/current_user" })) as { id?: unknown } | null)?.id;
  if (typeof id !== "string" || id === "") {
    throw new Error("auth/current_user returned no user id");
  }
  return id;
}

// The page is a channel only while someone may approve there (an
// administrator other than the Relay's own user); a failed read: not now.
export async function pageChannel(ws: WsClient): Promise<string[]> {
  try {
    const [users, relay] = await Promise.all([ws.sendMessage({ type: "config/auth/list" }), relayUserId(ws)]);
    return Array.isArray(users) && (users as HaAuthUser[]).some((user) => isApprover(user, relay)) ? ["page"] : [];
  } catch {
    return [];
  }
}

// The owner page's part: how many asks wait (a top-level link, so the view
// can't be framed) and the devices whose phone asks are paused, each with
// the owner page's own revoke step.
export function renderGateSection(page: GatePage, devices: DeviceRecord[], ingressPath: string): string {
  const root = `${ingressPath.replace(/\/$/, "")}/`;
  const waiting = page.waiting().length;
  const paused = page.pausedDevices().flatMap((id) => devices.filter((d) => d.deviceId === id));
  if (waiting === 0 && paused.length === 0) return "";
  const open = waiting === 0 ? "" : `<p>${waiting === 1 ? "One request" : `${waiting} requests`} from your computers ${waiting === 1 ? "waits" : "wait"} for your OK. <a class="linkbtn" target="_top" href="${escapeHtml(`${root}approve`)}">Open</a></p>`;
  const rows = paused.map((d) => `<li><strong>${escapeHtml(d.name)}</strong> <span class="muted">three of its requests in a row were cancelled or went unanswered, so its phone asks pause for 15 minutes</span> <a class="linkbtn danger" href="${escapeHtml(`${root}?confirm=revoke_device&device=${encodeURIComponent(d.deviceId)}`)}">Revoke this device…</a></li>`).join("");
  return `<section><h2>Waiting for your OK</h2>${open}${rows ? `<ul>${rows}</ul>` : ""}</section>`;
}

// The link a non-owner administrator gets on the owner page's refusal.
export function approveLink(request: IncomingMessage): string {
  return `<p><a target="_top" href="${escapeHtml(`${base(request)}approve`)}">Requests waiting for your OK</a></p>`;
}
