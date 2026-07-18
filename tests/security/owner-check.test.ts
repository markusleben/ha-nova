import type { IncomingMessage } from "node:http";

import { describe, expect, it } from "vitest";

import { checkOwner, type HaAuthUser } from "../../nova/src/security/owner-check.js";

const OWNER: HaAuthUser = { id: "owner-1", name: "Owner", is_owner: true, is_active: true, system_generated: false };
const ADMIN: HaAuthUser = { id: "admin-1", name: "Admin", is_owner: false, is_active: true, system_generated: false };
const INACTIVE: HaAuthUser = { id: "inactive-1", is_owner: true, is_active: false };
const SYSTEM: HaAuthUser = { id: "sys-1", is_owner: true, system_generated: true };
const USERS = [OWNER, ADMIN, INACTIVE, SYSTEM];

function req(over: { peer?: string; userId?: string | string[]; ingressPath?: string } = {}): IncomingMessage {
  return {
    socket: { remoteAddress: over.peer ?? "172.30.32.2" },
    headers: {
      "x-ingress-path": over.ingressPath ?? "/api/hassio_ingress/abc",
      ...(over.userId !== undefined ? { "x-remote-user-id": over.userId } : {}),
    },
  } as unknown as IncomingMessage;
}
const users = (u: HaAuthUser[] = USERS) => ({ fetchAuthUsers: async () => u });
const fails = () => ({ fetchAuthUsers: async () => { throw new Error("ws down"); } });

describe("owner-check", () => {
  it("allows a real active non-system owner over ingress", async () => {
    const r = await checkOwner(req({ userId: "owner-1" }), users());
    expect(r).toEqual({ ok: true, userId: "owner-1", name: "Owner" });
  });

  it("denies a plain admin (403)", async () => {
    expect((await checkOwner(req({ userId: "admin-1" }), users())).ok).toBe(false);
  });

  it("denies an inactive or system-generated owner (403)", async () => {
    expect((await checkOwner(req({ userId: "inactive-1" }), users())).ok).toBe(false);
    expect((await checkOwner(req({ userId: "sys-1" }), users())).ok).toBe(false);
  });

  it("denies an unknown user id (403)", async () => {
    expect((await checkOwner(req({ userId: "ghost" }), users())).ok).toBe(false);
  });

  it("denies a direct LAN request that forges the header (wrong socket peer)", async () => {
    const r = await checkOwner(req({ peer: "192.168.1.50", userId: "owner-1" }), users());
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.status).toBe(403);
  });

  it("denies missing or multiple X-Remote-User-Id", async () => {
    expect((await checkOwner(req({}), users())).ok).toBe(false);
    expect((await checkOwner(req({ userId: ["owner-1", "admin-1"] }), users())).ok).toBe(false);
    expect((await checkOwner(req({ userId: "owner-1, admin-1" }), users())).ok).toBe(false);
  });

  it("fails closed with 503 when the auth list cannot be fetched", async () => {
    const r = await checkOwner(req({ userId: "owner-1" }), fails());
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.status).toBe(503);
  });
});
