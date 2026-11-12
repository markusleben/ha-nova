import type { IncomingMessage, ServerResponse } from "node:http";

import { ASK_MS } from "../gate-ask-table.js";
import type { AskItem, GatePage, PageAsk } from "../gate-asks.js";
import type { PageKey, PageTexts } from "../page-texts.js";
import { createCsrfStore, type CsrfStore } from "../../security/csrf.js";
import type { DeviceRecord } from "../../security/device-registry.js";
import { checkApprover, isApprover, type HaAuthUser } from "../../security/owner-check.js";
import type { RouteHandler } from "../router.js";
import type { RelayLogger } from "../server.js";
import { createLiveHandler, LIVE_CSP, liveScript, liveState } from "./nova-page-live.js";
import { PAGE_CSS } from "./nova-page-style.js";
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
const LEAVE_S = 4; // nothing left to answer: back to NOVA after this many seconds
const SLUG = /^[a-z0-9_]+$/;
const DONE = new Map<string, PageKey>([["approved", "page.done_approved"], ["cancelled", "page.done_cancelled"], ["gone", "page.done_gone"]]);
type WsClient = { sendMessage(message: { type: string }): Promise<unknown> };

export interface ApprovePageDeps {
  fetchAuthUsers: () => Promise<HaAuthUser[]>;
  relayUserId: () => Promise<string>; // auth/current_user of the Relay's own connection
  csrf: CsrfStore;
  page: GatePage;
  panel: () => Promise<string>; // NOVA's sidebar panel, where the page leads back to
  now: () => number;
  logger?: RelayLogger;
}

