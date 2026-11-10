// The approval view (gate spec §3, item 2): who may approve, the single-use
// buttons, framing, and that an approval reaches only the device that asked.
import type { IncomingMessage, ServerResponse } from "node:http";

import { describe, expect, it } from "vitest";

import { askRef, createGateAsks, type PreparedAsk } from "../../nova/src/http/gate-asks.js";
import { approveRoutes, createApproveActionHandler, createApprovePageHandler, pageChannel, renderGateSection, type ApprovePageDeps } from "../../nova/src/http/handlers/nova-approve.js";
import { createNovaPageHandler, type NovaPageDeps } from "../../nova/src/http/handlers/nova-page.js";
import type { RouteContext } from "../../nova/src/http/router.js";
import { createCsrfStore } from "../../nova/src/security/csrf.js";
import type { DeviceRecord } from "../../nova/src/security/device-registry.js";
import { PAGE_KEYS } from "../../nova/src/http/page-texts.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";
import type { Principal } from "../../nova/src/security/principal.js";

const OWNER: HaAuthUser = { id: "owner-1", name: "Markus", is_owner: true, is_active: true, system_generated: false, group_ids: ["system-admin"] };
const ADMIN: HaAuthUser = { id: "admin-1", name: "Anna", is_owner: false, is_active: true, system_generated: false, group_ids: ["system-admin"] };
const USER: HaAuthUser = { id: "user-1", name: "Kim", is_owner: false, is_active: true, system_generated: false, group_ids: ["system-users"] };
const RELAY: HaAuthUser = { id: "relay-1", name: "HA NOVA", is_owner: false, is_active: true, system_generated: false, group_ids: ["system-admin"] };
const laptop: Principal = { kind: "device", deviceId: "laptop" };
// The worker's words (cli/gate_texts.go) as markers; the name is the worker's (sayText).
const TEXTS: Record<string, string> = { ...Object.fromEntries(PAGE_KEYS.map((key) => [key, `‹${key}›`])), "page.ends_in": "‹page.ends_in› {minutes}", "page.more": "‹page.more› {count}", "page.waiting_many": "{count} ‹page.waiting_many›" };
const words = (lang = "en", over: Record<string, unknown> = {}) => async (name: string): Promise<unknown> => ({ lang, texts: { ...TEXTS, "page.signed_in": `‹signed in› “${name}” <b>`, ...over } });
const typed: PreparedAsk = { verdict: "typed", lang: "en", title: "Unlock?", body: "Requested by <Mac> through HA NOVA.", button: "Unlock", items: [{ verdict: "typed", text: "Unlock <Front>", detail: "lock", entity_ids: ["lock.front"] }] };

function bed(users: HaAuthUser[] = [OWNER, ADMIN, USER, RELAY], relay: () => Promise<string> = async () => RELAY.id, texts = words()) {
  let now = 1_000;
  const asks = createGateAsks({
    ask: async () => typed,
    prepare: {},
    deviceName: () => "Markus' <MacBook>",
    channels: () => ["page"],
    link: () => undefined,
    phone: async () => null,
    now: () => now,
    pollMs: 5,
  });
  const deps: ApprovePageDeps = { fetchAuthUsers: async () => users, relayUserId: relay, csrf: createCsrfStore(), page: { ...asks, texts }, now: () => now };
  const ctx = (principal: Principal, body: unknown): RouteContext => {
    const request = { headers: {}, once: () => request, off: () => request } as unknown as IncomingMessage;
    const response = { destroyed: false, once: () => response, off: () => response } as unknown as ServerResponse;
    return { request, response, path: "/gate/v1/ask", body, principal };
  };
  const open = async () => (await asks.handler(ctx(laptop, { requests: [{ endpoint: "core", body_b64: Buffer.from("{}").toString("base64") }] }))) as { ask_id: string };
  const wait = async (askId: string) => (await asks.handler(ctx(laptop, { ask_id: askId, op: "wait" }))) as { state: string };
  return { asks, deps, open, wait, tick: (ms: number) => (now += ms) };
}

