// The computer's waiting window opens its ask on the NOVA page (MAR-175):
// NOVA's panel with ?nova_ask=<ref> hands that ask to the approval view
// top-level, which shows it first. The ref approves nothing on its own.
import { createHash } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import { runInNewContext } from "node:vm";

import { describe, expect, it } from "vitest";

import { createApprovePageHandler, type ApprovePageDeps } from "../../nova/src/http/handlers/nova-approve.js";
import { createNovaPageHandler, type NovaPageDeps } from "../../nova/src/http/handlers/nova-page.js";
import { LIVE_SCRIPT } from "../../nova/src/http/handlers/nova-page-live.js";
import { PAGE_KEYS, pageTexts } from "../../nova/src/http/page-texts.js";
import { createCsrfStore } from "../../nova/src/security/csrf.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";

const OWNER: HaAuthUser = { id: "owner-1", name: "Markus", is_owner: true, is_active: true, system_generated: false, group_ids: ["system-admin"] };
const ADMIN: HaAuthUser = { id: "admin-1", name: "Anna", is_owner: false, is_active: true, system_generated: false, group_ids: ["system-admin"] };
const RELAY: HaAuthUser = { id: "relay-1", name: "HA NOVA", is_owner: false, is_active: true, system_generated: false, group_ids: ["system-admin"] };
const TEXTS = Object.fromEntries(PAGE_KEYS.map((key) => [key, `‹${key}›`]));
const ref = (id: string) => createHash("sha256").update(id).digest("hex");
const asks = Array.from({ length: 5 }, (_, i) => ({ ref: ref("ask" + i), body: "Requested by Mac.", from: "Requested by Mac.", items: [{ verdict: "typed" as const, text: "Unlock door " + i }], expiresAt: 9_000 + i }));
const page = { waiting: () => asks, pausedDevices: () => [], approve: () => false, cancel: () => false, resume: () => false, texts: pageTexts(async () => ({ lang: "en", texts: TEXTS }), () => 0) };
type Handler = ReturnType<typeof createApprovePageHandler>;

async function get(handler: Handler, url: string) {
  const headers: Record<string, string> = { "x-ingress-path": "/api/hassio_ingress/tok", "x-remote-user-id": ADMIN.id };
  const request = { url, socket: { remoteAddress: "172.30.32.2" }, headers, rawHeaders: Object.entries(headers).flat() } as unknown as IncomingMessage;
  const r = { statusCode: 0, body: "", setHeader: () => undefined, end(b?: string) { if (b) this.body = b; } };
  await handler({ request, response: r as unknown as ServerResponse, path: url.split("?")[0]!, body: null });
  return r;
}

// Runs the live script as a page in NOVA's panel would; top: the window it's in.
function run(top: { search?: string; throws?: boolean } | "self", dataset: Record<string, string>): string[] {
  const replaced: string[] = [];
  const self = {};
  const foreign = new Proxy({}, { get: () => { throw new Error("SecurityError"); } });
  const frame = top === "self" ? self : { location: top.throws ? foreign : { search: top.search ?? "", replace: (to: string) => void replaced.push(to) } };
  runInNewContext(LIVE_SCRIPT, {
    self, top: frame, URLSearchParams, Date, Math, String,
    location: { replace: (to: string) => void replaced.push("self:" + to), reload: () => undefined },
    document: { body: { dataset }, hidden: false, querySelectorAll: () => [], querySelector: () => null },
    addEventListener: () => undefined, setTimeout: () => 0, setInterval: () => 0, fetch: async () => null,
  });
  return replaced;
}

describe("the window's ask on the NOVA page", () => {
  const approve = "/api/hassio_ingress/tok/approve";

  it("the approval view shows the named ask first, keeps it on a live reload, and ignores anything but a ref", async () => {
    const deps: ApprovePageDeps = { fetchAuthUsers: async () => [OWNER, ADMIN, RELAY], relayUserId: async () => RELAY.id, csrf: createCsrfStore(), page, panel: async () => "/app/x_ha_nova", now: () => 1_000 };
    const named = await get(createApprovePageHandler(deps), `/approve?nova_ask=${asks[4]!.ref}`);
    expect(named.body).toContain("Unlock door 4");
    expect(named.body.indexOf("Unlock door 4")).toBeLessThan(named.body.indexOf("Unlock door 0"));
    expect(named.body).not.toContain("Unlock door 2"); // three shown: the named one, then the soonest
    expect(named.body).toContain(`data-reload="${approve}?nova_ask=${asks[4]!.ref}"`);
    for (const bad of ["ask4", asks[4]!.ref.toUpperCase(), `${asks[4]!.ref}"><script>`]) {
      const plain = await get(createApprovePageHandler(deps), `/approve?nova_ask=${encodeURIComponent(bad)}`);
      expect(plain.body).not.toContain("Unlock door 4");
      expect(plain.body).toContain(`data-reload="${approve}"`);
    }
  });

  it("the NOVA page names the approval view for the hand-off", async () => {
    const deps = {
      fetchAuthUsers: async () => [OWNER, ADMIN], gatePage: page, csrf: createCsrfStore(), now: () => 1000,
      registry: { list: () => [] }, pairing: { getStatus: () => ({ phase: "idle" }) }, connection: () => ({ haConnected: true }),
      update: async () => ({ version: "0.7.0", versionLatest: "0.7.0", updateAvailable: false, error: false }), relayVersion: "0.7.0",
    } as unknown as NovaPageDeps;
    const r = await get(createNovaPageHandler(deps) as Handler, "/");
    expect(r.body).toContain(`data-ask="${approve}"`);
  });

  it("the live script hands the named ask to the approval view top-level, only from a frame and only when one is named", () => {
    expect(run({ search: `?nova_ask=${asks[1]!.ref}` }, { ask: approve })).toEqual([`${approve}?nova_ask=${asks[1]!.ref}`]);
    expect(run({ search: "?nova_ask=a%26b%3Dc" }, { ask: approve })).toEqual([`${approve}?nova_ask=a%26b%3Dc`]); // passed on encoded; the view checks it
    expect(run({ search: "" }, { ask: approve })).toEqual([]);
    expect(run({ search: `?nova_ask=${asks[1]!.ref}` }, {})).toEqual([]); // the approval view itself: no hand-off
    expect(run("self", { ask: approve })).toEqual([]);
    expect(run({ throws: true }, { ask: approve })).toEqual([]); // a foreign top can't be read: no hand-off, no error
  });
});
