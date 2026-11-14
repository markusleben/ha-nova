import type { IncomingMessage } from "node:http";

import { describe, expect, it } from "vitest";

import {
  checkApprover,
  checkOwner,
  isAdmin,
  type HaAuthUser,
} from "../../nova/src/security/owner-check.js";

const OWNER: HaAuthUser = {
  id: "owner-1",
  name: "Owner",
  is_owner: true,
  is_active: true,
  system_generated: false,
};
const ADMIN: HaAuthUser = {
  id: "admin-1",
  name: "Admin",
  is_owner: false,
  is_active: true,
  system_generated: false,
};
const INACTIVE: HaAuthUser = {
  id: "inactive-1",
  is_owner: true,
  is_active: false,
};
const SYSTEM: HaAuthUser = {
  id: "sys-1",
  is_owner: true,
  system_generated: true,
};
const USERS = [OWNER, ADMIN, INACTIVE, SYSTEM];

function req(
  over: {
    peer?: string;
    userId?: string | string[];
    ingressPath?: string;
  } = {},
): IncomingMessage {
  const headers = {
    "x-ingress-path": over.ingressPath ?? "/api/hassio_ingress/abc",
    ...(over.userId !== undefined ? { "x-remote-user-id": over.userId } : {}),
  };
  const rawHeaders = Object.entries(headers).flatMap(([name, value]) =>
    (Array.isArray(value) ? value : [value]).flatMap((item) => [name, item]),
  );
  return {
    socket: { remoteAddress: over.peer ?? "172.30.32.2" },
    headers,
    rawHeaders,
  } as unknown as IncomingMessage;
}
const users = (u: HaAuthUser[] = USERS) => ({ fetchAuthUsers: async () => u });
const fails = () => ({
  fetchAuthUsers: async () => {
    throw new Error("ws down");
  },
});

describe("owner-check", () => {
  it("allows a real active non-system owner over ingress", async () => {
    const r = await checkOwner(req({ userId: "owner-1" }), users());
    expect(r).toEqual({ ok: true, userId: "owner-1", name: "Owner", users: USERS, isOwner: true });
  });

  it("denies a plain admin (403)", async () => {
    expect((await checkOwner(req({ userId: "admin-1" }), users())).ok).toBe(
      false,
    );
  });

  it("denies an inactive or system-generated owner (403)", async () => {
    expect((await checkOwner(req({ userId: "inactive-1" }), users())).ok).toBe(
      false,
    );
    expect((await checkOwner(req({ userId: "sys-1" }), users())).ok).toBe(
      false,
    );
  });

  it("denies an unknown user id (403)", async () => {
    expect((await checkOwner(req({ userId: "ghost" }), users())).ok).toBe(
      false,
    );
  });

  it("denies a direct LAN request that forges the header (wrong socket peer)", async () => {
    const r = await checkOwner(
      req({ peer: "192.0.2.50", userId: "owner-1" }),
      users(),
    );
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.status).toBe(403);
  });

  it("denies missing or multiple X-Remote-User-Id", async () => {
    expect((await checkOwner(req({}), users())).ok).toBe(false);
    expect(
      (await checkOwner(req({ userId: ["owner-1", "admin-1"] }), users())).ok,
    ).toBe(false);
    expect(
      (await checkOwner(req({ userId: "owner-1, admin-1" }), users())).ok,
    ).toBe(false);
  });

  it("fails closed with 503 when the auth list cannot be fetched", async () => {
    const r = await checkOwner(req({ userId: "owner-1" }), fails());
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.status).toBe(503);
  });
});

// The approval view's refusals name their page text; the worker words it
// (page-texts.ts), so no English sits here.
describe("approver check", () => {
  const ADMIN_GROUP = { ...ADMIN, group_ids: ["system-admin"] };
  const deps = (relay = "relay-1", u: HaAuthUser[] = [OWNER, ADMIN_GROUP]) => ({ ...users(u), relayUserId: async () => relay });
  it("names the text for each refusal", async () => {
    const cases: [IncomingMessage, Parameters<typeof checkApprover>[1], unknown][] = [
      [req({}), deps(), { ok: false, status: 403, text: "page.approver_unknown" }],
      [req({ userId: "owner-1" }), { ...fails(), relayUserId: async () => "relay-1" }, { ok: false, status: 503, text: "page.approver_unavailable" }],
      [req({ userId: "owner-1" }), deps(""), { ok: false, status: 503, text: "page.approver_unavailable" }],
      [req({ userId: "ghost" }), deps(), { ok: false, status: 403, text: "page.approver_not_admin" }],
      [req({ userId: "owner-1" }), deps("owner-1"), { ok: false, status: 403, text: "page.approver_relay_user" }],
      [req({ userId: "admin-1" }), deps(), { ok: true, userId: "admin-1", name: "Admin" }],
    ];
    for (const [request, d, want] of cases) {
      expect(await checkApprover(request, d)).toEqual(want);
    }
  });
});

// The NOVA page's administrators (docs/work/2026-11-12-nova-page-admins-spec.md):
// a positive read of an active, non-system admin; any role it can't read is none.
describe("checkOwner with admins", () => {
  const base = { id: "a-1", name: "Anna", is_owner: false, is_active: true, system_generated: false };
  it("admits the owner and an administrator, and tells them apart", async () => {
    const admin = { ...base, group_ids: ["system-admin"] };
    const both = [OWNER, admin];
    const owner = await checkOwner(req({ userId: "owner-1" }), users(both), { admins: true });
    const anna = await checkOwner(req({ userId: "a-1" }), users(both), { admins: true });
    expect([owner.ok && owner.isOwner, anna.ok && anna.isOwner, anna.ok]).toEqual([true, false, true]);
    expect((await checkOwner(req({ userId: "a-1" }), users(both))).ok).toBe(false); // owner-only routes stay owner-only
  });
  it("refuses every role it can't read positively", () => {
    for (const user of [
      { ...base, group_ids: ["system-users"] },
      { ...base, group_ids: ["system-read-only"] },
      { ...base, group_ids: "system-admin" as unknown as string[] },
      { ...base },
      { id: "a-1", is_owner: false, system_generated: false, group_ids: ["system-admin"] },
      { ...base, is_active: false, group_ids: ["system-admin"] },
      { ...base, system_generated: true, group_ids: ["system-admin"] },
      { id: "o-1", is_owner: true, system_generated: false },
    ]) {
      expect(isAdmin(user as HaAuthUser), JSON.stringify(user)).toBe(false);
    }
  });
  it("answers 503 when the users can't be read", async () => {
    const r = await checkOwner(req({ userId: "a-1" }), { fetchAuthUsers: async () => { throw new Error("down"); } }, { admins: true });
    expect(r).toMatchObject({ ok: false, status: 503 });
  });
});