async function call(handler: ReturnType<typeof createApprovePageHandler>, user: string, body: unknown = null, extra: Record<string, string> = {}, peer = "172.30.32.2", url = "/approve") {
  const headers: Record<string, string> = { "x-ingress-path": "/api/hassio_ingress/tok", "x-remote-user-id": user, ...extra };
  const request = { url, socket: { remoteAddress: peer }, headers, rawHeaders: Object.entries(headers).flat() } as unknown as IncomingMessage;
  const set: Record<string, string> = {};
  const r = { statusCode: 0, body: "", headers: set, setHeader: (k: string, v: string) => void (set[k.toLowerCase()] = v), end(b?: string) { if (b) this.body = b; } };
  await handler({ request, response: r as unknown as ServerResponse, path: url, body });
  return r;
}

const form = (html: string, action: string) => {
  const at = html.indexOf('name="action" value="' + action + '"');
  const ref = /name="ref" value="([^"]+)"/.exec(html.slice(at))![1]!;
  const csrf = /name="csrf" value="([^"]+)"/.exec(html.slice(at))![1]!;
  return { action, ref, csrf };
};

describe("approval view", () => {
  it("lists the waiting ask by ref, escaped, and can't be framed", async () => {
    const t = bed();
    const { ask_id } = await t.open();
    const page = await call(createApprovePageHandler(t.deps), ADMIN.id);
    expect(page.statusCode).toBe(200);
    expect(page.headers["content-security-policy"]).toContain("frame-ancestors 'none'");
    expect(page.headers["x-frame-options"]).toBe("DENY");
    expect(page.body).toContain("Requested by &lt;Mac&gt; through HA NOVA.");
    expect(page.body).toContain("Unlock &lt;Front&gt;");
    expect(page.body).toContain("<code>lock.front</code>");
    expect(page.body).not.toContain(ask_id);
    expect(page.body).toContain(askRef(ask_id));
  });

  it("words the page only with the worker's texts, escaped, numbers filled in", async () => {
    const t = bed();
    await t.open();
    const page = await call(createApprovePageHandler(t.deps), ADMIN.id, null, {}, "172.30.32.2", "/approve?done=approved");
    for (const key of ["page.title", "page.heading", "page.done_approved", "page.approve", "window.cancel", "page.back"]) {
      expect(page.body).toContain(`‹${key}›`);
    }
    expect(page.body).toContain('<html lang="en">');
    expect(page.body).toContain("‹page.ends_in› 3");
    expect(page.body).toContain("‹signed in› “Anna” &lt;b&gt;");
    expect(page.body).not.toMatch(/Approve<|Cancel<|Waiting for your OK|Back to Home Assistant/);
    const refused = await call(createApprovePageHandler(t.deps), USER.id);
    expect([refused.statusCode, refused.body.includes("‹page.approver_not_admin›")]).toEqual([403, true]);
  });

  it("speaks Home Assistant's language", async () => {
    const t = bed(undefined, undefined, words("de", { "page.heading": "Wartet auf dein OK", "page.approve": "Genehmigen", "window.cancel": "Abbrechen", "page.ends_in": "Endet in {minutes} Min." }));
    await t.open();
    const page = await call(createApprovePageHandler(t.deps), ADMIN.id);
    expect(page.body).toContain('<html lang="de">');
    for (const text of ["<h1>Wartet auf dein OK</h1>", ">Genehmigen</button>", ">Abbrechen</button>", "Endet in 3 Min."]) {
      expect(page.body).toContain(text);
    }
  });

  it("without the worker's texts says nothing: 503, no English instead", async () => {
    const t = bed();
    const { ask_id } = await t.open();
    const button = form((await call(createApprovePageHandler(t.deps), ADMIN.id)).body, "approve");
    const missing = { ...TEXTS };
    delete missing["page.back"];
    const broken = [
      async () => { throw new Error("worker down"); },
      async () => null,
      async () => ({ lang: "en", texts: missing }),
      async () => ({ lang: "en", texts: { ...TEXTS, "page.approve": 1 } }),
      words('en"><script>'),
    ];
    for (const texts of broken) {
      const deps = { ...t.deps, page: { ...t.deps.page, texts } };
      for (const [handler, user, body] of [[createApprovePageHandler(deps), ADMIN.id, null], [createApproveActionHandler(deps), ADMIN.id, button], [createApprovePageHandler(deps), USER.id, null]] as const) {
        const r = await call(handler, user, body);
        expect([r.statusCode, r.body]).toEqual([503, ""]);
      }
    }
    expect((await t.wait(ask_id)).state).toBe("waiting");
  });

  it("an approval grants the device's ask once; its button works once", async () => {
    const t = bed();
    const { ask_id } = await t.open();
    const page = await call(createApprovePageHandler(t.deps), ADMIN.id);
    const approve = form(page.body, "approve");
    const done = await call(createApproveActionHandler(t.deps), ADMIN.id, approve);
    expect(done.statusCode).toBe(303);
    expect(done.headers.location).toBe("/api/hassio_ingress/tok/approve?done=approved");
    expect((await t.wait(ask_id)).state).toBe("approved");
    expect((await call(createApproveActionHandler(t.deps), ADMIN.id, approve)).statusCode).toBe(403);
  });

  it("a button is bound to its person, its ask, and its action", async () => {
    const t = bed();
    const { ask_id } = await t.open();
    const page = await call(createApprovePageHandler(t.deps), ADMIN.id);
    const approve = form(page.body, "approve");
    expect((await call(createApproveActionHandler(t.deps), OWNER.id, approve)).statusCode).toBe(403);
    expect((await call(createApproveActionHandler(t.deps), ADMIN.id, { ...approve, action: "cancel" })).statusCode).toBe(403);
    expect((await call(createApproveActionHandler(t.deps), ADMIN.id, { ...approve, ref: "x".repeat(64) })).statusCode).toBe(403);
    expect((await call(createApproveActionHandler(t.deps), ADMIN.id, approve, { "sec-fetch-site": "cross-site" })).statusCode).toBe(403);
    expect((await t.wait(ask_id)).state).toBe("waiting");
    const cancel = form((await call(createApprovePageHandler(t.deps), ADMIN.id)).body, "cancel");
    expect((await call(createApproveActionHandler(t.deps), ADMIN.id, cancel)).headers.location).toBe("/api/hassio_ingress/tok/approve?done=cancelled");
    expect((await t.wait(ask_id)).state).toBe("cancelled");
  });

  it("only an active, non-system administrator other than the Relay's user approves; a failed read approves nothing", async () => {
    const t = bed();
    const { ask_id } = await t.open();
    const button = form((await call(createApprovePageHandler(t.deps), OWNER.id)).body, "approve");
    const cases: [ApprovePageDeps, string, number, string?][] = [
      [t.deps, USER.id, 403],
      [t.deps, RELAY.id, 403],
      [{ ...t.deps, fetchAuthUsers: async () => [{ ...OWNER, is_active: false }] }, OWNER.id, 403],
      [{ ...t.deps, fetchAuthUsers: async () => [{ ...OWNER, system_generated: true }] }, OWNER.id, 403],
      [{ ...t.deps, fetchAuthUsers: async () => [OWNER, ADMIN] }, "someone-else", 403],
      [{ ...t.deps, fetchAuthUsers: async () => { throw new Error("down"); } }, OWNER.id, 503],
      [{ ...t.deps, relayUserId: async () => { throw new Error("down"); } }, OWNER.id, 503],
      [{ ...t.deps, relayUserId: async () => OWNER.id }, OWNER.id, 403],
      [t.deps, OWNER.id, 403, "192.168.1.20"],
    ];
    for (const [deps, user, status, peer] of cases) {
      expect((await call(createApprovePageHandler(deps), user, null, {}, peer)).statusCode).toBe(status);
      expect((await call(createApproveActionHandler(deps), user, button, {}, peer)).statusCode).toBe(status);
    }
    expect((await t.wait(ask_id)).state).toBe("waiting");
    expect((await call(createApproveActionHandler(t.deps), OWNER.id, button)).statusCode).toBe(303);
    expect((await t.wait(ask_id)).state).toBe("approved");
  });

  it("an ask that ended meanwhile changes nothing", async () => {
    const t = bed();
    await t.open();
    const button = form((await call(createApprovePageHandler(t.deps), ADMIN.id)).body, "approve");
    t.tick(181_000);
    expect((await call(createApproveActionHandler(t.deps), ADMIN.id, button)).headers.location).toBe("/api/hassio_ingress/tok/approve?done=gone");
  });
});

