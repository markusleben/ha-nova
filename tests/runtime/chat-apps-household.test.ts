import { mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { chatAppsFiles } from "../../nova/src/runtime/chat-apps-files.js";
import { ChatAppsError } from "../../nova/src/runtime/chat-apps-ha.js";
import { effectiveLevel, roleCap, usersToSave } from "../../nova/src/runtime/chat-apps-household.js";
import { ChatPhoneError, type ChatPhoneChange, type ChatPhoneControl } from "../../nova/src/runtime/chat-phone-control.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";

// Who may use chat apps (docs/work/2026-10-24-chat-apps-household-spec.md):
// the save's checks against Home Assistant's users, and what it writes to
// the bridge's chat-apps.json, with a removed user's phone link dropped.

const OWNER = "0".repeat(31) + "1";
const ANNA = "a".repeat(32);
const BEN = "b".repeat(32);
const ADMIN = "c".repeat(32);

const users: HaAuthUser[] = [
  { id: OWNER, name: "Markus", is_owner: true, is_active: true, group_ids: ["system-admin"] },
  { id: ANNA, name: "Anna", is_active: true, group_ids: ["system-users"] },
  { id: BEN, name: "Ben", is_active: true, group_ids: ["system-read-only"] },
  { id: ADMIN, name: "Clara", is_active: true, group_ids: ["system-admin"] },
  { id: "d".repeat(32), name: "Off", is_active: false, group_ids: ["system-users"] },
  { id: "e".repeat(32), name: "Supervisor", is_active: true, system_generated: true, group_ids: ["system-admin"] },
];

const save = (picked: Record<string, string>) => usersToSave(OWNER, new Map(Object.entries(picked)), users);
const refusal = (picked: Record<string, string>): string => {
  try {
    save(picked);
  } catch (error) {
    return error instanceof ChatAppsError ? error.code : String(error);
  }
  return "saved";
};

describe("chat-app household levels: the save's checks", () => {
  it("caps each level by the Home Assistant role, as the bridge does", () => {
    expect(users.slice(0, 4).map(roleCap)).toEqual(["manage", "use", "ask", "manage"]);
    expect(roleCap({ id: ANNA, is_active: true })).toBe("ask"); // no group: Ask only
    expect(effectiveLevel(users[1]!, "manage")).toBe("use"); // a level above the role falls to its cap
    expect(effectiveLevel(users[3]!, undefined)).toBe("manage"); // none stored: the cap
    expect(effectiveLevel(users[3]!, "ask")).toBe("ask");
  });

  it("refuses the whole save for an unknown, inactive, or system user, or a level above the role", () => {
    expect(refusal({ [ANNA]: "use", ["f".repeat(32)]: "ask" })).toBe("USER_NOT_PICKABLE");
    expect(refusal({ [ANNA]: "use", ["d".repeat(32)]: "ask" })).toBe("USER_NOT_PICKABLE");
    expect(refusal({ ["e".repeat(32)]: "ask" })).toBe("USER_NOT_PICKABLE");
    expect(refusal({ [ANNA]: "manage" })).toBe("LEVEL_ABOVE_ROLE");
    expect(refusal({ [BEN]: "use" })).toBe("LEVEL_ABOVE_ROLE");
    expect(refusal({ [ANNA]: "" })).toBe("LEVEL_ABOVE_ROLE");
    expect(refusal({ [ANNA]: "admin" })).toBe("LEVEL_ABOVE_ROLE");
  });

  it("always keeps the owner, and marks every explicit list with roles: 1", () => {
    expect(save({ [ANNA]: "use", [BEN]: "ask", [ADMIN]: "manage" })).toEqual({
      allowed_users: [OWNER, ANNA, BEN, ADMIN],
      levels: { [ANNA]: "use", [BEN]: "ask", [ADMIN]: "manage" },
      roles: 1,
    });
    expect(save({ [OWNER]: "ask", [ANNA]: "ask" })).toEqual({ allowed_users: [OWNER, ANNA], levels: { [ANNA]: "ask" }, roles: 1 });
    expect(save({})).toEqual({}); // only the owner: no list
    expect(save({ [OWNER]: "ask" })).toEqual({});
  });
});

let dir: string;
let calls: string[];
let dropFails: boolean;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-chat-household-"));
  mkdirSync(join(dir, "bridge"));
  calls = [];
  dropFails = false;
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

const bridge = (name: string): string => join(dir, "bridge", name);
const settings = (): Record<string, unknown> => JSON.parse(readFileSync(bridge("chat-apps.json"), "utf8")) as Record<string, unknown>;
const links = (byUser: Record<string, string>): void =>
  writeFileSync(bridge("phone-links.json"), JSON.stringify(Object.fromEntries(Object.entries(byUser).map(([u, n]) => [u, { name: n, device_id: "1".repeat(32) }]))), { mode: 0o600 });

function files() {
  const phone: ChatPhoneControl = {
    phones: async () => ({ phones: [], link: null }),
    job: async () => ({ status: "lost" }),
    activity: async () => [],
    seen: async () => ({}),
    start: async (user: string, change: ChatPhoneChange) => {
      // what chat-apps.json lists while the bridge is asked to drop this user's link
      calls.push(`${change.action}:${user}:${JSON.stringify((JSON.parse(readFileSync(bridge("chat-apps.json"), "utf8")) as { allowed_users?: string[] }).allowed_users ?? null)}`);
      if (dropFails) {
        throw new ChatPhoneError(503, "CHAT_PHONE_UNAVAILABLE", "the chat-app bridge can't be reached");
      }
      return null;
    },
  };
  const warned: string[] = [];
  const logger = { info: () => {}, warn: (message: string) => void warned.push(message), error: () => {} };
  return { files: chatAppsFiles(join(dir, "bridge"), phone, logger as never), warned };
}

describe("chat-app household levels: chat-apps.json", () => {
  it("writes the list next to the switches, which it reads again, and only the owner's file mode", async () => {
    writeFileSync(bridge("chat-apps.json"), JSON.stringify({ read_only: true, chat_codes: false }), { mode: 0o600 });
    const { files: f } = files();
    await f.setUsers(OWNER, save({ [ANNA]: "use" }));
    expect(settings()).toEqual({ read_only: true, chat_codes: false, allowed_users: [OWNER, ANNA], levels: { [ANNA]: "use" }, roles: 1 });
    expect(statSync(bridge("chat-apps.json")).mode & 0o777).toBe(0o600);
    f.setSwitches({ readOnly: false });
    expect(settings()).toEqual({ read_only: false, chat_codes: false, allowed_users: [OWNER, ANNA], levels: { [ANNA]: "use" }, roles: 1 });
    expect(f.choices(OWNER)).toMatchObject({ readOnly: false, chatCodes: false, allowedUsers: [OWNER, ANNA], levels: { [ANNA]: "use" } });
  });

  it("removes the list, its levels, and roles when only the owner is left, and drops the removed users' phone links after the write", async () => {
    writeFileSync(bridge("chat-apps.json"), JSON.stringify({ allowed_users: [OWNER, ANNA, BEN], levels: { [ANNA]: "use", [BEN]: "ask" }, roles: 1, chat_codes: true }), { mode: 0o600 });
    links({ [OWNER]: "iPhone", [ANNA]: "Anna's Pixel" });
    const { files: f } = files();
    expect(f.choices(OWNER)).toMatchObject({ phones: { [OWNER]: "iPhone", [ANNA]: "Anna's Pixel" }, ownerPhone: "iPhone", ownerPhoneId: "1".repeat(32) });
    await f.setUsers(OWNER, save({}));
    expect(settings()).toEqual({ chat_codes: true });
    expect(calls).toEqual([`drop:${ANNA}:null`]); // the owner's link stays; Anna's goes once she is off the list
  });

  it("drops an old link of a user added again before the write, so they link a phone anew", async () => {
    writeFileSync(bridge("chat-apps.json"), JSON.stringify({ allowed_users: [OWNER, BEN], levels: { [BEN]: "ask" }, roles: 1 }), { mode: 0o600 });
    links({ [ANNA]: "Anna's Pixel" });
    const { files: f } = files();
    await f.setUsers(OWNER, save({ [ANNA]: "use", [BEN]: "ask" }));
    expect(calls).toEqual([`drop:${ANNA}:${JSON.stringify([OWNER, BEN])}`]);
    expect(settings()).toMatchObject({ allowed_users: [OWNER, ANNA, BEN] });
  });

  it("refuses to add a user again while their old phone link can't be removed", async () => {
    writeFileSync(bridge("chat-apps.json"), JSON.stringify({ chat_codes: false }), { mode: 0o600 });
    links({ [ANNA]: "Anna's Pixel" });
    dropFails = true;
    const { files: f } = files();
    await expect(f.setUsers(OWNER, save({ [ANNA]: "use" }))).rejects.toMatchObject({ status: 503, code: "OLD_LINK_STAYS" });
    expect(settings()).toEqual({ chat_codes: false }); // nothing written: the old phone never confirms again unasked
  });

  it("takes someone off even when phone-links.json can't be read, and adds no one then", async () => {
    writeFileSync(bridge("chat-apps.json"), JSON.stringify({ allowed_users: [OWNER, ANNA], levels: { [ANNA]: "use" }, roles: 1 }), { mode: 0o600 });
    writeFileSync(bridge("phone-links.json"), "{not json", { mode: 0o600 });
    dropFails = true; // the bridge can't read the file either: LINKS_UNWRITABLE
    const { files: f, warned } = files();
    expect(f.choices(OWNER)).toMatchObject({ allowedUsers: [OWNER, ANNA], phones: {}, ownerPhone: null });
    await expect(f.setUsers(OWNER, save({ [ANNA]: "use", [BEN]: "ask" }))).rejects.toMatchObject({ code: "OLD_LINK_STAYS" }); // Ben may have an old link
    await f.setUsers(OWNER, save({}));
    expect(settings()).toEqual({});
    expect(calls).toEqual([`drop:${BEN}:${JSON.stringify([OWNER, ANNA])}`, `drop:${ANNA}:null`]);
    expect(warned).toContain("Chat apps: phone-links.json can't be read");
  });

  it("never shows a hand-edited list the bridge refuses", () => {
    for (const bad of [{ allowed_users: OWNER }, { allowed_users: [OWNER, 1] }, { allowed_users: [OWNER], levels: { [ANNA]: "admin" }, roles: 1 }, { levels: [] }]) {
      writeFileSync(bridge("chat-apps.json"), JSON.stringify(bad), { mode: 0o600 });
      expect(() => files().files.choices(OWNER), JSON.stringify(bad)).toThrow(/chat-apps.json/);
    }
  });

  it("saves even when a drop fails: the bridge ignores the links of users not on the list", async () => {
    writeFileSync(bridge("chat-apps.json"), JSON.stringify({ allowed_users: [OWNER, ANNA], levels: { [ANNA]: "use" }, roles: 1 }), { mode: 0o600 });
    links({ [ANNA]: "Anna's Pixel" });
    dropFails = true;
    const { files: f, warned } = files();
    await f.setUsers(OWNER, save({}));
    expect(settings()).toEqual({});
    expect(warned).toEqual(["Chat apps: a removed user's phone link stays until the next save"]);
  });
});
