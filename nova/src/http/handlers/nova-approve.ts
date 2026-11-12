import type { IncomingMessage, ServerResponse } from "node:http";

import type { GatePage, PageAsk } from "../gate-asks.js";
import type { PageKey, PageTexts } from "../page-texts.js";
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
// which can approve or cancel but never wait for the grant. Every word comes
// from the worker (page-texts.ts); without them the page answers 503 with
// none. A button's approval waits for no words: nothing between the check
// and the grant.

const CSP = ["default-src 'none'", "style-src 'unsafe-inline'", "base-uri 'none'", "form-action 'self'", "frame-ancestors 'none'"].join("; ");
const SHOWN = 3; // two tokens each: two people's pages fit the CSRF store's 16
const DONE = new Map<string, PageKey>([["approved", "page.done_approved"], ["cancelled", "page.done_cancelled"], ["gone", "page.done_gone"]]);
type WsClient = { sendMessage(message: { type: string }): Promise<unknown> };

export interface ApprovePageDeps {
  fetchAuthUsers: () => Promise<HaAuthUser[]>;
  relayUserId: () => Promise<string>; // auth/current_user of the Relay's own connection
  csrf: CsrfStore;
  page: GatePage;
  now: () => number;
  logger?: RelayLogger;
}

// t null: the worker gave no words, so the page says nothing (503).
function send(response: ServerResponse, status: number, t: PageTexts | null, body = ""): void {
  response.setHeader("content-security-policy", CSP);
  response.setHeader("x-frame-options", "DENY");
  response.setHeader("cache-control", "no-store");
  response.setHeader("x-content-type-options", "nosniff");
  response.setHeader("referrer-policy", "no-referrer");
  response.statusCode = t === null ? 503 : status;
  response.setHeader("content-type", "text/html; charset=utf-8");
  if (t === null) {
    response.end();
    return;
  }
  response.end(`<!doctype html><html lang="${t.lang}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>${t.say("page.title")}</title>
<style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:2rem auto;padding:0 1rem;line-height:1.45}section{border:1px solid #8884;border-radius:12px;padding:1rem;margin:1rem 0}.muted{opacity:.7}button{font:inherit;padding:.45rem .9rem;border-radius:10px;margin-right:.5rem}code{font-size:.9em}</style></head><body><main><h1>${t.say("page.heading")}</h1>${body}<p><a href="/">${t.say("page.back")}</a></p></main></body></html>`);
}

function base(request: IncomingMessage): string {
  const path = request.headers["x-ingress-path"];
  return `${(typeof path === "string" ? path : "").replace(/\/$/, "")}/`;
}

function renderAsk(ask: PageAsk, form: (action: string) => string, now: number, t: PageTexts): string {
  const minutes = Math.max(1, Math.ceil((ask.expiresAt - now) / 60_000));
  const items = ask.items.map((item) => `<li>${escapeHtml(item.text ?? "")}${item.detail ? ` <span class="muted">${escapeHtml(item.detail)}</span>` : ""}${(item.entity_ids ?? []).map((id) => ` <code>${escapeHtml(id)}</code>`).join("")}</li>`).join("");
  return `<section><ul>${items}</ul><p>${escapeHtml(ask.body)} <span class="muted">${t.say("page.ends_in", { minutes })}</span></p>${form("approve")}<button type="submit">${t.say("page.approve")}</button></form>${form("cancel")}<button type="submit">${t.say("window.cancel")}</button></form></section>`;
}

