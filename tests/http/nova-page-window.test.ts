// The NOVA page's window lines (spec §3): every computer row says whether it
// confirms in its own window or why not; only the owner gets the buttons,
// each a POST whose token is bound to that computer, logged with who.
import type { IncomingMessage, ServerResponse } from "node:http";

import { describe, expect, it, vi } from "vitest";

import { createNovaActionHandler, createNovaPageHandler, PAGE_CSRF_PENDING, type NovaPageDeps } from "../../nova/src/http/handlers/nova-page.js";
import { recentWindowSection, windowRow, type WindowAction } from "../../nova/src/http/handlers/nova-page-window.js";
import { PAGE_KEYS, pageTexts, type PageTexts } from "../../nova/src/http/page-texts.js";
import { createCsrfStore } from "../../nova/src/security/csrf.js";
import type { DeviceRecord } from "../../nova/src/security/device-registry-types.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";

const OWNER: HaAuthUser = { id: "owner-1", name: "Markus", is_owner: true, is_active: true, system_generated: false, group_ids: ["system-admin"] };
const ADMIN: HaAuthUser = { id: "admin-1", name: "Anna", is_owner: false, is_active: true, system_generated: false, group_ids: ["system-admin"] };
const TEXTS = Object.fromEntries(PAGE_KEYS.map((key) => [key, `‹${key}›`]));
const words: PageTexts = { lang: "en", say: (key) => TEXTS[key]! };
const base: DeviceRecord = { deviceId: "mac", secretDigest: "x", state: "active", clientInstallId: "install", name: "Mac <1>", platform: "darwin", client: "cli", createdAtMs: 0 };
const on: DeviceRecord = { ...base, credentialStore: "keychain", legacyCleanup: "complete", ownComputer: { userId: OWNER.id, atMs: 1 }, windowConfirm: true };
const button = (action: WindowAction, id: string, label: string) => `[${action}:${id}:${label}]`;
const ownerView = { isOwner: true, userId: OWNER.id };
const row = (d: DeviceRecord, viewer = ownerView, users = [OWNER, ADMIN]) => windowRow(d, users, viewer, words, button);

describe("a computer's window line", () => {
  it.each([
    ["on", on, "‹page.window_on›", ["window_off"]],
    ["off", { ...on, windowConfirm: false }, "‹page.window_off›", ["window_on"]],
    ["undeclared", { ...on, ownComputer: undefined, windowConfirm: undefined }, "‹page.window_off›", ["window_mine"]],
    ["declared by someone who is no longer the owner", { ...on, ownComputer: { userId: ADMIN.id, atMs: 1 } }, "‹page.window_off›", ["window_mine"]],
    ["a file key", { ...on, credentialStore: "file" }, "‹page.window_file›", ["window_not_mine"]],
    ["paired before 0.32", { ...base }, "‹page.window_pair_again›", []],
    ["a Mac whose old sign-in isn't cleaned up", { ...on, legacyCleanup: "pending" }, "‹page.window_cleanup›", ["window_not_mine"]],
    ["a Mac without a cleanup report", { ...on, legacyCleanup: undefined }, "‹page.window_cleanup›", ["window_not_mine"]],
    ["signed in to Home Assistant as someone else", { ...on, cloudUserId: ADMIN.id }, "‹page.window_owner›", ["window_not_mine"]],
    ["someone else's Link, turned off", { ...on, cloudUserId: ADMIN.id, windowConfirm: false }, "‹page.window_owner›", ["window_not_mine"]],
    ["someone else's Link, undeclared", { ...on, cloudUserId: ADMIN.id, ownComputer: undefined }, "‹page.window_owner›", []],
  ] as const)("%s: says so; the owner gets only the buttons that apply", (_, device, line, actions) => {
    const html = row(device as DeviceRecord);
    expect(html).toContain(`<span class="muted meta">${line}</span>`);
    expect([...html.matchAll(/\[(window_\w+):mac:/g)].map((m) => m[1])).toEqual(actions);
    expect(html.includes("‹page.window_hint› ‹page.window_warning›")).toBe(["‹page.window_on›", "‹page.window_off›"].includes(line));
    expect(html.match(/\[window_\w+:mac:([^\]]*)\]/)?.[1] ?? "").toBe(actions.length === 0 ? "" : ["window_off", "window_not_mine"].includes(actions[0]!) ? "‹page.window_turn_off›" : "‹page.window_turn_on›"); // one on/off button
    if (html.includes("‹page.window_warning›")) expect(html.indexOf("‹page.window_warning›")).toBeGreaterThan(html.lastIndexOf("[window_")); // under the switch
  });

  it("shows an administrator the state only: no buttons, no switch", () => {
    const { ownComputer: _, ...undeclared } = on;
    for (const device of [on, { ...on, windowConfirm: false }, undeclared]) {
      const html = row(device, { isOwner: false, userId: ADMIN.id });
      expect(html).toMatch(/^<span class="muted meta">‹page\.window_\w+›<\/span>$/);
    }
  });

  it("says nothing about the App's own entries", () => {
    expect(row({ ...on, clientInstallId: "nova-app-gate" })).toBe("");
    expect(row({ ...on, clientInstallId: "nova-app-chat-bridge" })).toBe("");
    expect(row({ ...on, state: "pending" })).toBe("");
  });
});

