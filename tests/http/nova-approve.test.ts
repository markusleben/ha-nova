// The approval view (gate spec §3, item 2): who may approve, the single-use
// buttons, framing, and that an approval reaches only the device that asked.
import { readFileSync } from "node:fs";
import type { IncomingMessage, ServerResponse } from "node:http";

import { describe, expect, it } from "vitest";

import { askRef, createGateAsks, type PreparedAsk } from "../../nova/src/http/gate-asks.js";
import { approveRoutes, createApproveActionHandler, createApprovePageHandler, novaPanel, pageChannel, renderGateSection, type ApprovePageDeps } from "../../nova/src/http/handlers/nova-approve.js";
import { createNovaActionHandler, createNovaPageHandler, type NovaPageDeps } from "../../nova/src/http/handlers/nova-page.js";
import { createLiveHandler, LIVE_CSP, LIVE_SCRIPT, liveState } from "../../nova/src/http/handlers/nova-page-live.js";
import { createHash } from "node:crypto";
import type { RouteContext } from "../../nova/src/http/router.js";
import { createCsrfStore } from "../../nova/src/security/csrf.js";
import type { DeviceRecord } from "../../nova/src/security/device-registry.js";
import { PAGE_KEYS, pageTexts } from "../../nova/src/http/page-texts.js";
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
const typed: PreparedAsk = { verdict: "typed", lang: "en", title: "Unlock?", body: "Requested by <Mac> through HA NOVA.", from: "Requested by “Markus' <MacBook>” through HA NOVA.", button: "Unlock", items: [{ verdict: "typed", text: "Unlock <Front>", detail: "lock", entity_ids: ["lock.front"] }] };

