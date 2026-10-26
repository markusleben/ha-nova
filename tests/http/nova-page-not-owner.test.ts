// The NOVA page for an admin who isn't the owner: NOVA shows in their sidebar
// (panel_admin), so the refusal names the owner and the next step.
import type { IncomingMessage, ServerResponse } from "node:http";

import { describe, expect, it } from "vitest";

import {
  createNovaActionHandler,
  createNovaPageHandler,
  type NovaPageDeps,
} from "../../nova/src/http/handlers/nova-page.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";

const ADMIN: HaAuthUser = { id: "admin-1", name: "Anna", is_owner: false, is_active: true, system_generated: false };
const OWNER: HaAuthUser = { id: "owner-1", name: "Markus", is_owner: true, is_active: true, system_generated: false };

// Refused before any other dependency is used.
const deps = (users: HaAuthUser[]) => ({ fetchAuthUsers: async () => users }) as unknown as NovaPageDeps;

async function open(users: HaAuthUser[], handler = createNovaPageHandler, peer = "172.30.32.2") {
  const headers = { "x-ingress-path": "/api/hassio_ingress/tok", "x-remote-user-id": ADMIN.id };
  const request = {
    url: "/",
    socket: { remoteAddress: peer },
    headers,
    rawHeaders: Object.entries(headers).flat(),
  } as unknown as IncomingMessage;
  const r = { statusCode: 0, body: "", setHeader: () => undefined, end(b?: string) { if (b) this.body = b; } };
  await handler(deps(users))({ request, response: r as unknown as ServerResponse, path: "/home", body: null });
  return r;
}

const NEXT = "Ask them to open it and click Connect a device. That shows a six-digit code for your computer.";

describe("NOVA page: an admin who isn't the owner", () => {
  it("names the owner and how they connect this computer", async () => {
    for (const handler of [createNovaPageHandler, createNovaActionHandler]) {
      const r = await open([ADMIN, OWNER], handler);
      expect(r.statusCode).toBe(403);
      expect(r.body).toContain(`<p>Only the owner, Markus, can open NOVA. ${NEXT}</p>`);
      expect(r.body).not.toContain("<form");
    }
  });

  it("keeps a hostile owner name inert", async () => {
    const r = await open([ADMIN, { ...OWNER, name: '<img src=x onerror="alert(1)">' }]);
    expect(r.body).toContain("Only the owner, &lt;img src=x onerror=&quot;alert(1)&quot;&gt;, can open NOVA.");
    expect(r.body).not.toContain("<img");
  });

  it("says it generically when no usable owner is in the list", async () => {
    const generic = "Only the owner of this Home Assistant can open NOVA. Ask the owner to open it and click Connect a device.";
    for (const users of [[ADMIN], [ADMIN, { ...OWNER, is_active: false }], [ADMIN, { ...OWNER, name: "  " }], [ADMIN, { ...OWNER, name: 42 as unknown as string }]]) {
      const r = await open(users);
      expect(r.statusCode).toBe(403);
      expect(r.body).toContain(generic);
      expect(r.body).not.toContain("Markus");
    }
  });

  it("says neutrally when it can't tell who opened it, and names nobody", async () => {
    for (const handler of [createNovaPageHandler, createNovaActionHandler]) {
      const r = await open([ADMIN, OWNER], handler, "192.168.1.20"); // not through the Home Assistant sidebar
      expect(r.statusCode).toBe(403);
      expect(r.body).toContain("<p>Couldn&#39;t tell who you are. Open NOVA from the Home Assistant sidebar.</p>");
      expect(r.body).not.toContain("Markus");
      expect(r.body).not.toContain("Only the owner");
    }
  });
});