describe("owner page: waiting asks and paused devices", () => {
  const device = { deviceId: "laptop", name: "Laptop <1>" } as DeviceRecord;

  it("links the view top-level and lists a paused device with its revoke step", async () => {
    const t = bed();
    for (let i = 0; i < 3; i++) {
      const { ask_id } = await t.open();
      t.asks.cancel(askRef(ask_id));
    }
    expect(t.asks.pausedDevices()).toEqual(["laptop"]);
    await t.open();
    const html = await renderGateSection(t.deps.page, [device], "/api/hassio_ingress/tok");
    expect(html).toContain('target="_top" href="/api/hassio_ingress/tok/approve"');
    expect(html).toContain("Laptop &lt;1&gt;");
    expect(html).toContain('href="/api/hassio_ingress/tok/?confirm=revoke_device&amp;device=laptop"');
    for (const key of ["page.heading", "page.waiting_one", "page.open", "page.paused", "page.revoke_device"]) {
      expect(html).toContain(`‹${key}›`);
    }
    const two = { ...t.deps.page, waiting: () => [...t.asks.waiting(), ...t.asks.waiting()], pausedDevices: () => [] };
    expect(await renderGateSection(two, [device], "/x")).toContain("2 ‹page.waiting_many›");
    expect(await renderGateSection(bed().deps.page, [device], "/x")).toBe("");
    expect(await renderGateSection({ ...t.deps.page, texts: async () => { throw new Error("down"); } }, [device], "/x")).toBe("");
  });

  it("points an administrator who isn't the owner to the view", async () => {
    const t = bed();
    const deps = { fetchAuthUsers: async () => [OWNER, ADMIN], gatePage: t.deps.page } as unknown as NovaPageDeps;
    const r = await call(createNovaPageHandler(deps), ADMIN.id, null, {}, "172.30.32.2", "/");
    expect(r.statusCode).toBe(403);
    expect(r.body).toContain('<a target="_top" href="/api/hassio_ingress/tok/approve">‹page.link›</a>');
    const silent = { ...deps, gatePage: { ...t.deps.page, texts: async () => null } } as unknown as NovaPageDeps;
    expect((await call(createNovaPageHandler(silent), ADMIN.id, null, {}, "172.30.32.2", "/")).body).not.toContain("approve");
  });
});