function bed(users: HaAuthUser[] = [OWNER, ADMIN, USER, RELAY], relay: () => Promise<string> = async () => RELAY.id, load = words()) {
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
  const deps: ApprovePageDeps = { fetchAuthUsers: async () => users, relayUserId: relay, csrf: createCsrfStore(), page: { ...asks, texts: pageTexts(load, () => now) }, panel: async () => "/app/2368fcfa_ha_nova_relay", now: () => now };
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
    expect(page.body).not.toContain("Requested by &lt;Mac&gt; through HA NOVA."); // the phone's sentence repeats the item and the computer
    expect(page.body.match(/Unlock &lt;Front&gt;/g)).toHaveLength(1);
    expect(page.body).toContain("<code>lock.front</code>"); // an id the text doesn't name
    expect(page.body).toContain('<span class="muted meta">Requested by “Markus&#39; &lt;MacBook&gt;” through HA NOVA.</span>'); // the worker's words, the name through sayText
    expect(page.body).toContain('data-left="180"');
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
      async () => ({ lang: "en", texts: { ...TEXTS, "page.open": "" } }),
      words('en"><script>'),
    ];
    const expired = { ...button, csrf: "x" };
    for (const load of broken) {
      const deps = { ...t.deps, page: { ...t.deps.page, texts: pageTexts(load, () => 0) } };
      for (const [handler, user, body] of [[createApprovePageHandler(deps), ADMIN.id, null], [createApproveActionHandler(deps), ADMIN.id, expired], [createApprovePageHandler(deps), USER.id, null], [createApproveActionHandler(deps), USER.id, button]] as const) {
        const r = await call(handler, user, body);
        expect([r.statusCode, r.body]).toEqual([503, ""]);
      }
    }
    expect((await t.wait(ask_id)).state).toBe("waiting");
  });

  it("a button's approval waits for no words: none are read between the check and the grant", async () => {
    for (const texts of [async () => { throw new Error("worker down"); }, () => new Promise<never>(() => {})]) {
      const t = bed();
      const { ask_id } = await t.open();
      const approve = form((await call(createApprovePageHandler(t.deps), ADMIN.id)).body, "approve");
      let read = 0;
      const deps = { ...t.deps, page: { ...t.deps.page, texts: () => { read++; return texts(); } } };
      const done = await call(createApproveActionHandler(deps), ADMIN.id, approve);
      expect([done.statusCode, done.headers.location, read]).toEqual([303, "/api/hassio_ingress/tok/approve?done=approved", 0]);
      expect((await t.wait(ask_id)).state).toBe("approved");
    }
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

  it("leads back to NOVA's panel; on its own once nothing is left to answer", async () => {
    const t = bed();
    await t.open();
    const refresh = '<meta http-equiv="refresh" content="4;url=/app/2368fcfa_ha_nova_relay">';
    const one = await call(createApprovePageHandler(t.deps), ADMIN.id, null, {}, "172.30.32.2", "/approve?done=approved"); // another still waits
    expect(one.body).toContain('<a href="/app/2368fcfa_ha_nova_relay">‹page.back›</a>');
    expect([one.body.includes("‹page.done_approved›"), one.body.includes(refresh), one.body.includes("‹page.returning›")]).toEqual([true, false, false]);
    await call(createApproveActionHandler(t.deps), ADMIN.id, form(one.body, "cancel"));
    const none = await call(createApprovePageHandler(t.deps), ADMIN.id, null, {}, "172.30.32.2", "/approve?done=cancelled");
    expect([none.body.includes("‹page.done_cancelled›"), none.body.includes(refresh), none.body.includes("‹page.returning›")]).toEqual([true, true, true]);
    expect((await call(createApprovePageHandler(t.deps), ADMIN.id)).body).toContain(refresh); // ended elsewhere or expired: nothing left
    const refused = await call(createApprovePageHandler(t.deps), USER.id);
    expect([refused.body.includes('<a href="/app/2368fcfa_ha_nova_relay">'), refused.body.includes(refresh)]).toEqual([true, false]);
    const expired = await call(createApproveActionHandler(t.deps), ADMIN.id, { action: "approve", ref: "r", csrf: "x" });
    expect([expired.statusCode, expired.body.includes('<a href="/app/2368fcfa_ha_nova_relay">'), expired.body.includes(refresh)]).toEqual([403, true, false]);
  });

  it("finds NOVA's panel from the App's own slug only, and Home Assistant's start while it can't", async () => {
    let reads = 0;
    const warnings: unknown[] = [];
    const logger = { info: () => {}, warn: (...a: unknown[]) => void warnings.push(a), error: () => {} };
    const panel = novaPanel(async () => (reads++, "2368fcfa_ha_nova_relay"), logger);
    expect([await panel(), await panel(), reads]).toEqual(["/app/2368fcfa_ha_nova_relay", "/app/2368fcfa_ha_nova_relay", 1]);
    for (const slug of ["", "../x", "//evil.example", "a b", "A_B", "x?y"]) {
      expect(await novaPanel(async () => slug, logger)()).toBe("/");
    }
    let fail = true;
    const flaky = novaPanel(async () => { if (fail) throw new Error("down"); return "abc_1"; }, logger);
    expect(await flaky()).toBe("/");
    fail = false;
    expect(await flaky()).toBe("/app/abc_1");
    expect(warnings).toHaveLength(7);
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

  it("links the view top-level, says what waits, and shows a paused device with its five asks, a way to ask again, and revoke", async () => {
    const t = bed();
    for (let i = 0; i < 5; i++) {
      await t.open();
      t.tick(181_000);
      t.asks.waiting(); // expires it
    }
    expect(t.asks.pausedDevices()).toMatchObject([{ deviceId: "laptop", until: 1_000 + 5 * 181_000 + 300_000 }]);
    expect(t.asks.pausedDevices()[0]!.misses).toHaveLength(5);
    await t.open();
    const html = await renderGateSection(t.deps.page, [device], "/api/hassio_ingress/tok", t.deps.now(), (id) => `<form data-device="${id}">`);
    expect(html).toContain('target="_top" href="/api/hassio_ingress/tok/approve"');
    expect(html).toContain("Unlock &lt;Front&gt; <span class=\"muted meta\">· Requested by “Markus&#39; &lt;MacBook&gt;” through HA NOVA.</span>");
    expect(html).toContain("Laptop &lt;1&gt;");
    expect(html.match(/· Unlock &lt;Front&gt;<\/span><\/li>/g)).toHaveLength(5);
    expect(html).toContain('<form data-device="laptop"><button type="submit">‹page.resume›</button></form>');
    expect(html).toContain('href="/api/hassio_ingress/tok/?confirm=revoke_device&amp;device=laptop"');
    for (const key of ["page.heading", "page.waiting_one", "page.open", "page.paused_heading", "page.paused", "page.revoke_device"]) {
      expect(html).toContain(`‹${key}›`);
    }
    const two = { ...t.deps.page, waiting: () => [...t.asks.waiting(), ...t.asks.waiting()], pausedDevices: () => [] };
    expect(await renderGateSection(two, [device], "/x", 0, () => "")).toContain("2 ‹page.waiting_many›");
    expect(await renderGateSection(bed().deps.page, [device], "/x", 0, () => "")).toBe("");
    expect(await renderGateSection({ ...t.deps.page, texts: async () => null }, [device], "/x", 0, () => "")).toBe("");
  });

  it("asks on the phone again only from an administrator's page, with a token bound to that device, and logs who", async () => {
    const t = bed();
    for (let i = 0; i < 5; i++) {
      await t.open();
      t.tick(181_000);
      t.asks.waiting();
    }
    const logged: unknown[] = [];
    const csrf = createCsrfStore();
    const deps = {
      fetchAuthUsers: async () => [OWNER, ADMIN, USER], gatePage: t.deps.page, csrf, now: t.deps.now,
      registry: { list: () => [device] }, pairing: { getStatus: () => ({ phase: "idle" }) }, connection: () => ({ haConnected: true }),
      update: async () => ({ version: "0.7.0", versionLatest: "0.7.0", updateAvailable: false, error: false }), relayVersion: "0.7.0",
      logger: { info: (...args: unknown[]) => void logged.push(args), warn: () => undefined, error: () => undefined },
    } as unknown as NovaPageDeps;
    const page = await call(createNovaPageHandler(deps), ADMIN.id, null, {}, "172.30.32.2", "/");
    const token = /name="csrf" value="([^"]+)"><input type="hidden" name="action" value="resume_phone_asks"><input type="hidden" name="device_id" value="laptop">/.exec(page.body)?.[1];
    expect(token).toBeDefined();
    const act = (user: string, body: Record<string, string>) => call(createNovaActionHandler(deps), user, body, {}, "172.30.32.2", "/action");
    expect((await act(ADMIN.id, { action: "resume_phone_asks", csrf: token!, device_id: "other" })).statusCode).toBe(403); // bound to the device
    expect((await act(USER.id, { action: "resume_phone_asks", csrf: csrf.issue(USER.id, "resume_phone_asks:laptop", t.deps.now()), device_id: "laptop" })).statusCode).toBe(403);
    expect(t.asks.pausedDevices()).toHaveLength(1);
    const fresh = /name="csrf" value="([^"]+)"><input type="hidden" name="action" value="resume_phone_asks">/.exec((await call(createNovaPageHandler(deps), ADMIN.id, null, {}, "172.30.32.2", "/")).body)![1]!;
    expect((await act(ADMIN.id, { action: "resume_phone_asks", csrf: fresh, device_id: "laptop" })).statusCode).toBe(303);
    expect(t.asks.pausedDevices()).toEqual([]);
    expect(logged).toContainEqual(["NOVA page action", { action: "resume_phone_asks", user_id: ADMIN.id, device_id: "laptop" }]);
    expect((await act(ADMIN.id, { action: "resume_phone_asks", csrf: fresh, device_id: "laptop" })).statusCode).toBe(403); // once
    const noGate = { ...deps, gatePage: undefined } as unknown as NovaPageDeps;
    expect((await call(createNovaActionHandler(noGate), ADMIN.id, { action: "resume_phone_asks", csrf: csrf.issue(ADMIN.id, "resume_phone_asks:laptop", t.deps.now()), device_id: "laptop" }, {}, "172.30.32.2", "/action")).statusCode).toBe(400);
  });

  it("updates on its own: an administrator's poll reads a digest that changes with the asks; the only script is pinned by its hash", async () => {
    const t = bed();
    const live = createLiveHandler({ fetchAuthUsers: t.deps.fetchAuthUsers, page: t.deps.page });
    const state = async (user: string) => {
      const r = await call(live, user, null, {}, "172.30.32.2", "/live");
      return [r.statusCode, r.body] as const;
    };
    const [code, quiet] = await state(ADMIN.id);
    expect(code).toBe(200);
    await t.open();
    const [, busy] = await state(ADMIN.id);
    expect(JSON.parse(busy).state).not.toBe(JSON.parse(quiet).state);
    expect(await state(USER.id)).toEqual([403, "{}"]);
    const page = await call(createApprovePageHandler(t.deps), ADMIN.id);
    expect(page.body).toContain(`data-state="${JSON.parse(busy).state}"`);
    expect(page.body.match(/<script\b/gi)).toHaveLength(1);
    expect(page.body).toContain(`<script>${LIVE_SCRIPT}</script>`);
    expect(page.headers["content-security-policy"]).toContain(`script-src 'sha256-${createHash("sha256").update(LIVE_SCRIPT).digest("base64")}'`);
    expect(LIVE_CSP).not.toContain("unsafe");
  });

  it("embeds the digest of the asks it shows, so an ask that arrives while the page renders still reloads it", async () => {
    const t = bed();
    let release = (): void => {};
    const deps = {
      fetchAuthUsers: async () => [OWNER, ADMIN], gatePage: t.deps.page, csrf: createCsrfStore(), now: () => 1000,
      registry: { list: () => [] }, pairing: { getStatus: () => ({ phase: "idle" }) }, connection: () => ({ haConnected: true }),
      update: () => new Promise((resolve) => { release = () => resolve({ version: "0.7.0", versionLatest: "0.7.0", updateAvailable: false, error: false }); }), relayVersion: "0.7.0",
    } as unknown as NovaPageDeps;
    const quiet = liveState(t.deps.page);
    const rendering = call(createNovaPageHandler(deps), ADMIN.id, null, {}, "172.30.32.2", "/");
    await new Promise((resolve) => setTimeout(resolve, 0));
    await t.open(); // arrives after the page read the asks
    release();
    const r = await rendering;
    expect(r.body).not.toContain("‹page.waiting_one›");
    expect(r.body).toContain(`data-state="${quiet}"`);
    expect(liveState(t.deps.page)).not.toBe(quiet); // the next poll reloads it
  });

  it("shows an administrator who isn't the owner the waiting asks on the page itself", async () => {
    const t = bed();
    await t.open();
    const deps = {
      fetchAuthUsers: async () => [OWNER, ADMIN], gatePage: t.deps.page, csrf: createCsrfStore(), now: () => 1000,
      registry: { list: () => [] }, pairing: { getStatus: () => ({ phase: "idle" }) }, connection: () => ({ haConnected: true }),
      update: async () => ({ version: "0.7.0", versionLatest: "0.7.0", updateAvailable: false, error: false }), relayVersion: "0.7.0",
    } as unknown as NovaPageDeps;
    const r = await call(createNovaPageHandler(deps), ADMIN.id, null, {}, "172.30.32.2", "/");
    expect(r.statusCode).toBe(200);
    expect(r.body).toContain("‹page.heading›");
    expect(r.body).toContain('target="_top" href="/api/hassio_ingress/tok/approve"');
  });
});