// t null: the worker gave no words, so the page says nothing (503). back:
// NOVA's panel; leave: nothing left to answer, so go there on its own. live:
// the body's attributes for the live poll (nova-page-live.ts).
function send(response: ServerResponse, status: number, t: PageTexts | null, back = "/", body = "", live = "", leave = false): void {
  response.setHeader("content-security-policy", `${CSP}; ${LIVE_CSP}`);
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
${leave ? `<meta http-equiv="refresh" content="${LEAVE_S};url=${escapeHtml(back)}">` : ""}<style>${PAGE_CSS}</style></head><body${live}><main><h1>${t.say("page.heading")}</h1>${body}${leave ? `<p class="muted meta">${t.say("page.returning")}</p>` : ""}<p><a href="${escapeHtml(back)}">${t.say("page.back")}</a></p></main>${liveScript()}</body></html>`);
}

function base(request: IncomingMessage): string {
  const path = request.headers["x-ingress-path"];
  return `${(typeof path === "string" ? path : "").replace(/\/$/, "")}/`;
}

// One card per ask: what, once and large; from which computer; the time
// left (counted down live, a bar without the script); Approve and Cancel.
function renderAsk(ask: PageAsk, form: (action: string) => string, now: number, t: PageTexts): string {
  const left = Math.max(0, Math.round((ask.expiresAt - now) / 1000));
  const item = (i: AskItem): string => `${escapeHtml(i.text ?? "")}${(i.entity_ids ?? []).filter((id) => !(i.text ?? "").includes(id)).map((id) => ` <code>${escapeHtml(id)}</code>`).join("")}${i.detail ? `<span class="detail muted meta">${escapeHtml(i.detail)}</span>` : ""}`;
  const what = ask.items.length === 1 ? `<p class="what">${item(ask.items[0]!)}</p>` : `<ol class="what">${ask.items.map((i) => `<li>${item(i)}</li>`).join("")}</ol>`;
  const head = `<span class="muted meta">${escapeHtml(ask.from)}</span><span class="timer" title="${t.say("page.ends_in", { minutes: Math.max(1, Math.ceil(left / 60)) })}" data-left="${left}">${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}</span>`;
  return `<section class="ask alert"><div class="ask-head">${head}</div>${what}<div class="actions">${form("approve")}<button type="submit">${t.say("page.approve")}</button></form>${form("cancel")}<button class="secondary" type="submit">${t.say("window.cancel")}</button></form></div><div class="bar"><span style="--from:${Math.min(1, (left * 1000) / ASK_MS).toFixed(3)};animation-duration:${left}s"></span></div></section>`;
}

export function createApprovePageHandler(deps: ApprovePageDeps): RouteHandler {
  return async ({ request, response }) => {
    const who = await checkApprover(request, deps);
    const t = await deps.page.texts(who.ok ? who.name : "");
    if (t === null) return send(response, 503, null);
    const back = await deps.panel();
    if (!who.ok) return send(response, who.status, t, back, `<p>${t.say(who.text)}</p>`);
    const done = DONE.get(new URLSearchParams((request.url ?? "").split("?")[1] ?? "").get("done") ?? "");
    // An ask the worker didn't word isn't shown: nobody approves what they can't read.
    const all = deps.page.waiting().filter((ask) => ask.body !== "" && ask.items.length > 0 && ask.items.every((item) => (item.text ?? "") !== "")).sort((a, b) => a.expiresAt - b.expiresAt);
    const asks = all.slice(0, SHOWN);
    const more = all.length > SHOWN ? `<p class="muted">${t.say("page.more", { count: all.length - SHOWN })}</p>` : "";
    const now = deps.now();
    const root = base(request);
    const action = `${root}approve`;
    const html = asks.map((ask) => renderAsk(ask, (kind) => `<form method="post" action="${escapeHtml(action)}"><input type="hidden" name="action" value="${kind}"><input type="hidden" name="ref" value="${ask.ref}"><input type="hidden" name="csrf" value="${escapeHtml(deps.csrf.issue(who.userId, `${kind}:${ask.ref}`, now))}">`, now, t)).join("");
    const live = ` data-live="${escapeHtml(`${root}live`)}" data-state="${liveState(deps.page)}" data-reload="${escapeHtml(action)}"`;
    send(response, 200, t, back, `${done ? `<p class="ok"><strong>${t.say(done)}</strong></p>` : ""}${html || `<section><p class="muted">${t.say("page.empty")}</p></section>`}${more}<p class="muted meta">${t.say("page.signed_in")}</p>`, live, html === "");
  };
}

export function createApproveActionHandler(deps: ApprovePageDeps): RouteHandler {
  return async ({ request, response, body }) => {
    const refuse = async (status: 403 | 503, words: (t: PageTexts) => string): Promise<void> => {
      const t = await deps.page.texts("");
      send(response, status, t, t === null ? "/" : await deps.panel(), t === null ? "" : words(t));
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
export function approveRoutes(input: Omit<ApprovePageDeps, "relayUserId" | "csrf" | "panel"> & { wsClient: WsClient; slug: () => Promise<string> }): ["GET" | "POST", string, "none" | "form", RouteHandler][] {
  const deps: ApprovePageDeps = {
    ...input,
    relayUserId: () => relayUserId(input.wsClient),
    panel: novaPanel(input.slug, input.logger),
    csrf: createCsrfStore(64), // its own, roomy: several people's open pages keep their buttons
  };
  const [view, act, live] = [createApprovePageHandler(deps), createApproveActionHandler(deps), createLiveHandler({ fetchAuthUsers: input.fetchAuthUsers, page: input.page, ...(input.logger ? { logger: input.logger } : {}) })];
  return ["", "/home"].flatMap((prefix): ["GET" | "POST", string, "none" | "form", RouteHandler][] => [["GET", prefix + "/approve", "none", view], ["POST", prefix + "/approve", "form", act], ["GET", prefix + "/live", "none", live]]);
}

// NOVA's sidebar panel, /app/<slug>, from the App's own Supervisor identity,
// never from the request; Home Assistant's start ("/") while it can't be read.
export function novaPanel(slug: () => Promise<string>, logger?: RelayLogger): () => Promise<string> {
  let known: string | undefined;
  return async () => {
    if (known !== undefined) return known;
    try {
      const s = await slug();
      if (!SLUG.test(s)) throw new Error("no usable App slug");
      return (known = `/app/${s}`);
    } catch (error) {
      logger?.warn("Could not read the App's slug; the approval page links to Home Assistant's start", { error: String((error as Error).message) });
      return "/";
    }
  };
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

// The NOVA page's part, at its top: how many asks wait (a top-level link,
// so the view can't be framed), and each device whose phone asks are paused,
// with the asks that paused them, a way to ask on the phone again (resume:
// the CSRF-guarded form's opening tags), and the page's own revoke step.
// Without the worker's words: none.
export async function renderGateSection(page: GatePage, devices: DeviceRecord[], ingressPath: string, now: number, resume: (deviceId: string) => string): Promise<string> {
  const root = `${ingressPath.replace(/\/$/, "")}/`;
  const asks = page.waiting().sort((a, b) => a.expiresAt - b.expiresAt);
  const waiting = asks.length;
  const paused = page.pausedDevices().flatMap((p) => devices.filter((d) => d.deviceId === p.deviceId).map((d) => ({ ...p, name: d.name })));
  const t = waiting === 0 && paused.length === 0 ? null : await page.texts("");
  if (t === null) return "";
  // The soonest ask's first item and its computer, so the banner says what waits.
  const first = (ask: PageAsk): string => {
    const text = ask.items[0]?.text ?? "";
    return text === "" ? "" : `<p>${escapeHtml(text)}${ask.from === "" ? "" : ` <span class="muted meta">· ${escapeHtml(ask.from)}</span>`}</p>`;
  };
  const open = waiting === 0 ? "" : `<section class="alert" id="asks"><h2>${t.say("page.heading")} <span class="count">${waiting}</span></h2><p class="lead">${waiting === 1 ? t.say("page.waiting_one") : t.say("page.waiting_many", { count: waiting })}</p>${first(asks[0]!)}<div class="actions"><a class="linkbtn primary" target="_top" href="${escapeHtml(`${root}approve`)}">${t.say("page.open")}</a></div></section>`;
  const pauses = paused.map((p) => {
    const misses = p.misses.map((m) => `<li><span>${clock(m.at)}${m.what ? ` · ${escapeHtml(m.what)}` : ""}</span></li>`).join("");
    const revoke = `<a class="linkbtn quiet danger" href="${escapeHtml(`${root}?confirm=revoke_device&device=${encodeURIComponent(p.deviceId)}`)}">${t.say("page.revoke_device")}</a>`;
    return `<section><h2>${t.say("page.paused_heading")}</h2><p><strong>${escapeHtml(p.name)}</strong> · ${t.say("page.paused_left", { minutes: Math.max(1, Math.ceil((p.until - now) / 60_000)) })}</p><p class="muted meta">${t.say("page.paused")}</p>${misses ? `<ul class="facts">${misses}</ul>` : ""}<div class="actions">${resume(p.deviceId)}<button type="submit">${t.say("page.resume")}</button></form>${revoke}</div></section>`;
  });
  return open + pauses.join("");
}

// Server-local hour and minute, as the device list.
function clock(ms: number): string {
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}