describe("recent window confirmations", () => {
  it("lists them newest first, escaped; none: no section", () => {
    const html = recentWindowSection([
      { atMs: Date.UTC(2026, 10, 16, 9), computer: { id: "mac", name: "Mac <1>" }, title: "Unlock <Front>?" },
      { atMs: Date.UTC(2026, 10, 16, 10), computer: { id: "mac", name: "Mac <1>" }, title: "Delete Kitchen?" },
    ], words);
    expect(html).toContain("<h2>‹page.window_recent›</h2>");
    expect(html.indexOf("Delete Kitchen?")).toBeLessThan(html.indexOf("Unlock &lt;Front&gt;?"));
    expect(html).toContain("<strong>Mac &lt;1&gt;</strong>");
    expect(recentWindowSection([], words)).toBe("");
  });
});

function harness(users: HaAuthUser[] = [OWNER, ADMIN], refusal: string | null = null, devices: DeviceRecord[] = [on, { ...on, deviceId: "other", name: "Other" }]) {
  const logged: unknown[] = [];
  const csrf = createCsrfStore(PAGE_CSRF_PENDING);
  const registry = {
    list: () => devices,
    setOwnComputer: vi.fn(() => refusal),
    setWindowConfirm: vi.fn(() => refusal),
  };
  const gatePage = { waiting: () => [], pausedDevices: () => [], approve: () => false, cancel: () => false, resume: () => false, texts: pageTexts(async () => ({ lang: "en", texts: TEXTS }), () => 0), recent: () => [] };
  const deps = {
    fetchAuthUsers: async () => users, gatePage, csrf, now: () => 1000, registry,
    pairing: { getStatus: () => ({ phase: "idle" }) }, connection: () => ({ haConnected: true }),
    update: async () => ({ version: "0.7.0", versionLatest: "0.7.0", updateAvailable: false, error: false }), relayVersion: "0.7.0",
    logger: { info: (...args: unknown[]) => void logged.push(args), warn: () => undefined, error: () => undefined },
  } as unknown as NovaPageDeps;
  const call = async (handler: ReturnType<typeof createNovaPageHandler>, user: string, url: string, body: unknown = null) => {
    const headers: Record<string, string> = { "x-ingress-path": "/api/hassio_ingress/tok", "x-remote-user-id": user };
    const request = { url, socket: { remoteAddress: "172.30.32.2" }, headers, rawHeaders: Object.entries(headers).flat() } as unknown as IncomingMessage;
    const set: Record<string, string> = {};
    const r = { statusCode: 0, body: "", headers: set, setHeader: (k: string, v: string) => void (set[k.toLowerCase()] = v), end(b?: string) { if (b) this.body = b; } };
    await handler({ request, response: r as unknown as ServerResponse, path: url, body });
    return r;
  };
  const page = (user = OWNER.id) => call(createNovaPageHandler(deps), user, "/");
  const act = (user: string, body: Record<string, string>) => call(createNovaActionHandler(deps), user, "/action", body);
  const token = (html: string, action: string, device: string) =>
    new RegExp(`name="csrf" value="([^"]+)"><input type="hidden" name="action" value="${action}"><input type="hidden" name="device_id" value="${device}">`).exec(html)?.[1];
  return { deps, registry, logged, csrf, page, act, token };
}