describe("the page's words", () => {
  it("are exactly the catalog's page texts, plus the shared Cancel", () => {
    const catalog = JSON.parse(readFileSync(new URL("../../cli/confirm_strings.en.json", import.meta.url), "utf8")) as Record<string, unknown>;
    const keys = [...Object.keys(catalog).filter((key) => key.startsWith("page.")), "window.cancel"].sort();
    expect([...PAGE_KEYS].sort()).toEqual(keys);
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
    const routes = approveRoutes({ fetchAuthUsers: t.deps.fetchAuthUsers, wsClient: ws([], RELAY.id), page: t.deps.page, slug: async () => "x", now: t.deps.now });
    expect(routes.map(([method, path, body]) => [method, path, body])).toEqual([["GET", "/approve", "none"], ["POST", "/approve", "form"], ["GET", "/live", "none"], ["GET", "/home/approve", "none"], ["POST", "/home/approve", "form"], ["GET", "/home/live", "none"]]);
  });

  it("never shows an ask it can't word, and says when more wait", async () => {
    const page = { waiting: () => [{ ref: "r1", body: "Requested by Mac.", from: "Requested by Mac.", items: [{ verdict: "typed" as const }], expiresAt: 9_000 }], pausedDevices: () => [], approve: () => false, cancel: () => false, resume: () => false, texts: pageTexts(words(), () => 0) };
    const t = bed();
    const blank = await call(createApprovePageHandler({ ...t.deps, page }), ADMIN.id);
    expect(blank.body).not.toContain('name="ref"');
    expect(blank.body).toContain("‹page.empty›");
    const many = Array.from({ length: 5 }, (_, i) => ({ ref: "r" + i, body: "Requested by Mac " + i + ".", from: "Requested by Mac.", items: [{ verdict: "typed" as const, text: "Unlock" }], expiresAt: 9_000 + i }));
    const shown = await call(createApprovePageHandler({ ...t.deps, page: { ...page, waiting: () => many } }), ADMIN.id);
    expect(shown.body.match(/name="action" value="approve"/g)).toHaveLength(3);
    expect(shown.body).toContain("‹page.more› 2");
  });
});