export function createApprovePageHandler(deps: ApprovePageDeps): RouteHandler {
  return async ({ request, response }) => {
    const who = await checkApprover(request, deps);
    const t = await deps.page.texts(who.ok ? who.name : "");
    if (t === null) return send(response, 503, null);
    if (!who.ok) return send(response, who.status, t, `<p>${t.say(who.text)}</p>`);
    const done = DONE.get(new URLSearchParams((request.url ?? "").split("?")[1] ?? "").get("done") ?? "");
    // An ask the worker didn't word isn't shown: nobody approves what they can't read.
    const all = deps.page.waiting().filter((ask) => ask.body !== "" && ask.items.length > 0 && ask.items.every((item) => (item.text ?? "") !== "")).sort((a, b) => a.expiresAt - b.expiresAt);
    const asks = all.slice(0, SHOWN);
    const more = all.length > SHOWN ? `<p class="muted">${t.say("page.more", { count: all.length - SHOWN })}</p>` : "";
    const now = deps.now();
    const action = `${base(request)}approve`;
    const html = asks.map((ask) => renderAsk(ask, (kind) => `<form method="post" action="${escapeHtml(action)}"><input type="hidden" name="action" value="${kind}"><input type="hidden" name="ref" value="${ask.ref}"><input type="hidden" name="csrf" value="${escapeHtml(deps.csrf.issue(who.userId, `${kind}:${ask.ref}`, now))}">`, now, t)).join("");
    send(response, 200, t, `${done ? `<p><strong>${t.say(done)}</strong></p>` : ""}${html || `<p class="muted">${t.say("page.empty")}</p>`}${more}<p class="muted">${t.say("page.signed_in")}</p>`);
  };
}

export function createApproveActionHandler(deps: ApprovePageDeps): RouteHandler {
  return async ({ request, response, body }) => {
    const refuse = async (status: 403 | 503, words: (t: PageTexts) => string): Promise<void> => {
      const t = await deps.page.texts("");
      send(response, status, t, t === null ? "" : words(t));
    };
    const who = await checkApprover(request, deps);
    if (!who.ok) {
      const key = who.text;
      return await refuse(who.status, (t) => `<p>${t.say(key)}</p>`);
    }
    const site = request.headers["sec-fetch-site"];
    if (typeof site === "string" && site !== "same-origin" && site !== "same-site" && site !== "none") {
      return await refuse(403, (t) => `<p>${t.say("page.cross_site")}</p>`);
    }
    const form = (body ?? {}) as Record<string, unknown>;
    const action = form.action === "approve" || form.action === "cancel" ? form.action : null;
    const ref = typeof form.ref === "string" ? form.ref : "";
    if (action === null || !deps.csrf.consume(who.userId, `${action}:${ref}`, typeof form.csrf === "string" ? form.csrf : "", deps.now())) {
      return await refuse(403, (t) => `<p>${t.say("page.expired")} <a href="${escapeHtml(`${base(request)}approve`)}">${t.say("page.open_again")}</a></p>`);
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
    csrf: createCsrfStore(64), // its own, roomy: several people's open pages keep their buttons
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
// the owner page's own revoke step. Without the worker's words: none.
export async function renderGateSection(page: GatePage, devices: DeviceRecord[], ingressPath: string): Promise<string> {
  const root = `${ingressPath.replace(/\/$/, "")}/`;
  const waiting = page.waiting().length;
  const paused = page.pausedDevices().flatMap((id) => devices.filter((d) => d.deviceId === id));
  const t = waiting === 0 && paused.length === 0 ? null : await page.texts("");
  if (t === null) return "";
  const open = waiting === 0 ? "" : `<p>${waiting === 1 ? t.say("page.waiting_one") : t.say("page.waiting_many", { count: waiting })} <a class="linkbtn" target="_top" href="${escapeHtml(`${root}approve`)}">${t.say("page.open")}</a></p>`;
  const rows = paused.map((d) => `<li><strong>${escapeHtml(d.name)}</strong>: <span class="muted">${t.say("page.paused")}</span> <a class="linkbtn danger" href="${escapeHtml(`${root}?confirm=revoke_device&device=${encodeURIComponent(d.deviceId)}`)}">${t.say("page.revoke_device")}</a></li>`).join("");
  return `<section><h2>${t.say("page.heading")}</h2>${open}${rows ? `<ul>${rows}</ul>` : ""}</section>`;
}