describe("the page as a channel, and what it shows", () => {
  const ws = (users: unknown, relay: unknown) => ({ sendMessage: async ({ type }: { type: string }) => (type === "config/auth/list" ? users : { id: relay }) });

  it("is a channel only while an administrator other than the Relay's user exists; a failed read is no channel", async () => {
    expect(await pageChannel(ws([OWNER, RELAY], RELAY.id))).toEqual(["page"]);
    expect(await pageChannel(ws([OWNER], OWNER.id))).toEqual([]);
    expect(await pageChannel(ws([USER, RELAY, { ...ADMIN, is_active: false }], RELAY.id))).toEqual([]);
    expect(await pageChannel(ws([OWNER], ""))).toEqual([]);
    expect(await pageChannel(ws(null, RELAY.id))).toEqual([]);
    expect(await pageChannel({ sendMessage: async () => { throw new Error("down"); } })).toEqual([]);
  });

  it("serves the view and its buttons under / and /home", () => {
    const t = bed();
    const routes = approveRoutes({ fetchAuthUsers: t.deps.fetchAuthUsers, wsClient: ws([], RELAY.id), page: t.deps.page, now: t.deps.now });
    expect(routes.map(([method, path, body]) => [method, path, body])).toEqual([["GET", "/approve", "none"], ["POST", "/approve", "form"], ["GET", "/home/approve", "none"], ["POST", "/home/approve", "form"]]);
  });

  it("never shows an ask it can't word, and says when more wait", async () => {
    const page = { waiting: () => [{ ref: "r1", body: "Requested by Mac.", items: [{ verdict: "typed" as const }], expiresAt: 9_000 }], pausedDevices: () => [], approve: () => false, cancel: () => false, texts: words() };
    const t = bed();
    const blank = await call(createApprovePageHandler({ ...t.deps, page }), ADMIN.id);
    expect(blank.body).not.toContain('name="ref"');
    expect(blank.body).toContain("‹page.empty›");
    const many = Array.from({ length: 5 }, (_, i) => ({ ref: "r" + i, body: "Requested by Mac " + i + ".", items: [{ verdict: "typed" as const, text: "Unlock" }], expiresAt: 9_000 + i }));
    const shown = await call(createApprovePageHandler({ ...t.deps, page: { ...page, waiting: () => many } }), ADMIN.id);
    expect(shown.body.match(/name="action" value="approve"/g)).toHaveLength(3);
    expect(shown.body).toContain("‹page.more› 2");
  });
});