describe("the owner's window buttons", () => {
  it("are on the owner's page only, under each computer, with the warning", async () => {
    const h = harness();
    const mine = (await h.page()).body;
    expect(mine).toContain("‹page.window_on›");
    expect(mine).toContain("‹page.window_warning›");
    expect(h.token(mine, "window_off", "mac")).toBeDefined();
    expect(h.token(mine, "window_off", "other")).toBeDefined();
    const admin = (await h.page(ADMIN.id)).body;
    expect(admin).toContain("‹page.window_on›");
    expect(admin).not.toContain('value="window_');
  });

  it("turn the window off with a token bound to that computer, once, and log who", async () => {
    const h = harness();
    const tok = h.token((await h.page()).body, "window_off", "mac")!;
    expect((await h.act(OWNER.id, { action: "window_off", csrf: tok, device_id: "other" })).statusCode).toBe(403);
    expect((await h.act(OWNER.id, { action: "window_on", csrf: tok, device_id: "mac" })).statusCode).toBe(403);
    expect(h.registry.setWindowConfirm).not.toHaveBeenCalled();
    const fresh = h.token((await h.page()).body, "window_off", "mac")!;
    const r = await h.act(OWNER.id, { action: "window_off", csrf: fresh, device_id: "mac" });
    expect([r.statusCode, r.headers.location]).toEqual([303, "/api/hassio_ingress/tok/"]);
    expect(h.registry.setWindowConfirm).toHaveBeenCalledWith("mac", false, OWNER.id);
    expect(h.logged).toContainEqual(["NOVA page action", { action: "window_off", user_id: OWNER.id, device_id: "mac" }]);
    expect((await h.act(OWNER.id, { action: "window_off", csrf: fresh, device_id: "mac" })).statusCode).toBe(403);
  });

  it("declare the computer the owner's, or not, with the owner's own id", async () => {
    const h = harness();
    await h.act(OWNER.id, { action: "window_mine", csrf: h.csrf.issue(OWNER.id, "window_mine:mac", 1000), device_id: "mac" });
    expect(h.registry.setOwnComputer).toHaveBeenCalledWith("mac", OWNER.id, 1000);
    await h.act(OWNER.id, { action: "window_not_mine", csrf: h.csrf.issue(OWNER.id, "window_not_mine:mac", 1000), device_id: "mac" });
    expect(h.registry.setOwnComputer).toHaveBeenLastCalledWith("mac", null, 1000);
    await h.act(OWNER.id, { action: "window_on", csrf: h.csrf.issue(OWNER.id, "window_on:mac", 1000), device_id: "mac" });
    expect(h.registry.setWindowConfirm).toHaveBeenCalledWith("mac", true, OWNER.id);
  });

  it("change nothing for an administrator, even with a token of their own", async () => {
    const h = harness();
    for (const action of ["window_mine", "window_not_mine", "window_on", "window_off"]) {
      const r = await h.act(ADMIN.id, { action, csrf: h.csrf.issue(ADMIN.id, `${action}:mac`, 1000), device_id: "mac" });
      expect(r.statusCode).toBe(403);
    }
    expect(h.registry.setOwnComputer).not.toHaveBeenCalled();
    expect(h.registry.setWindowConfirm).not.toHaveBeenCalled();
  });

  it("report a refusal (say, pending cleanup) as a failed action, and exist only with the safety check", async () => {
    const h = harness(undefined, "legacy_cleanup_pending");
    const r = await h.act(OWNER.id, { action: "window_on", csrf: h.csrf.issue(OWNER.id, "window_on:mac", 1000), device_id: "mac" });
    expect(r.headers.location).toBe("/api/hassio_ingress/tok/?err=1");
    const noGate = { ...h.deps, gatePage: undefined } as unknown as NovaPageDeps;
    const headers: Record<string, string> = { "x-ingress-path": "/x", "x-remote-user-id": OWNER.id };
    const res = { statusCode: 0, body: "", setHeader: () => undefined, end(b?: string) { if (b) this.body = b; } };
    await createNovaActionHandler(noGate)({ request: { url: "/action", socket: { remoteAddress: "172.30.32.2" }, headers, rawHeaders: Object.entries(headers).flat() } as unknown as IncomingMessage, response: res as unknown as ServerResponse, path: "/action", body: { action: "window_on", csrf: h.csrf.issue(OWNER.id, "window_on:mac", 1000), device_id: "mac" } });
    expect(res.statusCode).toBe(400);
  });

  it("still work on a full page: 64 computers' buttons evict none of the page's tokens", async () => {
    const h = harness(undefined, undefined, Array.from({ length: 64 }, (_, i) => ({ ...on, deviceId: `mac${i}`, name: `Mac ${i}` })));
    const html = (await h.page()).body;
    expect(html.match(/value="window_/g)).toHaveLength(64); // one on/off button each
    const first = h.token(html, "window_off", "mac0")!;
    expect((await h.act(OWNER.id, { action: "window_off", csrf: first, device_id: "mac0" })).statusCode).toBe(303);
    const code = /name="csrf" value="([^"]+)"><input type="hidden" name="action" value="generate_code"/.exec(html)?.[1];
    expect(code).toBeDefined();
    expect(h.csrf.consume(OWNER.id, "generate_code", code!, 1000)).toBe(true);
  });

  it("aren't shown at all without the worker's words", async () => {
    const h = harness();
    (h.deps.gatePage as { texts: unknown }).texts = async () => null;
    const html = (await h.page()).body;
    expect(html).not.toContain("‹page.window_");
    expect(html).not.toContain('value="window_');
  });
});
